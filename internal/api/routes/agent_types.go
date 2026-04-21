package routes

// agentMessageRequest is the JSON body for POST /api/agent/message.
// Fields align with the Rust reference request schema.
type agentMessageRequest struct {
	Content   string `json:"content"`
	SessionID string `json:"session_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	Model     string `json:"model,omitempty"`
	Channel   string `json:"channel,omitempty"`
	SenderID  string `json:"sender_id,omitempty"`
	PeerID    string `json:"peer_id,omitempty"`
	GroupID   string `json:"group_id,omitempty"`
	IsGroup   bool   `json:"is_group,omitempty"`
	NoCache   bool   `json:"no_cache,omitempty"`
	NoEscalate bool  `json:"no_escalate,omitempty"`
}

// agentMessageResponse wraps the pipeline Outcome with Rust-parity response fields.
type agentMessageResponse struct {
	SessionID          string  `json:"session_id"`
	MessageID          string  `json:"message_id"`
	Content            string  `json:"content"`
	Model              string  `json:"model,omitempty"`
	TokensIn           int     `json:"tokens_in,omitempty"`
	TokensOut          int     `json:"tokens_out,omitempty"`
	ReactTurns         int     `json:"react_turns,omitempty"`
	FromCache          bool    `json:"from_cache,omitempty"`
	Cached             bool    `json:"cached,omitempty"`
	TokensSaved        int     `json:"tokens_saved,omitempty"`
	Cost               float64 `json:"cost,omitempty"`
	ModelShiftFrom     string  `json:"model_shift_from,omitempty"`
	UserMessageID      string  `json:"user_message_id,omitempty"`
	AssistantMessageID string  `json:"assistant_message_id,omitempty"`
}
