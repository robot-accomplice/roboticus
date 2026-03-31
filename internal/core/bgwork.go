package core

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// BackgroundWorker provides a bounded, trackable pool for fire-and-forget work.
// All background goroutines should be submitted here instead of bare `go func()`.
// The Drain method blocks until all submitted work completes or the timeout expires,
// ensuring clean shutdown.
type BackgroundWorker struct {
	wg     sync.WaitGroup
	sem    chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
}

// NewBackgroundWorker creates a worker pool with the given concurrency limit.
func NewBackgroundWorker(maxConcurrency int) *BackgroundWorker {
	if maxConcurrency < 1 {
		maxConcurrency = 16
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &BackgroundWorker{
		sem:    make(chan struct{}, maxConcurrency),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Submit enqueues a function for background execution. If the pool is at capacity,
// the function runs synchronously to apply backpressure. The context passed to fn
// is cancelled when Drain is called.
func (w *BackgroundWorker) Submit(name string, fn func(ctx context.Context)) {
	select {
	case <-w.ctx.Done():
		log.Debug().Str("task", name).Msg("background worker shutting down, skipping task")
		return
	default:
	}

	w.wg.Add(1)
	select {
	case w.sem <- struct{}{}:
		go func() {
			defer w.wg.Done()
			defer func() { <-w.sem }()
			defer func() {
				if r := recover(); r != nil {
					log.Error().Str("task", name).Interface("panic", r).Msg("background task panicked")
				}
			}()
			fn(w.ctx)
		}()
	default:
		// Pool full — run synchronously to apply backpressure.
		defer w.wg.Done()
		fn(w.ctx)
	}
}

// Drain cancels the worker context and waits for all submitted work to complete,
// up to the given timeout.
func (w *BackgroundWorker) Drain(timeout time.Duration) {
	w.cancel()
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
}
