package llm

import (
	"context"
	"fmt"
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
