// Pipeline-level semantic cache stage.
//
// Provides CheckCache and StoreInCache as explicit pipeline stages that wrap
// the LLM service's cache with pipeline-level quality guards. This prevents
// stale, low-value, or parroting cached responses from bypassing the guard chain.
//
// Ported from Rust: crates/roboticus-pipeline/src/core/cache.rs

package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/rs/zerolog/log"
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
func (p *Pipeline) CheckCache(content string) *CacheHit {
	if p.store == nil {
		return nil
	}

	fp := cacheFingerprint(content)

	var cached, model string
	row := p.store.DB().QueryRow(
		`SELECT response, model FROM semantic_cache
		 WHERE prompt_hash = ?
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
	_, _ = p.store.DB().Exec(
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
func (p *Pipeline) StoreInCache(content, response, model string) {
	if p.store == nil {
		return
	}

	// Don't cache very short responses.
	if len(strings.TrimSpace(response)) < 20 {
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

	fp := cacheFingerprint(content)
	_, err := p.store.DB().Exec(
		`INSERT OR REPLACE INTO semantic_cache (id, prompt_hash, response, model)
		 VALUES (hex(randomblob(16)), ?, ?, ?)`,
		fp, response, model,
	)
	if err != nil {
		log.Warn().Err(err).Str("prompt_hash", fp).Msg("cache store failed")
	}
}

// cacheFingerprint generates a deterministic fingerprint for cache lookup.
// Uses SHA-256 of the normalized content (lowercase, trimmed, collapsed whitespace).
func cacheFingerprint(content string) string {
	normalized := strings.ToLower(strings.TrimSpace(content))
	normalized = strings.Join(strings.Fields(normalized), " ")
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:16])
}
