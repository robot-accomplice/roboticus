package channel

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
)

// ChannelHealth represents the connection health state (Rust parity).
type ChannelHealth int

const (
	HealthConnected    ChannelHealth = iota // Healthy, recent activity
	HealthDegraded                          // Errors occurring but still partially functional
	HealthDisconnected                      // No successful communication
)

func (h ChannelHealth) String() string {
	switch h {
	case HealthConnected:
		return "connected"
	case HealthDegraded:
		return "degraded"
	case HealthDisconnected:
		return "disconnected"
	default:
		return "unknown"
	}
}

// ChannelStatus tracks the health of a channel adapter.
type ChannelStatus struct {
	Name             string        `json:"name"`
	Connected        bool          `json:"connected"`
	Health           ChannelHealth `json:"health"`
	MessagesReceived int64         `json:"messages_received"`
	MessagesSent     int64         `json:"messages_sent"`
	ErrorCount       int64         `json:"error_count"`
	LastError        string        `json:"last_error,omitempty"`
	LastActivity     *time.Time    `json:"last_activity,omitempty"`
	LastSuccessfulAt *time.Time    `json:"last_successful_at,omitempty"`
}

type channelEntry struct {
	adapter Adapter
	status  ChannelStatus
}

// Router dispatches messages to/from channel adapters.
type Router struct {
	mu       sync.Mutex
	channels map[string]*channelEntry
	queue    *DeliveryQueue
}

// NewRouter creates a channel router with a delivery queue.
func NewRouter(queue *DeliveryQueue) *Router {
	return &Router{
		channels: make(map[string]*channelEntry),
		queue:    queue,
	}
}

// Register adds a channel adapter.
func (r *Router) Register(adapter Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := adapter.PlatformName()
	r.channels[name] = &channelEntry{
		adapter: adapter,
		status: ChannelStatus{
			Name:      name,
			Connected: true,
		},
	}
	log.Info().Str("channel", name).Msg("channel registered")
}

// PollAll polls all registered adapters for inbound messages.
func (r *Router) PollAll(ctx context.Context) []InboundMessage {
	r.mu.Lock()
	entries := make(map[string]*channelEntry, len(r.channels))
	for k, v := range r.channels {
		entries[k] = v
	}
	r.mu.Unlock()

	if len(entries) == 0 {
		return nil
	}

	var messages []InboundMessage
	for name, entry := range entries {
		msg, err := entry.adapter.Recv(ctx)
		if err != nil {
			r.mu.Lock()
			entry.status.LastError = err.Error()
			entry.status.ErrorCount++
			r.recordHealthTransition(entry)
			r.mu.Unlock()
			continue
		}
		if msg != nil {
			r.mu.Lock()
			entry.status.MessagesReceived++
			now := time.Now()
			entry.status.LastActivity = &now
			entry.status.LastSuccessfulAt = &now
			entry.status.LastError = "" // Clear error on success.
			entry.status.Health = HealthConnected
			entry.status.Connected = true
			r.mu.Unlock()

			msg.Platform = name
			messages = append(messages, *msg)
		}
	}
	return messages
}

// SendTo sends a message through a specific channel adapter.
// On transient failure, enqueues for retry. Permanent failures are logged.
func (r *Router) SendTo(ctx context.Context, platform string, msg OutboundMessage) error {
	r.mu.Lock()
	entry, ok := r.channels[platform]
	r.mu.Unlock()

	if !ok {
		return core.NewError(core.ErrChannel, "unknown channel: "+platform)
	}

	err := entry.adapter.Send(ctx, msg)
	if err != nil {
		errStr := err.Error()

		r.mu.Lock()
		entry.status.LastError = errStr
		entry.status.ErrorCount++
		r.recordHealthTransition(entry)
		r.mu.Unlock()

		if isPermanentError(errStr) {
			log.Warn().Str("channel", platform).Str("error", errStr).Msg("permanent send failure")
			return err
		}

		// Transient failure — enqueue for retry.
		r.queue.Enqueue(platform, msg.RecipientID, msg.Content)
		log.Debug().Str("channel", platform).Msg("message queued for retry")
		return nil
	}

	r.mu.Lock()
	entry.status.MessagesSent++
	now := time.Now()
	entry.status.LastActivity = &now
	entry.status.LastSuccessfulAt = &now
	entry.status.LastError = ""
	entry.status.Health = HealthConnected
	entry.status.Connected = true
	r.mu.Unlock()

	return nil
}

// SendReply is a convenience wrapper for replying to a platform.
func (r *Router) SendReply(ctx context.Context, platform, recipientID, content string) error {
	formatted := FormatFor(platform).Format(content)
	return r.SendTo(ctx, platform, OutboundMessage{
		Content:     formatted,
		RecipientID: recipientID,
		Platform:    platform,
	})
}

// TypingIndicator is an optional interface for adapters that support typing indicators.
type TypingIndicator interface {
	SendTyping(ctx context.Context, chatID string)
}

// SendTypingIndicator sends a typing/thinking indicator if the platform supports it.
func (r *Router) SendTypingIndicator(ctx context.Context, platform, chatID string) {
	r.mu.Lock()
	entry, ok := r.channels[platform]
	r.mu.Unlock()
	if !ok {
		return
	}
	if ti, ok := entry.adapter.(TypingIndicator); ok {
		ti.SendTyping(ctx, chatID)
	}
}

// Status returns the health of all registered channels.
func (r *Router) Status() []ChannelStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	var statuses []ChannelStatus
	for _, entry := range r.channels {
		statuses = append(statuses, entry.status)
	}
	return statuses
}

// ChannelNames returns the names of all registered channels.
func (r *Router) ChannelNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.channels))
	for name := range r.channels {
		names = append(names, name)
	}
	return names
}

// Adapters returns the adapter map for the delivery worker.
func (r *Router) Adapters() map[string]Adapter {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[string]Adapter, len(r.channels))
	for name, entry := range r.channels {
		result[name] = entry.adapter
	}
	return result
}

// GetAdapter returns the adapter registered under the given platform name, or nil.
func (r *Router) GetAdapter(platform string) Adapter {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.channels[platform]
	if !ok {
		return nil
	}
	return entry.adapter
}

// DeliveryQueue returns the underlying queue for inspection/admin.
func (r *Router) DeliveryQueue() *DeliveryQueue {
	return r.queue
}

// recordHealthTransition updates the health state based on error patterns.
// Must be called with r.mu held.
func (r *Router) recordHealthTransition(entry *channelEntry) {
	// Transition: Connected → Degraded after 3+ errors, Degraded → Disconnected after 10+.
	switch {
	case entry.status.ErrorCount >= 10:
		entry.status.Health = HealthDisconnected
		entry.status.Connected = false
	case entry.status.ErrorCount >= 3:
		entry.status.Health = HealthDegraded
		entry.status.Connected = true // Still partially functional
	default:
		// Keep current health (don't downgrade on single error)
	}
}

// RecordReceived explicitly records an inbound message (for adapters that push).
func (r *Router) RecordReceived(platform string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, ok := r.channels[platform]; ok {
		entry.status.MessagesReceived++
		now := time.Now()
		entry.status.LastActivity = &now
		entry.status.LastSuccessfulAt = &now
		entry.status.LastError = ""
		entry.status.Health = HealthConnected
		entry.status.Connected = true
	}
}

// RecordSent explicitly records an outbound message.
func (r *Router) RecordSent(platform string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, ok := r.channels[platform]; ok {
		entry.status.MessagesSent++
		now := time.Now()
		entry.status.LastActivity = &now
		entry.status.LastSuccessfulAt = &now
		entry.status.Health = HealthConnected
	}
}

// RecordError explicitly records a processing error.
func (r *Router) RecordError(platform, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, ok := r.channels[platform]; ok {
		entry.status.LastError = errMsg
		entry.status.ErrorCount++
		r.recordHealthTransition(entry)
	}
}
