package agent

import (
	"fmt"
	"sync"
	"time"
)

// ToolClassification indicates how a tool should be treated by the approval system.
type ToolClassification int

const (
	ToolSafe    ToolClassification = iota // Execute without approval
	ToolGated                             // Requires human approval before execution
	ToolBlocked                           // Never allowed
)

// ApprovalStatus tracks the lifecycle of an approval request.
type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalDenied   ApprovalStatus = "denied"
	ApprovalTimedOut ApprovalStatus = "timed_out"
)

// ApprovalRequest represents a pending approval for a gated tool call.
type ApprovalRequest struct {
	ID        string         `json:"id"`
	ToolName  string         `json:"tool_name"`
	ToolInput string         `json:"tool_input"`
	SessionID string         `json:"session_id"`
	TurnID    string         `json:"turn_id"`
	Status    ApprovalStatus `json:"status"`
	Operator  string         `json:"operator,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	DecidedAt *time.Time     `json:"decided_at,omitempty"`
	TimeoutAt time.Time      `json:"timeout_at"`
}

// ApprovalsConfig controls the approval system.
type ApprovalsConfig struct {
	Enabled        bool     `toml:"enabled"`
	GatedTools     []string `toml:"gated_tools"`
	BlockedTools   []string `toml:"blocked_tools"`
	TimeoutSeconds int      `toml:"timeout_seconds"`
}

// ApprovalManager manages in-memory approval requests.
type ApprovalManager struct {
	config   ApprovalsConfig
	mu       sync.Mutex
	requests map[string]*ApprovalRequest
	gated    map[string]bool
	blocked  map[string]bool
}

// NewApprovalManager creates an approval manager from config.
func NewApprovalManager(cfg ApprovalsConfig) *ApprovalManager {
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 300
	}

	gated := make(map[string]bool)
	for _, t := range cfg.GatedTools {
		gated[t] = true
	}
	blocked := make(map[string]bool)
	for _, t := range cfg.BlockedTools {
		blocked[t] = true
	}

	return &ApprovalManager{
		config:   cfg,
		requests: make(map[string]*ApprovalRequest),
		gated:    gated,
		blocked:  blocked,
	}
}

// ClassifyTool returns how a tool should be handled.
func (m *ApprovalManager) ClassifyTool(name string) ToolClassification {
	if m.blocked[name] {
		return ToolBlocked
	}
	if m.gated[name] {
		return ToolGated
	}
	return ToolSafe
}

// RequestApproval creates a new pending approval request. Returns the request ID.
func (m *ApprovalManager) RequestApproval(id, toolName, toolInput, sessionID, turnID string) *ApprovalRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	req := &ApprovalRequest{
		ID:        id,
		ToolName:  toolName,
		ToolInput: toolInput,
		SessionID: sessionID,
		TurnID:    turnID,
		Status:    ApprovalPending,
		CreatedAt: now,
		TimeoutAt: now.Add(time.Duration(m.config.TimeoutSeconds) * time.Second),
	}
	m.requests[id] = req
	return req
}

// Approve marks a request as approved.
func (m *ApprovalManager) Approve(id, operator string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	req, ok := m.requests[id]
	if !ok {
		return fmt.Errorf("approval request %q not found", id)
	}
	if req.Status != ApprovalPending {
		return fmt.Errorf("request %q already decided: %s", id, req.Status)
	}

	now := time.Now()
	req.Status = ApprovalApproved
	req.Operator = operator
	req.DecidedAt = &now
	return nil
}

// Deny marks a request as denied.
func (m *ApprovalManager) Deny(id, operator, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	req, ok := m.requests[id]
	if !ok {
		return fmt.Errorf("approval request %q not found", id)
	}
	if req.Status != ApprovalPending {
		return fmt.Errorf("request %q already decided: %s", id, req.Status)
	}

	now := time.Now()
	req.Status = ApprovalDenied
	req.Operator = operator
	req.Reason = reason
	req.DecidedAt = &now
	return nil
}

// ListPending returns all pending approval requests.
func (m *ApprovalManager) ListPending() []*ApprovalRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*ApprovalRequest
	for _, req := range m.requests {
		if req.Status == ApprovalPending {
			result = append(result, req)
		}
	}
	return result
}

// ListAll returns all approval requests.
func (m *ApprovalManager) ListAll() []*ApprovalRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*ApprovalRequest, 0, len(m.requests))
	for _, req := range m.requests {
		result = append(result, req)
	}
	return result
}

// Get returns a specific approval request.
func (m *ApprovalManager) Get(id string) *ApprovalRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests[id]
}

// ExpireTimedOut transitions pending requests past their timeout to TimedOut.
func (m *ApprovalManager) ExpireTimedOut() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	count := 0
	for _, req := range m.requests {
		if req.Status == ApprovalPending && now.After(req.TimeoutAt) {
			req.Status = ApprovalTimedOut
			t := now
			req.DecidedAt = &t
			count++
		}
	}
	return count
}

// ListAllJSON returns all requests as generic maps (for use by route handlers).
func (m *ApprovalManager) ListAllJSON() []map[string]any {
	reqs := m.ListAll()
	result := make([]map[string]any, len(reqs))
	for i, r := range reqs {
		result[i] = r.toMap()
	}
	return result
}

// ListPendingJSON returns pending requests as generic maps.
func (m *ApprovalManager) ListPendingJSON() []map[string]any {
	reqs := m.ListPending()
	result := make([]map[string]any, len(reqs))
	for i, r := range reqs {
		result[i] = r.toMap()
	}
	return result
}

// GetJSON returns a request as a generic map, or nil if not found.
func (m *ApprovalManager) GetJSON(id string) map[string]any {
	req := m.Get(id)
	if req == nil {
		return nil
	}
	return req.toMap()
}

func (r *ApprovalRequest) toMap() map[string]any {
	m := map[string]any{
		"id":         r.ID,
		"tool_name":  r.ToolName,
		"tool_input": r.ToolInput,
		"session_id": r.SessionID,
		"turn_id":    r.TurnID,
		"status":     string(r.Status),
		"created_at": r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"timeout_at": r.TimeoutAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if r.Operator != "" {
		m["operator"] = r.Operator
	}
	if r.Reason != "" {
		m["reason"] = r.Reason
	}
	if r.DecidedAt != nil {
		m["decided_at"] = r.DecidedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return m
}

// ClearDecided removes all non-pending requests from the in-memory store.
func (m *ApprovalManager) ClearDecided() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for id, req := range m.requests {
		if req.Status != ApprovalPending {
			delete(m.requests, id)
			count++
		}
	}
	return count
}
