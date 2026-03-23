package channel

import (
	"context"
	"sync"
)

// WebConfig holds web/WebSocket channel configuration.
type WebConfig struct {
	InboundBufferSize  int `mapstructure:"inbound_buffer_size"`  // default 256
	OutboundBufferSize int `mapstructure:"outbound_buffer_size"` // default 64
}

// WebAdapter implements Adapter for the web/WebSocket channel.
// Uses Go channels for inbound (mpsc) and outbound (broadcast via subscribers).
type WebAdapter struct {
	inbound     chan InboundMessage
	mu          sync.RWMutex
	subscribers map[chan OutboundMessage]struct{}
}

// NewWebAdapter creates a web channel adapter.
func NewWebAdapter(cfg WebConfig) *WebAdapter {
	if cfg.InboundBufferSize <= 0 {
		cfg.InboundBufferSize = 256
	}
	if cfg.OutboundBufferSize <= 0 {
		cfg.OutboundBufferSize = 64
	}
	return &WebAdapter{
		inbound:     make(chan InboundMessage, cfg.InboundBufferSize),
		subscribers: make(map[chan OutboundMessage]struct{}),
	}
}

func (w *WebAdapter) PlatformName() string { return "web" }

// PushMessage adds an inbound message from a WebSocket handler.
func (w *WebAdapter) PushMessage(msg InboundMessage) bool {
	select {
	case w.inbound <- msg:
		return true
	default:
		return false
	}
}

// Recv returns the next inbound message (non-blocking).
func (w *WebAdapter) Recv(ctx context.Context) (*InboundMessage, error) {
	select {
	case msg := <-w.inbound:
		return &msg, nil
	default:
		return nil, nil
	}
}

// Send broadcasts the outbound message to all subscribers (non-blocking per subscriber).
func (w *WebAdapter) Send(ctx context.Context, msg OutboundMessage) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for ch := range w.subscribers {
		select {
		case ch <- msg:
		default:
			// Slow subscriber, skip.
		}
	}
	return nil
}

// Subscribe returns a channel that receives outbound messages.
func (w *WebAdapter) Subscribe(bufSize int) chan OutboundMessage {
	if bufSize <= 0 {
		bufSize = 64
	}
	ch := make(chan OutboundMessage, bufSize)
	w.mu.Lock()
	w.subscribers[ch] = struct{}{}
	w.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel.
func (w *WebAdapter) Unsubscribe(ch chan OutboundMessage) {
	w.mu.Lock()
	delete(w.subscribers, ch)
	w.mu.Unlock()
	close(ch)
}
