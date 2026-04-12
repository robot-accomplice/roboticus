package api

import (
	"encoding/json"
	"sync"
)

// WebSocket message types — client → server.
const (
	MsgSubscribe   = "subscribe"
	MsgUnsubscribe = "unsubscribe"
)

// WebSocket topic names — correspond to dashboard data domains.
const (
	TopicAgentStatus    = "agent.status"
	TopicWorkspace      = "workspace"
	TopicModels         = "models"
	TopicSessions       = "sessions"
	TopicMemory         = "memory"
	TopicStats          = "stats"
	TopicTraces         = "traces"
	TopicSkills         = "skills"
	TopicCron           = "cron"
	TopicConfig         = "config"
	TopicPlugins        = "plugins"
	TopicChannels       = "channels"
	TopicWallet         = "wallet"
	TopicRoster         = "roster"
	TopicSubagents      = "subagents"
	TopicServices       = "services"
	TopicRecommendations = "recommendations"
	TopicMCP            = "mcp"
	TopicBreakers       = "breakers"
	TopicRouting        = "routing"
	TopicApprovals      = "approvals"
	TopicLogs           = "logs"
)

// ClientMessage is a typed envelope for client → server WebSocket messages.
type ClientMessage struct {
	Type   string          `json:"type"`
	Topics []string        `json:"topics,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// ServerMessage is a typed envelope for server → client push messages.
type ServerMessage struct {
	Type      string `json:"type"`
	Data      any    `json:"data,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// TopicRegistry tracks per-connection topic subscriptions.
type TopicRegistry struct {
	mu     sync.RWMutex
	topics map[string]struct{}
}

// NewTopicRegistry creates an empty topic registry.
func NewTopicRegistry() *TopicRegistry {
	return &TopicRegistry{topics: make(map[string]struct{})}
}

// Subscribe adds topics to the registry.
func (tr *TopicRegistry) Subscribe(topics []string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	for _, t := range topics {
		tr.topics[t] = struct{}{}
	}
}

// Unsubscribe removes topics from the registry.
func (tr *TopicRegistry) Unsubscribe(topics []string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	for _, t := range topics {
		delete(tr.topics, t)
	}
}

// Has returns true if the given topic is subscribed.
func (tr *TopicRegistry) Has(topic string) bool {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	_, ok := tr.topics[topic]
	return ok
}

// All returns a copy of all subscribed topics.
func (tr *TopicRegistry) All() []string {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	out := make([]string, 0, len(tr.topics))
	for t := range tr.topics {
		out = append(out, t)
	}
	return out
}
