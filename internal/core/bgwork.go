package core

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// BackgroundWorker provides a bounded, context-aware pool for fire-and-forget work.
//
// Follows the orDone pattern from "Concurrency in Go": every goroutine's lifetime
// is tied to a context. When the context is cancelled, new submissions are rejected
// and running tasks receive the cancelled context — they should check ctx.Err()
// and exit promptly.
//
// All background goroutines should be submitted here instead of bare `go func()`.
// This ensures:
//   - Bounded concurrency (semaphore)
//   - Panic recovery (defer/recover)
//   - Clean shutdown (Drain waits or context cancellation drops)
//   - No goroutine leaks (every goroutine has a termination path)
type BackgroundWorker struct {
	wg     sync.WaitGroup
	sem    chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
}

// NewBackgroundWorker creates a worker pool with the given concurrency limit.
// Uses an internal context — call Drain() to stop accepting work and wait.
func NewBackgroundWorker(maxConcurrency int) *BackgroundWorker {
	return NewBackgroundWorkerWithContext(context.Background(), maxConcurrency)
}

// NewBackgroundWorkerWithContext creates a worker pool tied to the given context.
//
// This is the preferred constructor for tests: pass a test-scoped context so that
// worker goroutines are cancelled when the test ends. This prevents the TempDir
// cleanup race where background goroutines write to a deleted SQLite file.
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//	bgw := core.NewBackgroundWorkerWithContext(ctx, 4)
//
// Or in Go 1.24+ tests:
//
//	bgw := core.NewBackgroundWorkerWithContext(t.Context(), 4)
func NewBackgroundWorkerWithContext(parent context.Context, maxConcurrency int) *BackgroundWorker {
	if maxConcurrency < 1 {
		maxConcurrency = 16
	}
	ctx, cancel := context.WithCancel(parent)
	return &BackgroundWorker{
		sem:    make(chan struct{}, maxConcurrency),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Submit enqueues a function for background execution.
//
// orDone pattern: if the worker's context is cancelled at any point (before
// submission, while waiting for a semaphore slot, or after acquiring one),
// the task is dropped. Running tasks receive the cancelled context and should
// check ctx.Err() to exit early.
//
// Backpressure: if all semaphore slots are occupied AND the context is live,
// the function runs synchronously in the caller's goroutine.
func (w *BackgroundWorker) Submit(name string, fn func(ctx context.Context)) {
	// orDone: reject immediately if context is already cancelled.
	select {
	case <-w.ctx.Done():
		log.Debug().Str("task", name).Msg("background worker cancelled, dropping task")
		return
	default:
	}

	w.wg.Add(1)

	// Three-way select: acquire slot, detect cancellation, or run synchronously.
	select {
	case w.sem <- struct{}{}:
		// Got a slot — run in a new goroutine.
		go func() {
			defer w.wg.Done()
			defer func() { <-w.sem }()
			defer func() {
				if r := recover(); r != nil {
					log.Error().Str("task", name).Interface("panic", r).Msg("background task panicked")
				}
			}()
			// orDone: check again before executing — context may have been
			// cancelled between acquiring the slot and starting the work.
			select {
			case <-w.ctx.Done():
				log.Debug().Str("task", name).Msg("background task cancelled before execution")
				return
			default:
			}
			fn(w.ctx)
		}()

	case <-w.ctx.Done():
		// orDone: cancelled while waiting for a slot.
		w.wg.Done()
		log.Debug().Str("task", name).Msg("background task cancelled waiting for slot")

	default:
		// Backpressure: pool full, run synchronously.
		defer w.wg.Done()
		fn(w.ctx)
	}
}

// Drain waits for all in-flight work to complete (up to timeout), THEN cancels
// the context to reject future submissions. This ordering matters:
//
//   - Wait first: in-flight tasks run to completion with a live context
//   - Cancel second: no new tasks accepted after drain returns
//
// The reverse (cancel-then-wait) would abort in-flight tasks that haven't
// started executing yet — the orDone check in the goroutine would see the
// cancelled context and skip the work.
func (w *BackgroundWorker) Drain(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		log.Info().Msg("background worker drained cleanly")
	case <-time.After(timeout):
		log.Warn().Msg("background worker drain timed out")
	}
	// Cancel AFTER drain — prevents new submissions but doesn't abort in-flight.
	w.cancel()
}

// Cancel immediately cancels the worker context, causing in-flight tasks to
// receive a cancelled context and new submissions to be dropped. Use this for
// hard shutdown where you don't want to wait for completion.
func (w *BackgroundWorker) Cancel() {
	w.cancel()
}
