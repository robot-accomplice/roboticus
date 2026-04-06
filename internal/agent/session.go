package agent

import "roboticus/internal/session"

// Session is an alias for the shared session type.
type Session = session.Session

// NewSession creates a session with the given identity.
func NewSession(id, agentID, agentName string) *Session {
	return session.New(id, agentID, agentName)
}
