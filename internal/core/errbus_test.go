package core

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// collectSubscriber records all events it receives.
type collectSubscriber struct {
	events []ErrorEvent
}

func (c *collectSubscriber) HandleError(event ErrorEvent) {
	c.events = append(c.events, event)
}

func TestErrorBus_DispatchesToSubscribers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := &collectSubscriber{}
	bus := NewErrorBus(ctx, 16, sub)

	bus.Report(ErrorEvent{
		Subsystem: "llm",
		Op:        "record_cost",
		Err:       errors.New("insert failed"),
		Severity:  SevWarning,
	})

	bus.Drain(2 * time.Second)

	if len(sub.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sub.events))
	}
	ev := sub.events[0]
	if ev.Subsystem != "llm" {
		t.Errorf("subsystem = %q", ev.Subsystem)
	}
	if ev.Op != "record_cost" {
		t.Errorf("op = %q", ev.Op)
	}
	if ev.Time.IsZero() {
		t.Error("Time should be auto-set")
	}
}

func TestErrorBus_MultipleSubscribers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub1 := &collectSubscriber{}
	sub2 := &collectSubscriber{}
	bus := NewErrorBus(ctx, 16, sub1, sub2)

	bus.Report(ErrorEvent{
		Subsystem: "db",
		Op:        "write",
		Err:       errors.New("disk full"),
		Severity:  SevError,
	})

	bus.Drain(2 * time.Second)

	if len(sub1.events) != 1 || len(sub2.events) != 1 {
		t.Errorf("both subscribers should receive the event: sub1=%d sub2=%d",
			len(sub1.events), len(sub2.events))
	}
}

func TestErrorBus_ReportIfErr_NilErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := &collectSubscriber{}
	bus := NewErrorBus(ctx, 16, sub)

	// nil error should not produce an event.
	bus.ReportIfErr(nil, "llm", "op", SevWarning)

	bus.Drain(2 * time.Second)

	if len(sub.events) != 0 {
		t.Errorf("nil error should not dispatch, got %d events", len(sub.events))
	}
}

func TestErrorBus_NilBusIsNoOp(t *testing.T) {
	var bus *ErrorBus

	// None of these should panic.
	bus.Report(ErrorEvent{Err: errors.New("test")})
	bus.ReportIfErr(errors.New("test"), "x", "y", SevError)
	bus.ReportEvent(ErrorEvent{Err: errors.New("test")})
	bus.Drain(time.Second)

	if bus.Overflow() != 0 {
		t.Error("nil bus Overflow should return 0")
	}
}

func TestErrorBus_SubscriberPanicRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	panicker := &panicSubscriber{}
	collector := &collectSubscriber{}
	// Panicker first, collector second — collector should still receive the event.
	bus := NewErrorBus(ctx, 16, panicker, collector)

	bus.Report(ErrorEvent{
		Subsystem: "test",
		Op:        "panic",
		Err:       errors.New("trigger panic"),
		Severity:  SevError,
	})

	bus.Drain(2 * time.Second)

	if len(collector.events) != 1 {
		t.Error("collector should receive event even when earlier subscriber panics")
	}
}

func TestErrorBus_OverflowCounting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Buffer size 1 + block the dispatcher so the channel fills up.
	blocker := &blockSubscriber{block: make(chan struct{})}
	bus := NewErrorBus(ctx, 1, blocker)

	// First event enters the channel buffer.
	bus.Report(ErrorEvent{Subsystem: "a", Op: "1", Err: errors.New("1"), Severity: SevInfo})
	// Give dispatcher a moment to pick up the first event and block.
	time.Sleep(50 * time.Millisecond)

	// Second event goes into the now-empty buffer slot.
	bus.Report(ErrorEvent{Subsystem: "a", Op: "2", Err: errors.New("2"), Severity: SevInfo})
	// Third event should overflow because buffer is full and dispatcher is blocked.
	bus.Report(ErrorEvent{Subsystem: "a", Op: "3", Err: errors.New("3"), Severity: SevInfo})

	if bus.Overflow() < 1 {
		t.Error("expected at least 1 overflow event")
	}

	close(blocker.block)
	cancel()
	bus.Drain(2 * time.Second)
}

func TestMetricSubscriber_Snapshot(t *testing.T) {
	m := NewMetricSubscriber()

	m.HandleError(ErrorEvent{Subsystem: "llm", Severity: SevWarning})
	m.HandleError(ErrorEvent{Subsystem: "llm", Severity: SevWarning})
	m.HandleError(ErrorEvent{Subsystem: "db", Severity: SevError})

	snap := m.Snapshot()
	if snap["llm:warning"] != 2 {
		t.Errorf("llm:warning = %d, want 2", snap["llm:warning"])
	}
	if snap["db:error"] != 1 {
		t.Errorf("db:error = %d, want 1", snap["db:error"])
	}
}

func TestRingBufferSubscriber_Recent(t *testing.T) {
	r := NewRingBufferSubscriber(3)

	for i := range 5 {
		r.HandleError(ErrorEvent{
			Subsystem: "test",
			Op:        string(rune('a' + i)),
			Err:       errors.New("err"),
		})
	}

	recent := r.Recent(10) // Ask for more than capacity.
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent events, got %d", len(recent))
	}
	// Newest first: e, d, c (indices 4, 3, 2).
	if recent[0].Op != "e" {
		t.Errorf("newest event op = %q, want 'e'", recent[0].Op)
	}
	if recent[2].Op != "c" {
		t.Errorf("oldest event op = %q, want 'c'", recent[2].Op)
	}
}

func TestRingBufferSubscriber_Empty(t *testing.T) {
	r := NewRingBufferSubscriber(10)
	recent := r.Recent(5)
	if recent != nil {
		t.Errorf("empty buffer should return nil, got %v", recent)
	}
}

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		sev  Severity
		want string
	}{
		{SevDebug, "debug"},
		{SevInfo, "info"},
		{SevWarning, "warning"},
		{SevError, "error"},
		{SevCritical, "critical"},
		{Severity(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.sev.String(); got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tt.sev, got, tt.want)
		}
	}
}

func TestErrorBus_ConcurrentReporting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var count atomic.Int64
	counter := &countSubscriber{count: &count}
	bus := NewErrorBus(ctx, 256, counter)

	const goroutines = 50
	const eventsPerGoroutine = 20

	var wg atomic.Int64
	wg.Store(goroutines)

	for range goroutines {
		go func() {
			for range eventsPerGoroutine {
				bus.Report(ErrorEvent{
					Subsystem: "test",
					Op:        "concurrent",
					Err:       errors.New("test"),
					Severity:  SevInfo,
				})
			}
			wg.Add(-1)
		}()
	}

	// Wait for all senders.
	for wg.Load() > 0 {
		time.Sleep(time.Millisecond)
	}

	bus.Drain(5 * time.Second)

	total := count.Load() + bus.Overflow()
	if total != goroutines*eventsPerGoroutine {
		t.Errorf("total events = %d (received %d + overflow %d), want %d",
			total, count.Load(), bus.Overflow(), goroutines*eventsPerGoroutine)
	}
}

// --- test helpers ---

type panicSubscriber struct{}

func (p *panicSubscriber) HandleError(_ ErrorEvent) {
	panic("intentional test panic")
}

type blockSubscriber struct {
	block chan struct{}
}

func (b *blockSubscriber) HandleError(_ ErrorEvent) {
	<-b.block
}

type countSubscriber struct {
	count *atomic.Int64
}

func (c *countSubscriber) HandleError(_ ErrorEvent) {
	c.count.Add(1)
}
