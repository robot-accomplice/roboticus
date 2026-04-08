package pipeline

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
)

const consentExpirySeconds = 300 // 5 minutes

// ConsentCheckResult indicates the outcome of a consent check.
type ConsentCheckResult int

const (
	ConsentContinue ConsentCheckResult = iota // No consent action needed, continue pipeline
	ConsentGranted                            // User confirmed consent, return synthetic response
	ConsentBlocked                            // Cross-channel access denied, return 403
)

// checkCrossChannelConsent verifies that cross-channel session access is authorized.
// If the requesting channel differs from the session's origin channel, consent is required.
// Matches Rust's cross-channel consent gate in pipeline stage 4a.
func (p *Pipeline) checkCrossChannelConsent(ctx context.Context, session *Session, input Input) (ConsentCheckResult, string) {
	if p.store == nil {
		return ConsentContinue, ""
	}

	// Extract origin channel from session's scope key (format: "platform:chatid" or "platform:peer:userid").
	originChannel := extractOriginChannel(session.ScopeKey)
	if originChannel == "" || originChannel == input.Platform {
		return ConsentContinue, "" // Same channel or no origin — no consent needed.
	}

	// Check if this is a consent confirmation from the origin channel.
	if isConsentConfirmation(input.Content) {
		return p.handleConsentConfirmation(ctx, session, input)
	}

	// Check if consent already fulfilled for this requesting principal.
	if p.hasfulfilledConsent(ctx, session.ID, input.Platform, input.SenderID) {
		return ConsentContinue, ""
	}

	// Create a consent request and block the requester.
	requestID := db.NewID()
	expiresAt := time.Now().Add(consentExpirySeconds * time.Second).UTC().Format(time.RFC3339)
	_, err := p.store.ExecContext(ctx,
		`INSERT INTO consent_requests (id, session_id, origin_channel, origin_recipient, requesting_channel, requesting_recipient, status, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)`,
		requestID, session.ID, originChannel, session.ScopeKey,
		input.Platform, input.SenderID, expiresAt,
	)
	if err != nil {
		// Fail closed: cross-channel consent is a security control (Rule 4.4).
		// If we can't create the consent request due to DB errors, we must
		// deny the cross-channel access rather than silently granting it.
		log.Error().Err(err).Msg("consent request creation failed — denying cross-channel access (fail-closed)")
		return ConsentBlocked, "Cross-channel access temporarily unavailable. Please try again."
	}

	log.Info().
		Str("session", session.ID).
		Str("origin", originChannel).
		Str("requesting", input.Platform).
		Str("requester", input.SenderID).
		Msg("cross-channel consent required")

	// The pipeline should notify the origin channel. For now, return blocked
	// with a message the API can forward.
	msg := "Cross-channel access request: someone on " + input.Platform +
		" is requesting access to this session. Reply 'yes' on " + originChannel +
		" to allow (expires in 5 minutes)."
	return ConsentBlocked, msg
}

// handleConsentConfirmation processes a "yes"/"confirm" reply on the origin channel.
func (p *Pipeline) handleConsentConfirmation(ctx context.Context, session *Session, input Input) (ConsentCheckResult, string) {
	// Find pending consent request for this session + origin channel.
	var requestID, requestingChannel string
	row := p.store.QueryRowContext(ctx,
		`SELECT id, requesting_channel FROM consent_requests
		 WHERE session_id = ? AND origin_channel = ? AND status = 'pending'
		 AND expires_at > datetime('now')
		 ORDER BY created_at DESC LIMIT 1`,
		session.ID, input.Platform,
	)
	if err := row.Scan(&requestID, &requestingChannel); err != nil {
		return ConsentContinue, "" // No pending request — process normally.
	}

	// Fulfill the consent request.
	_, err := p.store.ExecContext(ctx,
		`UPDATE consent_requests SET status = 'fulfilled', resolved_at = datetime('now') WHERE id = ?`,
		requestID,
	)
	if err != nil {
		log.Warn().Err(err).Str("request", requestID).Msg("failed to fulfill consent")
		return ConsentContinue, ""
	}

	log.Info().
		Str("session", session.ID).
		Str("request", requestID).
		Str("granted_for", requestingChannel).
		Msg("cross-channel consent granted")

	return ConsentGranted, "Cross-channel access granted. The session is now accessible from " + requestingChannel + "."
}

// hasfulfilledConsent checks if a fulfilled consent exists for the given triple.
func (p *Pipeline) hasfulfilledConsent(ctx context.Context, sessionID, requestingChannel, requestingRecipient string) bool {
	var count int
	row := p.store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM consent_requests
		 WHERE session_id = ? AND requesting_channel = ? AND requesting_recipient = ? AND status = 'fulfilled'`,
		sessionID, requestingChannel, requestingRecipient,
	)
	if err := row.Scan(&count); err != nil {
		return false
	}
	return count > 0
}

// isConsentConfirmation checks if a message is a consent confirmation keyword.
func isConsentConfirmation(content string) bool {
	lower := strings.TrimSpace(strings.ToLower(content))
	switch lower {
	case "yes", "y", "confirm", "allow":
		return true
	}
	return false
}

// extractOriginChannel extracts the platform name from a session scope key.
// Scope keys are formatted as "platform:chatid" or "platform:peer:userid".
func extractOriginChannel(scopeKey string) string {
	if scopeKey == "" {
		return ""
	}
	parts := strings.SplitN(scopeKey, ":", 2)
	if len(parts) == 0 {
		return ""
	}
	platform := parts[0]
	// Filter out non-channel scope keys (e.g., "api:sessionid").
	switch platform {
	case "telegram", "discord", "whatsapp", "signal", "email", "matrix", "voice", "web", "a2a":
		return platform
	}
	return ""
}

// SetCrossChannelConsent updates the session's cross_channel_consent flag.
func (p *Pipeline) SetCrossChannelConsent(ctx context.Context, sessionID string, consent bool) error {
	if p.store == nil {
		return core.NewError(core.ErrDatabase, "no store")
	}
	val := 0
	if consent {
		val = 1
	}
	_, err := p.store.ExecContext(ctx,
		`UPDATE sessions SET cross_channel_consent = ? WHERE id = ?`, val, sessionID)
	return err
}
