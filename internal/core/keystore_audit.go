package core

import (
	"encoding/json"
	"time"
)

// AuditEvent records a keystore operation for the audit trail.
// Matches the Rust reference's append_audit_event() pattern.
type AuditEvent struct {
	Op        string          `json:"op"`  // "set", "delete", "get", "save", "rekey", "initialize"
	Key       string          `json:"key"` // secret name (never the value)
	Timestamp time.Time       `json:"timestamp"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

// auditLog is the in-memory audit trail. Protected by the Keystore's mu lock.
// Not exported — access via Keystore.AuditLog().
type auditLog struct {
	events []AuditEvent
}

// appendAuditEvent records an operation in the audit log.
// Must be called while holding the Keystore's write lock.
func (ks *Keystore) appendAuditEvent(op, key string, meta map[string]interface{}) {
	var metaJSON json.RawMessage
	if meta != nil {
		if data, err := json.Marshal(meta); err == nil {
			metaJSON = data
		}
	}
	if ks.audit == nil {
		ks.audit = &auditLog{}
	}
	ks.audit.events = append(ks.audit.events, AuditEvent{
		Op:        op,
		Key:       key,
		Timestamp: time.Now(),
		Metadata:  metaJSON,
	})
}

// AuditLog returns a copy of the audit trail.
func (ks *Keystore) AuditLog() []AuditEvent {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	if ks.audit == nil {
		return nil
	}
	out := make([]AuditEvent, len(ks.audit.events))
	copy(out, ks.audit.events)
	return out
}
