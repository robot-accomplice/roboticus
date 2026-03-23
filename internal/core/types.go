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
type AgentState int

const (
	AgentStateIdle AgentState = iota
	AgentStateThinking
	AgentStateActing
	AgentStateObserving
	AgentStatePersisting
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
)

func (f APIFormat) String() string {
	switch f {
	case APIFormatOpenAI:
		return "openai"
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
	Platform   string `json:"platform"`
	ChannelID  string `json:"channel_id"`
	UserID     string `json:"user_id"`
	Username   string `json:"username,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	ThreadID   string `json:"thread_id,omitempty"`
	IsGroup    bool   `json:"is_group"`
	Authority  AuthorityLevel `json:"authority"`
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
