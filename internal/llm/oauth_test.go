package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func futureTime() time.Time { return time.Now().Add(1 * time.Hour) }

// mockRefresher is a test double for TokenRefresher.
type mockRefresher struct {
	newToken *OAuthToken
	err      error
	called   int
}

func (m *mockRefresher) Refresh(_ context.Context, refreshToken string) (*OAuthToken, error) {
	m.called++
	return m.newToken, m.err
}

func TestTokenManager_SetAndGet(t *testing.T) {
	tm := NewTokenManager(nil)
	tok := &OAuthToken{
		AccessToken:  "access123",
		RefreshToken: "refresh456",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		ProviderName: "openai",
	}
	tm.SetToken("openai", tok)

	got, err := tm.GetToken(context.Background(), "openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "access123" {
		t.Errorf("expected 'access123', got %q", got)
	}
}

func TestTokenManager_MissingToken(t *testing.T) {
	tm := NewTokenManager(nil)
	_, err := tm.GetToken(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for missing token")
	}
}

func TestTokenManager_HasToken(t *testing.T) {
	tm := NewTokenManager(nil)
	if tm.HasToken("openai") {
		t.Error("should not have token before SetToken")
	}
	tm.SetToken("openai", &OAuthToken{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})
	if !tm.HasToken("openai") {
		t.Error("should have token after SetToken")
	}
}

func TestTokenManager_ExpiredTokenRefresh(t *testing.T) {
	newTok := &OAuthToken{
		AccessToken:  "new_access",
		RefreshToken: "new_refresh",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		ProviderName: "openai",
	}
	refresher := &mockRefresher{newToken: newTok}
	tm := NewTokenManager(refresher)

	// Store an already-expired token.
	expired := &OAuthToken{
		AccessToken:  "old_access",
		RefreshToken: "old_refresh",
		ExpiresAt:    time.Now().Add(-1 * time.Minute), // in the past
		ProviderName: "openai",
	}
	tm.SetToken("openai", expired)

	got, err := tm.GetToken(context.Background(), "openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "new_access" {
		t.Errorf("expected 'new_access', got %q", got)
	}
	if refresher.called != 1 {
		t.Errorf("expected refresher called once, got %d", refresher.called)
	}
}

func TestTokenManager_ExpiredNoRefresher(t *testing.T) {
	tm := NewTokenManager(nil)
	expired := &OAuthToken{
		AccessToken:  "old",
		RefreshToken: "ref",
		ExpiresAt:    time.Now().Add(-1 * time.Minute),
	}
	tm.SetToken("openai", expired)

	_, err := tm.GetToken(context.Background(), "openai")
	if err == nil {
		t.Error("expected error when token expired and no refresher")
	}
}

func TestTokenManager_RefreshError(t *testing.T) {
	refresher := &mockRefresher{err: errors.New("network error")}
	tm := NewTokenManager(refresher)

	expired := &OAuthToken{
		AccessToken:  "old",
		RefreshToken: "ref",
		ExpiresAt:    time.Now().Add(-1 * time.Minute),
	}
	tm.SetToken("openai", expired)

	_, err := tm.GetToken(context.Background(), "openai")
	if err == nil {
		t.Error("expected error when refresh fails")
	}
}

func TestOAuthToken_IsExpired(t *testing.T) {
	// Future token: not expired.
	future := &OAuthToken{ExpiresAt: time.Now().Add(1 * time.Hour)}
	if future.IsExpired() {
		t.Error("token with future expiry should not be expired")
	}

	// Past token: expired.
	past := &OAuthToken{ExpiresAt: time.Now().Add(-1 * time.Minute)}
	if !past.IsExpired() {
		t.Error("token with past expiry should be expired")
	}

	// Within 30s buffer: should be considered expired.
	nearExpiry := &OAuthToken{ExpiresAt: time.Now().Add(10 * time.Second)}
	if !nearExpiry.IsExpired() {
		t.Error("token within 30s buffer should be considered expired")
	}
}

func TestGenerateCodeVerifier(t *testing.T) {
	v, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatal(err)
	}
	if len(v) < 43 {
		t.Errorf("verifier too short: %d chars", len(v))
	}
	v2, _ := GenerateCodeVerifier()
	if v == v2 {
		t.Error("verifiers should not be equal")
	}
}

func TestComputeCodeChallenge(t *testing.T) {
	ch := ComputeCodeChallenge("test_verifier")
	if ch == "" {
		t.Error("empty challenge")
	}
	if strings.ContainsAny(ch, "+/=") {
		t.Errorf("non-URL-safe chars in challenge: %s", ch)
	}
	if ComputeCodeChallenge("test_verifier") != ch {
		t.Error("challenge should be deterministic")
	}
}

func TestBuildAuthorizationURL(t *testing.T) {
	u := BuildAuthorizationURL(
		"https://auth.example.com/authorize",
		"client123",
		"http://localhost:8080/callback",
		"challenge_value",
		[]string{"openid", "profile"},
	)
	for _, want := range []string{"response_type=code", "client_id=client123", "code_challenge=challenge_value", "code_challenge_method=S256"} {
		if !strings.Contains(u, want) {
			t.Errorf("URL missing %q: %s", want, u)
		}
	}
}

func TestTokenManager_Status(t *testing.T) {
	tm := NewTokenManager(nil)
	s := tm.Status("openai")
	if s.HasToken {
		t.Error("should not have token")
	}

	tm.SetToken("openai", &OAuthToken{AccessToken: "tok", ExpiresAt: futureTime()})
	s = tm.Status("openai")
	if !s.HasToken || s.Expired {
		t.Error("should have valid token")
	}
}

func TestTokenManager_AllStatuses(t *testing.T) {
	tm := NewTokenManager(nil)
	tm.SetToken("a", &OAuthToken{AccessToken: "x", ExpiresAt: futureTime()})
	tm.SetToken("b", &OAuthToken{AccessToken: "y", ExpiresAt: futureTime()})
	if len(tm.AllStatuses()) != 2 {
		t.Errorf("got %d statuses, want 2", len(tm.AllStatuses()))
	}
}
