package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// OAuthPKCEConfig holds the parameters for an OAuth 2.0 PKCE flow.
type OAuthPKCEConfig struct {
	AuthURL      string // Authorization endpoint
	TokenURL     string // Token endpoint
	ClientID     string // OAuth client ID (public, no secret)
	RedirectPort int    // Local port for redirect callback (default 18788)
	Scopes       []string
}

// OAuthToken holds the result of a successful OAuth flow.
type OAuthToken struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresIn    int       `json:"expires_in,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scope        string    `json:"scope,omitempty"`
}

// IsExpired returns true if the token has expired.
func (t *OAuthToken) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(t.ExpiresAt)
}

// PKCEChallenge holds a PKCE code verifier and its derived challenge.
type PKCEChallenge struct {
	Verifier  string // Random 43-128 char string (RFC 7636)
	Challenge string // base64url(SHA256(verifier))
	Method    string // Always "S256"
}

// GeneratePKCE creates a new PKCE code verifier and challenge.
func GeneratePKCE() (*PKCEChallenge, error) {
	// Generate 32 random bytes → 43-char base64url string.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("generate verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)

	// S256: base64url(SHA256(verifier))
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	return &PKCEChallenge{
		Verifier:  verifier,
		Challenge: challenge,
		Method:    "S256",
	}, nil
}

// RunOAuthPKCEFlow executes the full OAuth 2.0 PKCE authorization code flow.
// It starts a local HTTP server, opens the authorization URL in a browser,
// waits for the callback, and exchanges the code for tokens.
//
// Matches Rust's OAuth PKCE flow for providers like Anthropic.
func RunOAuthPKCEFlow(ctx context.Context, cfg OAuthPKCEConfig) (*OAuthToken, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	port := cfg.RedirectPort
	if port == 0 {
		port = 18788
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	// Build authorization URL.
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {cfg.ClientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {pkce.Method},
	}
	if len(cfg.Scopes) > 0 {
		params.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	// Generate state parameter for CSRF protection.
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("oauth: crypto/rand failed: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	params.Set("state", state)

	authorizationURL := cfg.AuthURL + "?" + params.Encode()

	// Start local callback server.
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// Verify state.
		if r.URL.Query().Get("state") != state {
			errCh <- fmt.Errorf("state mismatch: possible CSRF attack")
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errCh <- fmt.Errorf("OAuth error: %s — %s", errMsg, r.URL.Query().Get("error_description"))
			http.Error(w, "Authorization failed", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no authorization code in callback")
			http.Error(w, "No code", http.StatusBadRequest)
			return
		}
		if _, err := fmt.Fprintf(w, "<html><body><h2>Authorization successful!</h2><p>You can close this tab and return to the terminal.</p></body></html>"); err != nil {
			log.Trace().Err(err).Msg("oauth: callback response write failed")
		}
		codeCh <- code
	})

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("start callback server: %w", err)
	}
	defer func() { _ = listener.Close() }()

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	// Ensure server shuts down when context cancels (belt-and-suspenders with defer).
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	defer func() { _ = server.Shutdown(context.Background()) }()

	// Return the authorization URL — caller must open it in a browser.
	fmt.Printf("\nOpen this URL in your browser to authorize:\n\n  %s\n\nWaiting for callback...\n", authorizationURL)

	// Wait for callback or timeout.
	select {
	case code := <-codeCh:
		return exchangeCodeForToken(ctx, cfg.TokenURL, cfg.ClientID, code, redirectURI, pkce.Verifier)
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("OAuth flow timed out (5 minute limit)")
	}
}

// exchangeCodeForToken exchanges an authorization code for access/refresh tokens.
func exchangeCodeForToken(ctx context.Context, tokenURL, clientID, code, redirectURI, codeVerifier string) (*OAuthToken, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {codeVerifier},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var token OAuthToken
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if token.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	return &token, nil
}

// RefreshOAuthToken uses a refresh token to obtain new access/refresh tokens.
func RefreshOAuthToken(ctx context.Context, tokenURL, clientID, refreshToken string) (*OAuthToken, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var token OAuthToken
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}
	if token.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	return &token, nil
}
