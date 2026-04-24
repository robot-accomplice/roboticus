package core

import (
	"encoding/json"
	"time"
)

// SurvivalTier represents the agent's operational state based on financial resources.
type SurvivalTier int

const (
	SurvivalTierDead SurvivalTier = iota
	SurvivalTierSurvival
	SurvivalTierStable
	SurvivalTierGrowth
	SurvivalTierThriving
)

// SurvivalTierFromBalance computes the survival tier from the agent's USD balance
// and how long it has been below zero. Matches the Rust reference thresholds:
// Dead: balance < 0 AND hours_below_zero >= 0.999
// Survival: balance < $0.10
// Stable: balance < $0.50
// Growth: balance < $5.00
// Thriving: balance >= $5.00
func SurvivalTierFromBalance(usd float64, hoursBelowZero float64) SurvivalTier {
	if usd < 0 && hoursBelowZero >= 0.999 {
		return SurvivalTierDead
	}
	if usd < 0.10 {
		return SurvivalTierSurvival
	}
	if usd < 0.50 {
		return SurvivalTierStable
	}
	if usd < 5.00 {
		return SurvivalTierGrowth
	}
	return SurvivalTierThriving
}

func (s SurvivalTier) String() string {
	switch s {
	case SurvivalTierDead:
		return "dead"
	case SurvivalTierSurvival:
		return "survival"
	case SurvivalTierStable:
		return "stable"
	case SurvivalTierGrowth:
		return "growth"
	case SurvivalTierThriving:
		return "thriving"
	default:
		return "unknown"
	}
}

// AgentState represents the current state of the ReAct loop.
//
// NOTE: Go intentionally uses 7 ReAct loop states (Idle/Thinking/Acting/Observing/
// Persisting/Reflecting/Done) while the Rust reference uses 5 lifecycle states
// (Setup/Waking/Running/Sleeping/Dead). This divergence is by design: Go models
// the inner ReAct state machine, Rust models the outer agent lifecycle. Both are
// valid views.
type AgentState int

const (
	AgentStateIdle AgentState = iota
	AgentStateThinking
	AgentStateActing
	AgentStateObserving
	AgentStatePersisting
	AgentStateReflecting
	AgentStateDone
)

func (s AgentState) String() string {
	switch s {
	case AgentStateIdle:
		return "idle"
	case AgentStateThinking:
		return "thinking"
	case AgentStateActing:
		return "acting"
	case AgentStateObserving:
		return "observing"
	case AgentStatePersisting:
		return "persisting"
	case AgentStateReflecting:
		return "reflecting"
	case AgentStateDone:
		return "done"
	default:
		return "unknown"
	}
}

// ModelTier classifies model capability levels for routing decisions.
type ModelTier int

const (
	ModelTierSmall ModelTier = iota
	ModelTierMedium
	ModelTierLarge
	ModelTierFrontier
)

func (t ModelTier) String() string {
	switch t {
	case ModelTierSmall:
		return "small"
	case ModelTierMedium:
		return "medium"
	case ModelTierLarge:
		return "large"
	case ModelTierFrontier:
		return "frontier"
	default:
		return "unknown"
	}
}

// APIFormat identifies the LLM provider API format.
type APIFormat int

const (
	APIFormatOpenAI APIFormat = iota
	APIFormatAnthropic
	APIFormatOllama
	APIFormatGoogle
	// APIFormatOpenAICompletions distinguishes the /v1/chat/completions endpoint
	// from the /v1/responses endpoint. Legacy code using APIFormatOpenAI is treated
	// as completions for backward compatibility.
	APIFormatOpenAICompletions
	// APIFormatOpenAIResponses is the newer OpenAI /v1/responses endpoint.
	APIFormatOpenAIResponses
)

func (f APIFormat) String() string {
	switch f {
	case APIFormatOpenAI, APIFormatOpenAICompletions:
		return "openai"
	case APIFormatOpenAIResponses:
		return "openai_responses"
	case APIFormatAnthropic:
		return "anthropic"
	case APIFormatOllama:
		return "ollama"
	case APIFormatGoogle:
		return "google"
	default:
		return "unknown"
	}
}

// IsOpenAI returns true if the format is any OpenAI variant.
func (f APIFormat) IsOpenAI() bool {
	return f == APIFormatOpenAI || f == APIFormatOpenAICompletions || f == APIFormatOpenAIResponses
}

// RiskLevel classifies the risk of a tool execution.
type RiskLevel int

const (
	RiskLevelSafe RiskLevel = iota
	RiskLevelCaution
	RiskLevelDangerous
	RiskLevelForbidden
)

func (r RiskLevel) String() string {
	switch r {
	case RiskLevelSafe:
		return "safe"
	case RiskLevelCaution:
		return "caution"
	case RiskLevelDangerous:
		return "dangerous"
	case RiskLevelForbidden:
		return "forbidden"
	default:
		return "unknown"
	}
}

// AuthorityLevel represents the trust level of the message source.
//
// This is the Go equivalent of Rust's InputAuthority enum. The iota ordering
// (External=0 < Peer=1 < SelfGenerated=2 < Creator=3) enables min/max
// comparison for the SecurityClaim composition algorithm:
//
//	effective = min(max(grants...), min(ceilings...))
type AuthorityLevel int

const (
	AuthorityExternal AuthorityLevel = iota
	AuthorityPeer
	AuthoritySelfGenerated
	AuthorityCreator
)

func (a AuthorityLevel) String() string {
	switch a {
	case AuthorityExternal:
		return "external"
	case AuthorityPeer:
		return "peer"
	case AuthoritySelfGenerated:
		return "self_generated"
	case AuthorityCreator:
		return "creator"
	default:
		return "unknown"
	}
}

// PolicyDecision is the outcome of a policy evaluation.
type PolicyDecision int

const (
	PolicyAllow PolicyDecision = iota
	PolicyDeny
	PolicyEscalate
)

// DeliveryStatus tracks message delivery state.
type DeliveryStatus int

const (
	DeliveryPending DeliveryStatus = iota
	DeliveryInFlight
	DeliveryDelivered
	DeliveryFailed
	DeliveryDeadLetter
)

func (d DeliveryStatus) String() string {
	switch d {
	case DeliveryPending:
		return "pending"
	case DeliveryInFlight:
		return "in_flight"
	case DeliveryDelivered:
		return "delivered"
	case DeliveryFailed:
		return "failed"
	case DeliveryDeadLetter:
		return "dead_letter"
	default:
		return "unknown"
	}
}

// InferenceMode controls how the LLM response is delivered.
type InferenceMode int

const (
	InferenceModeStandard InferenceMode = iota
	InferenceModeStreaming
)

// MemoryTier identifies the type of memory store.
type MemoryTier int

const (
	MemoryTierWorking MemoryTier = iota
	MemoryTierEpisodic
	MemoryTierSemantic
	MemoryTierProcedural
	MemoryTierRelationship
)

func (m MemoryTier) String() string {
	switch m {
	case MemoryTierWorking:
		return "working"
	case MemoryTierEpisodic:
		return "episodic"
	case MemoryTierSemantic:
		return "semantic"
	case MemoryTierProcedural:
		return "procedural"
	case MemoryTierRelationship:
		return "relationship"
	default:
		return "unknown"
	}
}

// ChannelContext carries per-request channel metadata through the pipeline.
type ChannelContext struct {
	Platform  string         `json:"platform"`
	ChannelID string         `json:"channel_id"`
	UserID    string         `json:"user_id"`
	Username  string         `json:"username,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	ThreadID  string         `json:"thread_id,omitempty"`
	IsGroup   bool           `json:"is_group"`
	Authority AuthorityLevel `json:"authority"`
}

// InboundMessage is the normalized message received from any channel.
type InboundMessage struct {
	ID          string            `json:"id"`
	Content     string            `json:"content"`
	Channel     ChannelContext    `json:"channel"`
	Attachments []MediaAttachment `json:"attachments,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
	ReplyTo     string            `json:"reply_to,omitempty"`
	Metadata    json.RawMessage   `json:"metadata,omitempty"`
}

// OutboundMessage is the normalized message to send to any channel.
type OutboundMessage struct {
	Content     string            `json:"content"`
	Channel     ChannelContext    `json:"channel"`
	Attachments []MediaAttachment `json:"attachments,omitempty"`
	ReplyTo     string            `json:"reply_to,omitempty"`
	Metadata    json.RawMessage   `json:"metadata,omitempty"`
}

// MediaAttachment represents a media file attached to a message.
type MediaAttachment struct {
	Type     MediaType `json:"type"`
	URL      string    `json:"url,omitempty"`
	Data     []byte    `json:"data,omitempty"`
	MimeType string    `json:"mime_type,omitempty"`
	Filename string    `json:"filename,omitempty"`
	Size     int64     `json:"size,omitempty"`
}

// MediaType classifies attachment types.
type MediaType int

const (
	MediaTypeImage MediaType = iota
	MediaTypeAudio
	MediaTypeVideo
	MediaTypeDocument
)

// ThreatScore is the result of injection defense scanning (0.0 to 1.0).
type ThreatScore float64

const (
	ThreatThresholdClean   ThreatScore = 0.3
	ThreatThresholdCaution ThreatScore = 0.7
)

// IsClean returns true if the score indicates no threat.
func (t ThreatScore) IsClean() bool { return t < ThreatThresholdClean }

// IsCaution returns true if the score warrants review.
func (t ThreatScore) IsCaution() bool { return t >= ThreatThresholdClean && t < ThreatThresholdCaution }

// IsBlocked returns true if the score should block the request.
func (t ThreatScore) IsBlocked() bool { return t >= ThreatThresholdCaution }

// DowngradeCeiling caps the threat score at the given ceiling.
// This implements the Rust threat_caution_ceiling mechanism: if a ceiling is
// applied and the score exceeds it, the effective score is reduced to the
// ceiling value, preventing over-aggressive blocking.
func (t ThreatScore) DowngradeCeiling(ceiling ThreatScore) ThreatScore {
	if t > ceiling {
		return ceiling
	}
	return t
}
