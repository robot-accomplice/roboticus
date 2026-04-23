// Pipeline-level semantic cache stage.
//
// Provides CheckCache and StoreInCache as explicit pipeline stages that wrap
// the LLM service's cache with pipeline-level quality guards. This prevents
// stale, low-value, or parroting cached responses from bypassing the guard chain.
//
// Ported from Rust: crates/roboticus-pipeline/src/core/cache.rs

package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// CacheHit represents a cached response that passed pipeline quality checks.
type CacheHit struct {
	Content     string
	Model       string
	Fingerprint string
}

// CheckCache looks up a cached response for the given content.
// Unlike the LLM-level cache, this applies pipeline-level quality guards:
//   - Rejects cache hits shorter than 20 chars (low-value)
//   - Rejects cache hits that parrot the user input (>60% overlap)
//   - Rejects cache hits that are pure acknowledgements
//
// Returns nil if no valid cache hit is found.
func (p *Pipeline) CheckCache(ctx context.Context, content string) *CacheHit {
	return p.CheckCacheForSession(ctx, nil, Config{}, content)
}

// CheckCacheForSession looks up a cached response using the full request-shaping
// scaffold that is available on the live pipeline path.
func (p *Pipeline) CheckCacheForSession(ctx context.Context, sess *Session, cfg Config, content string) *CacheHit {
	if p.store == nil {
		return nil
	}

	fp := cacheFingerprint(sess, cfg, content)
	return p.checkCacheByFingerprint(ctx, content, fp)
}

func (p *Pipeline) checkCacheByFingerprint(ctx context.Context, content, fp string) *CacheHit {
	if p.store == nil {
		return nil
	}

	var cached, model string
	row := p.store.QueryRowContext(ctx,
		`SELECT response, model FROM semantic_cache
		 WHERE prompt_hash = ?
		   AND (expires_at IS NULL OR expires_at > datetime('now'))
		 ORDER BY created_at DESC LIMIT 1`,
		fp,
	)
	if err := row.Scan(&cached, &model); err != nil {
		return nil // No cache hit.
	}

	// Low-value guard: reject very short cached responses.
	if len(strings.TrimSpace(cached)) < 20 {
		log.Debug().Str("prompt_hash", fp).Msg("cache hit rejected: too short")
		return nil
	}

	// Incomplete tool call guard: reject if cached response contains unparsed
	// tool call JSON. This means the model tried to call a tool but the response
	// was cached before execution — returning it would show raw JSON to the user.
	if strings.Contains(cached, `"tool_call"`) || strings.Contains(cached, `"function_call"`) {
		log.Debug().Str("prompt_hash", fp).Msg("cache hit rejected: contains unparsed tool call")
		return nil
	}

	// Parroting guard: reject if cached response overlaps heavily with input.
	overlap := textOverlapScore(cached, content)
	if overlap > 0.6 {
		log.Debug().Str("prompt_hash", fp).Float64("overlap", overlap).Msg("cache hit rejected: parroting user input")
		return nil
	}

	// Acknowledgement guard: reject if cached response is just an acknowledgement.
	ackCtx := &ShortcutContext{}
	ackHandler := &AcknowledgementShortcut{}
	if ackHandler.TryMatch(cached, ackCtx) != nil {
		log.Debug().Str("prompt_hash", fp).Msg("cache hit rejected: acknowledgement response")
		return nil
	}

	// Increment hit count.
	_, _ = p.store.ExecContext(ctx,
		`UPDATE semantic_cache SET hit_count = hit_count + 1 WHERE prompt_hash = ?`, fp)

	log.Debug().Str("prompt_hash", fp).Str("model", model).Msg("cache hit accepted")
	return &CacheHit{
		Content:     cached,
		Model:       model,
		Fingerprint: fp,
	}
}

// StoreInCache persists a response in the pipeline semantic cache.
// Only stores responses that pass the same quality guards used by CheckCache.
func (p *Pipeline) StoreInCache(ctx context.Context, content, response, model string) {
	p.StoreInCacheForSession(ctx, nil, Config{}, content, response, model)
}

// StoreInCacheForSession persists a response using the same request-context
// fingerprint that the live pipeline uses for cache replay.
func (p *Pipeline) StoreInCacheForSession(ctx context.Context, sess *Session, cfg Config, content, response, model string) {
	if p.store == nil {
		return
	}

	// Don't cache very short responses.
	if len(strings.TrimSpace(response)) < 20 {
		return
	}

	// Don't cache responses with unparsed tool calls — these are incomplete
	// (the tool was never executed, so the response is just raw JSON).
	if strings.Contains(response, `"tool_call"`) || strings.Contains(response, `"function_call"`) {
		return
	}

	// Don't cache acknowledgement-like responses.
	ackCtx := &ShortcutContext{}
	ackHandler := &AcknowledgementShortcut{}
	if ackHandler.TryMatch(response, ackCtx) != nil {
		return
	}

	// Don't cache parroting responses.
	if textOverlapScore(response, content) > 0.6 {
		return
	}

	fp := cacheFingerprint(sess, cfg, content)
	p.storeInCacheByFingerprint(ctx, response, model, fp)
}

func (p *Pipeline) storeInCacheByFingerprint(ctx context.Context, response, model, fp string) {
	if p.store == nil {
		return
	}

	now := time.Now()
	expiresAt := db.FormatTime(now.Add(p.cacheTTL))
	createdAt := db.FormatTime(now)
	_, err := p.store.ExecContext(ctx,
		`INSERT OR REPLACE INTO semantic_cache
		 (id, prompt_hash, response, model, tokens_saved, hit_count, created_at, expires_at)
		 VALUES (hex(randomblob(16)), ?, ?, ?, 0, 0, ?, ?)`,
		fp, response, model, createdAt, expiresAt,
	)
	if err != nil {
		log.Warn().Err(err).Str("prompt_hash", fp).Msg("cache store failed")
	}
}

// cacheFingerprint generates a deterministic fingerprint for pipeline cache
// lookup. Unlike the lower LLM cache, the pipeline cache now keys on the
// shaped pre-inference scaffold available on the live path so it does not
// replay across materially different conversation/memory/tool contexts.
func cacheFingerprint(sess *Session, cfg Config, content string) string {
	var b strings.Builder
	writeNormalizedCachePart(&b, "content", content)
	writeNormalizedCachePart(&b, "channel", cfg.ChannelLabel)
	writeNormalizedCachePart(&b, "model_override", cfg.ModelOverride)
	fmt.Fprintf(&b, "inference_mode=%d\n", cfg.InferenceMode)
	fmt.Fprintf(&b, "inject_diagnostics=%t\n", cfg.InjectDiagnostics)
	if sess != nil {
		writeNormalizedCachePart(&b, "agent_id", sess.AgentID)
		writeNormalizedCachePart(&b, "agent_name", sess.AgentName)
		writeNormalizedCachePart(&b, "scope_key", sess.ScopeKey)
		writeNormalizedCachePart(&b, "workspace", sess.Workspace)
		fmt.Fprintf(&b, "authority=%d\n", sess.Authority)
		for _, msg := range sess.Messages() {
			writeNormalizedCachePart(&b, "msg_role", msg.Role)
			writeNormalizedCachePart(&b, "msg_content", msg.Content)
		}
		writeNormalizedCachePart(&b, "memory_context", sess.MemoryContext())
		writeNormalizedCachePart(&b, "memory_index", sess.MemoryIndex())
		writeNormalizedCachePart(&b, "hippocampus_summary", sess.HippocampusSummary())
		for _, td := range sess.SelectedToolDefs() {
			writeNormalizedCachePart(&b, "tool_name", td.Function.Name)
			writeNormalizedCachePart(&b, "tool_desc", td.Function.Description)
		}
	}
	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:16])
}

func writeNormalizedCachePart(b *strings.Builder, key, value string) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.Join(strings.Fields(normalized), " ")
	b.WriteString(key)
	b.WriteByte('=')
	b.WriteString(normalized)
	b.WriteByte('\n')
}
