package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"nhooyr.io/websocket" //nolint:staticcheck // TODO: migrate to github.com/coder/websocket
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

// HandleWebSocket upgrades an HTTP connection to a WebSocket and streams events.
func HandleWebSocket(bus *EventBus, apiKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{ //nolint:staticcheck // TODO: migrate to github.com/coder/websocket
			OriginPatterns: []string{"localhost:*", "127.0.0.1:*", "0.0.0.0:*"},
		})
		if err != nil {
			log.Warn().Err(err).Msg("websocket upgrade failed")
			return
		}
		defer conn.CloseNow()

		ctx := r.Context()

		// Send welcome message.
		welcome, _ := json.Marshal(map[string]string{
			"type":      "connected",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
		conn.Write(ctx, websocket.MessageText, welcome)

		// Subscribe to events.
		sub := bus.Subscribe()
		defer bus.Unsubscribe(sub)

		// Ping ticker for keepalive.
		pingTicker := time.NewTicker(30 * time.Second)
		defer pingTicker.Stop()

		// Idle timeout.
		idleTimeout := 90 * time.Second
		idleTimer := time.NewTimer(idleTimeout)
		defer idleTimer.Stop()

		// Read pump (goroutine to consume client messages).
		clientDone := make(chan struct{})
		go func() {
			defer close(clientDone)
			for {
				_, msg, err := conn.Read(ctx) //nolint:staticcheck // TODO: migrate to github.com/coder/websocket
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

				// Ack client messages.
				if len(msg) <= 4096 {
					ack, _ := json.Marshal(map[string]string{"type": "ack"})
					conn.Write(ctx, websocket.MessageText, ack) //nolint:staticcheck // TODO: migrate to github.com/coder/websocket
				}
			}
		}()

		// Write pump.
		for {
			select {
			case <-ctx.Done():
				conn.Close(websocket.StatusNormalClosure, "server shutting down")
				return
			case <-clientDone:
				return
			case <-idleTimer.C:
				conn.Close(websocket.StatusNormalClosure, "idle timeout")
				return
			case <-pingTicker.C:
				conn.Ping(ctx)
			case event := <-sub:
				if err := conn.Write(ctx, websocket.MessageText, []byte(event)); err != nil { //nolint:staticcheck // TODO: migrate to github.com/coder/websocket
					return
				}
			}
		}
	}
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

// Publishf publishes a formatted string event.
func (eb *EventBus) PublishF(eventType, format string, args ...any) {
	eb.PublishEvent(eventType, fmt.Sprintf(format, args...))
}
