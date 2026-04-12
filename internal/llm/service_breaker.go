package llm

import (
	"context"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
)

// wrapStreamBreaker wraps a stream to record circuit breaker state.
// It owns the original errs channel and returns a new one to prevent data races.
func (s *Service) wrapStreamBreaker(ctx context.Context, in <-chan StreamChunk, errs <-chan error, cb *CircuitBreaker, provider string) (<-chan StreamChunk, <-chan error) {
	out := make(chan StreamChunk, 32)
	outErrs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(outErrs)
		gotChunk := false

		for chunk := range core.OrDone(ctx.Done(), in) {
			if !gotChunk {
				cb.RecordSuccess()
				gotChunk = true
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}

		// Drain the original error channel (single reader).
		select {
		case err := <-errs:
			if err != nil {
				if !gotChunk {
					cb.RecordFailure()
				}
				log.Warn().Err(err).Str("provider", provider).Msg("stream failed")
				outErrs <- err
			}
		default:
		}
	}()
	return out, outErrs
}
