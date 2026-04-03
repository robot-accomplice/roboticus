package llm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"sync"
	"time"
)

// OAuthToken holds a provider's OAuth2 credentials.
type OAuthToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	ProviderName string
}

// IsExpired returns true if the token needs refresh.
func (t *OAuthToken) IsExpired() bool {
	return time.Now().After(t.ExpiresAt.Add(-30 * time.Second)) // 30s buffer
}

// TokenRefresher refreshes an OAuth token.
type TokenRefresher interface {
	Refresh(ctx context.Context, refreshToken string) (*OAuthToken, error)
}

// TokenManager manages OAuth tokens for multiple providers.
type TokenManager struct {
	mu        sync.RWMutex
	tokens    map[string]*OAuthToken
	refresher TokenRefresher
}

// NewTokenManager creates a new token manager with an optional refresher.
func NewTokenManager(refresher TokenRefresher) *TokenManager {
	return &TokenManager{
		tokens:    make(map[string]*OAuthToken),
		refresher: refresher,
	}
}

// SetToken stores a token for a provider.
func (tm *TokenManager) SetToken(provider string, token *OAuthToken) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.tokens[provider] = token
}

// GetToken returns a valid token, refreshing if expired.
func (tm *TokenManager) GetToken(ctx context.Context, provider string) (string, error) {
	tm.mu.RLock()
	tok, ok := tm.tokens[provider]
	tm.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("no token for provider %q", provider)
	}

	if !tok.IsExpired() {
		return tok.AccessToken, nil
	}

	if tm.refresher == nil {
		return "", fmt.Errorf("token expired for %q and no refresher configured", provider)
	}

	newTok, err := tm.refresher.Refresh(ctx, tok.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh failed for %q: %w", provider, err)
	}

	tm.mu.Lock()
	tm.tokens[provider] = newTok
	tm.mu.Unlock()

	return newTok.AccessToken, nil
}

// HasToken returns whether a token exists for the provider.
func (tm *TokenManager) HasToken(provider string) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	_, ok := tm.tokens[provider]
	return ok
}

// TokenStatus reports the state of a provider's OAuth token.
type TokenStatus struct {
	Provider  string    `json:"provider"`
	HasToken  bool      `json:"has_token"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Expired   bool      `json:"expired"`
}

// Status returns the token status for a provider.
func (tm *TokenManager) Status(provider string) TokenStatus {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	tok, ok := tm.tokens[provider]
	if !ok {
		return TokenStatus{Provider: provider}
	}
	return TokenStatus{
		Provider:  provider,
		HasToken:  true,
		ExpiresAt: tok.ExpiresAt,
		Expired:   tok.IsExpired(),
	}
}

// AllStatuses returns token status for all providers.
func (tm *TokenManager) AllStatuses() []TokenStatus {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	statuses := make([]TokenStatus, 0, len(tm.tokens))
	for name, tok := range tm.tokens {
		statuses = append(statuses, TokenStatus{
			Provider:  name,
			HasToken:  true,
			ExpiresAt: tok.ExpiresAt,
			Expired:   tok.IsExpired(),
		})
	}
	return statuses
}

// --- PKCE (Proof Key for Code Exchange) helpers ---

// GenerateCodeVerifier creates a random PKCE code verifier (43-128 chars).
func GenerateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate code verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ComputeCodeChallenge computes the S256 PKCE code challenge from a verifier.
func ComputeCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// BuildAuthorizationURL constructs an OAuth2 authorization URL with PKCE.
func BuildAuthorizationURL(authorizeEndpoint, clientID, redirectURI, codeChallenge string, scopes []string) string {
	u, err := url.Parse(authorizeEndpoint)
	if err != nil {
		return authorizeEndpoint
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	if len(scopes) > 0 {
		scope := ""
		for i, s := range scopes {
			if i > 0 {
				scope += " "
			}
			scope += s
		}
		q.Set("scope", scope)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
