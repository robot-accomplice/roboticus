package core

import (
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// --- LogSubscriber ---

// LogSubscriber logs every error event with structured zerolog fields.
type LogSubscriber struct{}

// HandleError logs the event at the appropriate zerolog level.
func (s *LogSubscriber) HandleError(event ErrorEvent) {
	var e *zerolog.Event
	switch event.Severity {
	case SevDebug:
		e = log.Debug()
	case SevInfo:
		e = log.Info()
	case SevWarning:
		e = log.Warn()
	case SevError, SevCritical:
		e = log.Error()
	default:
		e = log.Warn()
	}

	e = e.Err(event.Err).
		Str("subsystem", event.Subsystem).
		Str("op", event.Op).
		Str("severity", event.Severity.String())

	if event.SessionID != "" {
		e = e.Str("session_id", event.SessionID)
	}
	if event.ChatID != "" {
		e = e.Str("chat_id", event.ChatID)
	}
	if event.Platform != "" {
		e = e.Str("platform", event.Platform)
	}
	if event.Model != "" {
		e = e.Str("model", event.Model)
	}
	if event.Retryable {
		e = e.Bool("retryable", true)
	}

	e.Msg("errbus")
}

// --- MetricSubscriber ---

// MetricSubscriber increments counters by subsystem:severity.
// Thread-safe; call Snapshot() to read current counts.
type MetricSubscriber struct {
	mu     sync.Mutex
	counts map[string]int64
}

// NewMetricSubscriber creates a MetricSubscriber.
func NewMetricSubscriber() *MetricSubscriber {
	return &MetricSubscriber{counts: make(map[string]int64)}
}

// HandleError increments the counter for subsystem:severity.
func (m *MetricSubscriber) HandleError(event ErrorEvent) {
	key := event.Subsystem + ":" + event.Severity.String()
	m.mu.Lock()
	m.counts[key]++
	m.mu.Unlock()
}

// Snapshot returns a copy of current error counts.
func (m *MetricSubscriber) Snapshot() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]int64, len(m.counts))
	for k, v := range m.counts {
		out[k] = v
	}
	return out
}

// --- RingBufferSubscriber ---

// RingBufferSubscriber captures the last N error events for dashboard display.
type RingBufferSubscriber struct {
	mu   sync.Mutex
	buf  []ErrorEvent
	cap  int
	head int
	full bool
}

// NewRingBufferSubscriber creates a ring buffer that holds the last cap events.
func NewRingBufferSubscriber(cap int) *RingBufferSubscriber {
	if cap < 1 {
		cap = 1000
	}
	return &RingBufferSubscriber{
		buf: make([]ErrorEvent, cap),
		cap: cap,
	}
}

// HandleError stores the event in the ring buffer.
func (r *RingBufferSubscriber) HandleError(event ErrorEvent) {
	r.mu.Lock()
	r.buf[r.head] = event
	r.head = (r.head + 1) % r.cap
	if r.head == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

// Recent returns up to the last N events, newest first.
func (r *RingBufferSubscriber) Recent(n int) []ErrorEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	var total int
	if r.full {
		total = r.cap
	} else {
		total = r.head
	}
	if total == 0 {
		return nil
	}
	if n > total {
		n = total
	}

	out := make([]ErrorEvent, n)
	for i := range n {
		idx := (r.head - 1 - i + r.cap) % r.cap
		out[i] = r.buf[idx]
	}
	return out
}
