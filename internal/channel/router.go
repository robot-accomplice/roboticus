package channel

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"goboticus/internal/core"
)

// ChannelStatus tracks the health of a channel adapter.
type ChannelStatus struct {
	Name             string     `json:"name"`
	Connected        bool       `json:"connected"`
	MessagesReceived int64      `json:"messages_received"`
	MessagesSent     int64      `json:"messages_sent"`
	LastError        string     `json:"last_error,omitempty"`
	LastActivity     *time.Time `json:"last_activity,omitempty"`
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

	var messages []InboundMessage
	for name, entry := range entries {
		msg, err := entry.adapter.Recv(ctx)
		if err != nil {
			r.mu.Lock()
			entry.status.LastError = err.Error()
			r.mu.Unlock()
			continue
		}
		if msg != nil {
			r.mu.Lock()
			entry.status.MessagesReceived++
			now := time.Now()
			entry.status.LastActivity = &now
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
	entry.status.LastError = ""
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

// DeliveryQueue returns the underlying queue for inspection/admin.
func (r *Router) DeliveryQueue() *DeliveryQueue {
	return r.queue
}
