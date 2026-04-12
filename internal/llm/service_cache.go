package llm

import (
	"context"
	"strings"

	"roboticus/internal/core"
)

// wrapStreamCache accumulates streamed chunks and caches the full response.
func (s *Service) wrapStreamCache(ctx context.Context, in <-chan StreamChunk, req *Request) <-chan StreamChunk {
	out := make(chan StreamChunk, 32)
	go func() {
		defer close(out)
		var full strings.Builder
		for chunk := range core.OrDone(ctx.Done(), in) {
			full.WriteString(chunk.Delta)
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
		if content := full.String(); content != "" {
			s.cache.Put(ctx, req, &Response{Content: content})
		}
	}()
	return out
}
