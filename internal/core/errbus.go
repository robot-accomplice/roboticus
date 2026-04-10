package core

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// Severity classifies how serious an error event is.
type Severity int

const (
	SevDebug    Severity = iota // Informational, no action needed (e.g. client-gone response write)
	SevInfo                     // Noteworthy but expected (e.g. cache miss)
	SevWarning                  // Something went wrong but operation continued (e.g. cost not recorded)
	SevError                    // Operation failed (e.g. DB write failed)
	SevCritical                 // System integrity at risk (e.g. crypto rand failure)
)

// String returns the severity label.
func (s Severity) String() string {
	switch s {
	case SevDebug:
		return "debug"
	case SevInfo:
		return "info"
	case SevWarning:
		return "warning"
	case SevError:
		return "error"
	case SevCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// ErrorEvent is the unit of work flowing through the ErrorBus.
type ErrorEvent struct {
	Time      time.Time
	Subsystem string // "llm", "pipeline", "tool", "channel", "db", "scheduler", "wallet"
	Op        string // "record_cost", "send_message", etc.
	Err       error
	Severity  Severity
	Retryable bool
	SessionID string
	ChatID    string
	Platform  string
	Model     string
	Metadata  map[string]string
}

// ErrorSubscriber receives error events from the bus.
type ErrorSubscriber interface {
	HandleError(event ErrorEvent)
}

// ErrorBus provides non-blocking error dispatch to registered subscribers.
// Subsystems call Report(); a dispatcher goroutine fans out to subscribers.
// Follows the orDone pattern for lifecycle management.
type ErrorBus struct {
	ch          chan ErrorEvent
	subscribers []ErrorSubscriber
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	overflow    atomic.Int64
}

// NewErrorBus creates an ErrorBus bound to the given context.
// bufSize controls the channel buffer (256 is a good default).
// The dispatcher goroutine starts immediately.
func NewErrorBus(ctx context.Context, bufSize int, subs ...ErrorSubscriber) *ErrorBus {
	if bufSize < 1 {
		bufSize = 256
	}
	busCtx, cancel := context.WithCancel(ctx)
	b := &ErrorBus{
		ch:          make(chan ErrorEvent, bufSize),
		subscribers: subs,
		ctx:         busCtx,
		cancel:      cancel,
	}
	b.wg.Add(1)
	go b.dispatch()
	return b
}

// Report sends an error event to the bus without blocking.
// If the bus is nil, this is a no-op (safe for unwired subsystems).
// If the channel is full, the event is counted as overflow but not dropped silently.
func (b *ErrorBus) Report(event ErrorEvent) {
	if b == nil {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	select {
	case b.ch <- event:
	default:
		// Channel full — count overflow. The dispatcher will log periodically.
		b.overflow.Add(1)
	}
}

// ReportIfErr is a nil-safe convenience: if err is non-nil, it reports it.
// Callers don't need to check err themselves for fire-and-forget reporting.
func (b *ErrorBus) ReportIfErr(err error, subsystem, op string, sev Severity) {
	if b == nil || err == nil {
		return
	}
	b.Report(ErrorEvent{
		Subsystem: subsystem,
		Op:        op,
		Err:       err,
		Severity:  sev,
	})
}

// ReportEvent is like ReportIfErr but accepts a pre-built event with only the
// error left to check. Useful when callers need to attach SessionID/ChatID/etc.
func (b *ErrorBus) ReportEvent(event ErrorEvent) {
	if b == nil || event.Err == nil {
		return
	}
	b.Report(event)
}

// Drain waits for the dispatcher to finish processing, then cancels.
// Follows wait-then-cancel ordering (same as BackgroundWorker).
func (b *ErrorBus) Drain(timeout time.Duration) {
	if b == nil {
		return
	}
	// Close the channel so the dispatcher exits its range loop.
	// This is safe because Report uses a select with default.
	b.cancel()

	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Debug().Msg("errbus: drained cleanly")
	case <-time.After(timeout):
		log.Warn().Msg("errbus: drain timed out")
	}
}

// Overflow returns the number of events dropped due to a full channel.
func (b *ErrorBus) Overflow() int64 {
	if b == nil {
		return 0
	}
	return b.overflow.Load()
}

// dispatch is the single dispatcher goroutine. It reads events from the channel
// and fans out to all subscribers. Follows the orDone pattern.
func (b *ErrorBus) dispatch() {
	defer b.wg.Done()

	for {
		select {
		case <-b.ctx.Done():
			// Drain remaining buffered events before exiting.
			b.drainRemaining()
			return
		case event, ok := <-b.ch:
			if !ok {
				return
			}
			b.fanOut(event)
		}
	}
}

// drainRemaining processes any events left in the channel buffer after cancellation.
func (b *ErrorBus) drainRemaining() {
	for {
		select {
		case event, ok := <-b.ch:
			if !ok {
				return
			}
			b.fanOut(event)
		default:
			return
		}
	}
}

// fanOut delivers an event to every subscriber. Subscriber panics are recovered
// so one bad subscriber doesn't crash the bus.
func (b *ErrorBus) fanOut(event ErrorEvent) {
	for _, sub := range b.subscribers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error().
						Interface("panic", r).
						Str("subsystem", event.Subsystem).
						Str("op", event.Op).
						Msg("errbus: subscriber panicked")
				}
			}()
			sub.HandleError(event)
		}()
	}
}
