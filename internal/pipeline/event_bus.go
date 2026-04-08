package pipeline

import (
	"sync"
	"time"
)

// PipelineEvent represents a significant event during pipeline execution (Wave 8, #85).
type PipelineEvent struct {
	Stage     string      `json:"stage"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data,omitempty"`
}

// EventBus provides a publish-subscribe mechanism for pipeline events.
// Subscribers receive events on buffered channels. Slow subscribers
// may miss events (non-blocking send).
type EventBus struct {
	mu          sync.RWMutex
	subscribers []chan PipelineEvent
}

// NewEventBus creates an empty event bus.
func NewEventBus() *EventBus {
	return &EventBus{}
}

// Subscribe returns a buffered channel that receives pipeline events.
// The buffer size controls how many events can queue before drops occur.
func (eb *EventBus) Subscribe(bufSize int) chan PipelineEvent {
	if bufSize < 1 {
		bufSize = 64
	}
	ch := make(chan PipelineEvent, bufSize)
	eb.mu.Lock()
	eb.subscribers = append(eb.subscribers, ch)
	eb.mu.Unlock()
	return ch
}

// Unsubscribe removes a channel from the subscriber list and closes it.
func (eb *EventBus) Unsubscribe(ch chan PipelineEvent) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	for i, sub := range eb.subscribers {
		if sub == ch {
			eb.subscribers = append(eb.subscribers[:i], eb.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// Publish sends an event to all subscribers. Non-blocking: slow subscribers
// will miss events rather than blocking the pipeline.
func (eb *EventBus) Publish(event PipelineEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for _, ch := range eb.subscribers {
		select {
		case ch <- event:
		default:
			// Subscriber channel full, drop event.
		}
	}
}

// SubscriberCount returns the current number of subscribers.
func (eb *EventBus) SubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.subscribers)
}
