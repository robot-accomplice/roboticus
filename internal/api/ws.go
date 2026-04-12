package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog/log"
)

// EventBus is a publish/subscribe hub for real-time events over WebSocket.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[chan string]struct{}
	capacity    int
}

// NewEventBus creates an event bus with the given subscriber buffer capacity.
func NewEventBus(capacity int) *EventBus {
	return &EventBus{
		subscribers: make(map[chan string]struct{}),
		capacity:    capacity,
	}
}

// Publish sends an event to all subscribers. Non-blocking: drops if subscriber is full.
func (eb *EventBus) Publish(event string) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for ch := range eb.subscribers {
		select {
		case ch <- event:
		default:
			// Drop if subscriber can't keep up.
		}
	}
}

// Subscribe returns a channel that receives events. Call Unsubscribe when done.
func (eb *EventBus) Subscribe() chan string {
	ch := make(chan string, eb.capacity)
	eb.mu.Lock()
	eb.subscribers[ch] = struct{}{}
	eb.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel.
func (eb *EventBus) Unsubscribe(ch chan string) {
	eb.mu.Lock()
	delete(eb.subscribers, ch)
	eb.mu.Unlock()
	close(ch)
}

// TopicSnapshotFunc returns a JSON-serializable snapshot for a topic.
// Used to push initial state when a client subscribes.
type TopicSnapshotFunc func() any

// WSHandlerDeps bundles dependencies for the WebSocket handler.
type WSHandlerDeps struct {
	Bus       *EventBus
	APIKey    string
	Tickets   *TicketStore
	Snapshots map[string]TopicSnapshotFunc // topic → snapshot generator
}

// HandleWebSocket upgrades an HTTP connection to a WebSocket and streams events.
// Supports topic-based subscriptions: clients send {"type":"subscribe","topics":["workspace","stats"]}
// and only receive events matching their subscribed topics. Unfiltered events (like pipeline
// lifecycle events) are always delivered.
func HandleWebSocket(deps WSHandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Ticket validation: if API key is configured, require a valid ticket.
		if deps.APIKey != "" && deps.Tickets != nil {
			ticket := r.URL.Query().Get("ticket")
			if ticket == "" || !deps.Tickets.Validate(ticket) {
				http.Error(w, "invalid or expired ticket", http.StatusUnauthorized)
				return
			}
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"localhost:*", "127.0.0.1:*", "0.0.0.0:*"},
		})
		if err != nil {
			log.Warn().Err(err).Msg("websocket upgrade failed")
			return
		}
		defer func() { _ = conn.CloseNow() }()

		ctx := r.Context()
		topics := NewTopicRegistry()

		// Send welcome message.
		welcome, _ := json.Marshal(map[string]string{
			"type":      "connected",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
		_ = conn.Write(ctx, websocket.MessageText, welcome)

		// Subscribe to EventBus.
		sub := deps.Bus.Subscribe()
		defer deps.Bus.Unsubscribe(sub)

		// Ping tickers for keepalive.
		pingTicker := time.NewTicker(30 * time.Second)
		defer pingTicker.Stop()

		jsonPingTicker := time.NewTicker(30 * time.Second)
		defer jsonPingTicker.Stop()

		// Idle timeout.
		idleTimeout := 90 * time.Second
		idleTimer := time.NewTimer(idleTimeout)
		defer idleTimer.Stop()

		// writeLock serializes writes to the WebSocket connection.
		var writeLock sync.Mutex
		writeJSON := func(data []byte) error {
			writeLock.Lock()
			defer writeLock.Unlock()
			return conn.Write(ctx, websocket.MessageText, data)
		}

		// pushSnapshot sends the current state for a topic.
		pushSnapshot := func(topic string) {
			if deps.Snapshots == nil {
				return
			}
			fn, ok := deps.Snapshots[topic]
			if !ok {
				return
			}
			snap := fn()
			if snap == nil {
				return
			}
			payload, err := json.Marshal(ServerMessage{
				Type:      topic + ".snapshot",
				Data:      snap,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
			if err != nil {
				return
			}
			_ = writeJSON(payload)
		}

		// Read pump — parses client messages and routes them.
		clientDone := make(chan struct{})
		go func() {
			defer close(clientDone)
			for {
				_, msg, err := conn.Read(ctx)
				if err != nil {
					return
				}
				// Reset idle timer on any client message.
				if !idleTimer.Stop() {
					select {
					case <-idleTimer.C:
					default:
					}
				}
				idleTimer.Reset(idleTimeout)

				// Parse client message.
				var cm ClientMessage
				if err := json.Unmarshal(msg, &cm); err != nil {
					continue // ignore malformed messages
				}

				switch cm.Type {
				case MsgSubscribe:
					topics.Subscribe(cm.Topics)
					// Push initial snapshot for each newly subscribed topic.
					for _, t := range cm.Topics {
						pushSnapshot(t)
					}
					ack, _ := json.Marshal(map[string]any{
						"type": "subscribed", "topics": cm.Topics,
					})
					_ = writeJSON(ack)

				case MsgUnsubscribe:
					topics.Unsubscribe(cm.Topics)
					ack, _ := json.Marshal(map[string]any{
						"type": "unsubscribed", "topics": cm.Topics,
					})
					_ = writeJSON(ack)

				default:
					// Unknown message type — ack and ignore.
					ack, _ := json.Marshal(map[string]string{"type": "ack"})
					_ = writeJSON(ack)
				}
			}
		}()

		// Write pump — forwards EventBus events to the client.
		for {
			select {
			case <-ctx.Done():
				_ = conn.Close(websocket.StatusNormalClosure, "server shutting down")
				return
			case <-clientDone:
				return
			case <-idleTimer.C:
				_ = conn.Close(websocket.StatusNormalClosure, "idle timeout")
				return
			case <-pingTicker.C:
				_ = conn.Ping(ctx)
			case <-jsonPingTicker.C:
				jsonPing, _ := json.Marshal(map[string]string{
					"type":      "ping",
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				})
				_ = writeJSON(jsonPing)
			case event := <-sub:
				// Filter: only deliver events matching subscribed topics.
				// Pipeline lifecycle events (agent_working, stream_start, etc.) are always delivered.
				if shouldDeliverEvent(event, topics) {
					if err := writeJSON([]byte(event)); err != nil {
						return
					}
				}
			}
		}
	}
}

// shouldDeliverEvent checks if an event should be sent to this client based on topic subscriptions.
// Pipeline lifecycle events are always delivered. Topic-prefixed events (e.g., "workspace.state")
// require a matching subscription.
func shouldDeliverEvent(event string, topics *TopicRegistry) bool {
	// Fast path: try to extract the event type.
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(event), &envelope); err != nil {
		return true // deliver unparseable events (defensive)
	}

	// Pipeline lifecycle events are always delivered (no topic filter).
	switch envelope.Type {
	case "connected", "ping", "ack",
		"agent_working", "agent_idle", "agent_moved",
		"agent_started", "agent_stopped", "agent_error",
		"stream_start", "stream_chunk", "stream_end",
		"model_selection", "model_shift",
		"skill_activated", "a2a_interaction",
		"agent.reply":
		return true
	}

	// Topic-prefixed events: match "workspace.state" against "workspace" subscription.
	for _, sep := range []string{"."} {
		for i := 0; i < len(envelope.Type); i++ {
			if string(envelope.Type[i]) == sep {
				topicPrefix := envelope.Type[:i]
				if topics.Has(topicPrefix) {
					return true
				}
				break
			}
		}
	}

	// Exact topic match.
	if topics.Has(envelope.Type) {
		return true
	}

	// If no topics subscribed at all, deliver everything (backward compat).
	if len(topics.All()) == 0 {
		return true
	}

	return false
}

// PublishEvent is a convenience to publish a typed event.
func (eb *EventBus) PublishEvent(eventType string, data any) {
	payload, err := json.Marshal(map[string]any{
		"type":      eventType,
		"data":      data,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return
	}
	eb.Publish(string(payload))
}

// PublishF publishes a formatted string event.
func (eb *EventBus) PublishF(eventType, format string, args ...any) {
	eb.PublishEvent(eventType, fmt.Sprintf(format, args...))
}

// NotifyTopicChanged publishes a lightweight change notification for a topic.
// Dashboard clients subscribed to this topic will receive a "<topic>.changed"
// event and can request a fresh snapshot.
func (eb *EventBus) NotifyTopicChanged(topic string) {
	eb.PublishEvent(topic+".changed", nil)
}
