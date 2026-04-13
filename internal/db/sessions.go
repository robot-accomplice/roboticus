package db

import (
	"context"
	"database/sql"
	"time"

	"roboticus/internal/core"
)

// Session represents a conversation session.
type Session struct {
	ID                  string
	AgentID             string
	ScopeKey            string
	Status              string
	Model               sql.NullString
	Nickname            sql.NullString
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Metadata            sql.NullString
	CrossChannelConsent bool
}

// FindOrCreateSession returns the active session for the given agent and scope,
// creating one if none exists.
func (s *Store) FindOrCreateSession(ctx context.Context, agentID, scopeKey string) (*Session, error) {
	sess, err := s.FindActiveSession(ctx, agentID, scopeKey)
	if err != nil {
		return nil, err
	}
	if sess != nil {
		return sess, nil
	}

	id := newID()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.ExecContext(ctx,
		`INSERT OR IGNORE INTO sessions (id, agent_id, scope_key, status, created_at, updated_at)
		 VALUES (?, ?, ?, 'active', ?, ?)`,
		id, agentID, scopeKey, now, now,
	)
	if err != nil {
		return nil, core.WrapError(core.ErrDatabase, "failed to create session", err)
	}

	// Re-query to handle the race: if another instance created the session
	// between our initial check and the INSERT OR IGNORE, we pick up theirs.
	return s.FindActiveSession(ctx, agentID, scopeKey)
}

// FindActiveSession returns the active session for the given agent and scope, or nil.
func (s *Store) FindActiveSession(ctx context.Context, agentID, scopeKey string) (*Session, error) {
	row := s.QueryRowContext(ctx,
		`SELECT id, agent_id, scope_key, status, model, nickname,
		        created_at, updated_at, metadata, cross_channel_consent
		 FROM sessions
		 WHERE agent_id = ? AND scope_key = ? AND status = 'active'
		 LIMIT 1`,
		agentID, scopeKey,
	)

	sess := &Session{}
	var createdAt, updatedAt string
	var consent int
	err := row.Scan(
		&sess.ID, &sess.AgentID, &sess.ScopeKey, &sess.Status,
		&sess.Model, &sess.Nickname,
		&createdAt, &updatedAt,
		&sess.Metadata, &consent,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, core.WrapError(core.ErrDatabase, "failed to find session", err)
	}

	sess.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	sess.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	sess.CrossChannelConsent = consent != 0
	return sess, nil
}

// GetSession returns a session by ID.
func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	row := s.QueryRowContext(ctx,
		`SELECT id, agent_id, scope_key, status, model, nickname,
		        created_at, updated_at, metadata, cross_channel_consent
		 FROM sessions WHERE id = ?`,
		id,
	)

	sess := &Session{}
	var createdAt, updatedAt string
	var consent int
	err := row.Scan(
		&sess.ID, &sess.AgentID, &sess.ScopeKey, &sess.Status,
		&sess.Model, &sess.Nickname,
		&createdAt, &updatedAt,
		&sess.Metadata, &consent,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, core.WrapError(core.ErrDatabase, "failed to get session", err)
	}

	sess.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	sess.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	sess.CrossChannelConsent = consent != 0
	return sess, nil
}

// ArchiveSession sets a session's status to 'archived' and fires post-archival hooks.
func (s *Store) ArchiveSession(ctx context.Context, id string) error {
	_, err := s.ExecContext(ctx,
		`UPDATE sessions SET status = 'archived', updated_at = datetime('now') WHERE id = ?`,
		id,
	)
	if err != nil {
		return core.WrapError(core.ErrDatabase, "failed to archive session", err)
	}
	// Fire post-archival callbacks (best-effort — hook errors don't block archival).
	for _, cb := range s.onSessionArchived {
		func() {
			defer func() { recover() }() // Don't let panicking hooks block archival.
			cb(ctx, id)
		}()
	}
	return nil
}

// ListSessions returns sessions for the given agent, ordered by updated_at DESC.
func (s *Store) ListSessions(ctx context.Context, agentID string, limit int) ([]Session, error) {
	rows, err := s.QueryContext(ctx,
		`SELECT id, agent_id, scope_key, status, model, nickname,
		        created_at, updated_at, metadata, cross_channel_consent
		 FROM sessions
		 WHERE agent_id = ?
		 ORDER BY updated_at DESC
		 LIMIT ?`,
		agentID, limit,
	)
	if err != nil {
		return nil, core.WrapError(core.ErrDatabase, "failed to list sessions", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []Session
	for rows.Next() {
		var sess Session
		var createdAt, updatedAt string
		var consent int
		if err := rows.Scan(
			&sess.ID, &sess.AgentID, &sess.ScopeKey, &sess.Status,
			&sess.Model, &sess.Nickname,
			&createdAt, &updatedAt,
			&sess.Metadata, &consent,
		); err != nil {
			return nil, core.WrapError(core.ErrDatabase, "failed to scan session", err)
		}
		sess.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		sess.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		sess.CrossChannelConsent = consent != 0
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

// InsertMessage stores a session message.
func (s *Store) InsertMessage(ctx context.Context, sessionID, role, content string) (string, error) {
	id := newID()
	_, err := s.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content, created_at)
		 VALUES (?, ?, ?, ?, datetime('now'))`,
		id, sessionID, role, content,
	)
	if err != nil {
		return "", core.WrapError(core.ErrDatabase, "failed to insert message", err)
	}
	return id, nil
}

// newID generates a unique ID using crypto/rand.
func newID() string {
	b := make([]byte, 16)
	if _, err := readRandom(b); err != nil {
		panic("db.newID: crypto/rand failed: " + err.Error())
	}
	return encodeHex(b)
}
