package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
)

// Cache provides a two-tier semantic cache:
//   - L1: In-memory LRU keyed by prompt hash (fast, volatile)
//   - L2: SQLite-backed persistent cache (survives restarts)
//
// Unlike the Rust version which bolted on persistence later, this is
// persistent from day one.
type Cache struct {
	mu      sync.RWMutex
	mem     map[string]*cacheEntry // L1: prompt_hash → entry
	order   []string               // LRU eviction order
	maxSize int
	ttl     time.Duration
	store  *db.Store // L2: persistent
	errBus *core.ErrorBus
}

type cacheEntry struct {
	Response      *Response
	CreatedAt     time.Time
	InvolvedTools bool // true when the originating request had tools
	Hits          uint64
	Embedding     []float64 // optional embedding for semantic lookup
}

// CacheConfig controls cache behavior.
type CacheConfig struct {
	Enabled    bool
	MaxEntries int
	TTL        time.Duration
}

// DefaultCacheConfig returns sensible defaults.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		Enabled:    true,
		MaxEntries: 1000,
		TTL:        1 * time.Hour,
	}
}

// NewCache creates a two-tier cache.
func NewCache(cfg CacheConfig, store *db.Store, errBus *core.ErrorBus) *Cache {
	return &Cache{
		mem:     make(map[string]*cacheEntry),
		maxSize: cfg.MaxEntries,
		ttl:     cfg.TTL,
		store:   store,
		errBus:  errBus,
	}
}

// Get checks L1 then L2 for a cached response. Returns nil on miss.
func (c *Cache) Get(ctx context.Context, req *Request) *Response {
	hash := hashRequest(req)

	// L1: in-memory.
	c.mu.RLock()
	if entry, ok := c.mem[hash]; ok {
		c.mu.RUnlock()
		// Tool-aware TTL: requests involving tools get TTL/4.
		effectiveTTL := c.ttl
		if entry.InvolvedTools {
			effectiveTTL = c.ttl / 4
		}
		if time.Since(entry.CreatedAt) < effectiveTTL {
			c.mu.Lock()
			entry.Hits++
			c.mu.Unlock()
			log.Trace().Str("hash", hash[:12]).Msg("cache hit (L1)")
			return entry.Response
		}
		// Expired — fall through to L2 and evict from L1.
		c.mu.Lock()
		delete(c.mem, hash)
		c.mu.Unlock()
	} else {
		c.mu.RUnlock()
	}

	// L2: SQLite.
	if c.store == nil {
		return nil
	}
	row := c.store.QueryRowContext(ctx,
		`SELECT response, created_at FROM semantic_cache
		 WHERE prompt_hash = ? AND (expires_at IS NULL OR expires_at > datetime('now'))
		 ORDER BY created_at DESC LIMIT 1`,
		hash,
	)

	var respJSON string
	var createdAt string
	if err := row.Scan(&respJSON, &createdAt); err != nil {
		return nil
	}

	var resp Response
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		return nil
	}

	// Promote to L1.
	created, _ := time.Parse(time.RFC3339, createdAt)
	c.put(hash, &resp, created)

	// Update hit count.
	if _, err := c.store.ExecContext(ctx,
		`UPDATE semantic_cache SET hit_count = hit_count + 1 WHERE prompt_hash = ?`, hash); err != nil {
		c.errBus.ReportIfErr(err, "llm", "cache_update_hit_count", core.SevDebug)
	}

	log.Trace().Str("hash", hash[:12]).Msg("cache hit (L2)")
	return &resp
}

// Put stores a response in both L1 and L2.
func (c *Cache) Put(ctx context.Context, req *Request, resp *Response) {
	hash := hashRequest(req)
	now := time.Now()
	involvedTools := len(req.Tools) > 0
	c.putWithTools(hash, resp, now, involvedTools)

	// Persist to L2.
	if c.store == nil {
		return
	}

	respJSON, err := json.Marshal(resp)
	if err != nil {
		return
	}

	id := hash[:32]
	expires := now.Add(c.ttl).Format(time.RFC3339)
	tokensSaved := resp.Usage.InputTokens + resp.Usage.OutputTokens

	if _, err := c.store.ExecContext(ctx,
		`INSERT OR REPLACE INTO semantic_cache
		 (id, prompt_hash, response, model, tokens_saved, hit_count, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, 0, datetime('now'), ?)`,
		id, hash, string(respJSON), resp.Model, tokensSaved, expires,
	); err != nil {
		c.errBus.ReportIfErr(err, "llm", "cache_persist", core.SevWarning)
	}
}

// put adds an entry to L1 with LFU eviction (no tools flag).
func (c *Cache) put(hash string, resp *Response, created time.Time) {
	c.putWithTools(hash, resp, created, false)
}

// putWithTools adds an entry to L1 with LFU eviction.
func (c *Cache) putWithTools(hash string, resp *Response, created time.Time, involvedTools bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.mem[hash] = &cacheEntry{
		Response:      resp,
		CreatedAt:     created,
		InvolvedTools: involvedTools,
		Hits:          0,
	}
	c.order = append(c.order, hash)

	// Evict entry with lowest hits (LFU) if over capacity.
	for len(c.mem) > c.maxSize && len(c.order) > 0 {
		lfuIdx := 0
		lfuHits := ^uint64(0) // max uint64
		for i, h := range c.order {
			if e, ok := c.mem[h]; ok && e.Hits < lfuHits {
				lfuHits = e.Hits
				lfuIdx = i
			}
		}
		evictHash := c.order[lfuIdx]
		c.order = append(c.order[:lfuIdx], c.order[lfuIdx+1:]...)
		delete(c.mem, evictHash)
	}
}

// PutWithEmbedding stores a response with an associated embedding for semantic lookup.
func (c *Cache) PutWithEmbedding(ctx context.Context, req *Request, resp *Response, embedding []float64) {
	hash := hashRequest(req)
	now := time.Now()
	involvedTools := len(req.Tools) > 0

	c.mu.Lock()
	c.mem[hash] = &cacheEntry{
		Response:      resp,
		CreatedAt:     now,
		InvolvedTools: involvedTools,
		Hits:          0,
		Embedding:     embedding,
	}
	c.order = append(c.order, hash)

	// Evict if over capacity.
	for len(c.mem) > c.maxSize && len(c.order) > 0 {
		lfuIdx := 0
		lfuHits := ^uint64(0)
		for i, h := range c.order {
			if e, ok := c.mem[h]; ok && e.Hits < lfuHits {
				lfuHits = e.Hits
				lfuIdx = i
			}
		}
		evictHash := c.order[lfuIdx]
		c.order = append(c.order[:lfuIdx], c.order[lfuIdx+1:]...)
		delete(c.mem, evictHash)
	}
	c.mu.Unlock()

	// Persist to L2 (same as Put, embedding is L1-only for now).
	if c.store == nil {
		return
	}
	respJSON, err := json.Marshal(resp)
	if err != nil {
		return
	}
	id := hash[:32]
	expires := now.Add(c.ttl).Format(time.RFC3339)
	tokensSaved := resp.Usage.InputTokens + resp.Usage.OutputTokens
	if _, err := c.store.ExecContext(ctx,
		`INSERT OR REPLACE INTO semantic_cache
		 (id, prompt_hash, response, model, tokens_saved, hit_count, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, 0, datetime('now'), ?)`,
		id, hash, string(respJSON), resp.Model, tokensSaved, expires,
	); err != nil {
		c.errBus.ReportIfErr(err, "llm", "cache_persist", core.SevWarning)
	}
}

// GetSemantic searches L1 cache entries by cosine similarity against the given
// embedding. Returns the best-matching cached response if similarity exceeds
// the threshold. This is the three-tier cache's semantic lookup layer.
func (c *Cache) GetSemantic(_ context.Context, embedding []float64, threshold float64) (*Response, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var bestEntry *cacheEntry
	var bestSim float64

	for _, entry := range c.mem {
		if len(entry.Embedding) == 0 {
			continue
		}
		if time.Since(entry.CreatedAt) >= c.ttl {
			continue
		}
		sim := cosineSimilarity(embedding, entry.Embedding)
		if sim > bestSim {
			bestSim = sim
			bestEntry = entry
		}
	}

	if bestEntry == nil || bestSim < threshold {
		return nil, false
	}

	log.Trace().Float64("similarity", bestSim).Msg("cache hit (semantic)")
	return bestEntry.Response, true
}

// cosineSimilarity computes the cosine similarity between two vectors.
// Returns 0 if either vector is zero-length or they have different dimensions.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (sqrt(normA) * sqrt(normB))
}

// sqrt is a simple Newton's method square root to avoid importing math.
func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 20; i++ {
		z = (z + x/z) / 2
	}
	return z
}

// hashRequest produces a deterministic hash of the request for cache keying.
func hashRequest(req *Request) string {
	h := sha256.New()
	h.Write([]byte(req.Model))
	for _, m := range req.Messages {
		h.Write([]byte(m.Role))
		h.Write([]byte(m.Content))
	}
	// Include tool definitions in hash so different tool sets don't collide.
	if len(req.Tools) > 0 {
		toolBytes, _ := json.Marshal(req.Tools)
		h.Write(toolBytes)
	}
	return hex.EncodeToString(h.Sum(nil))
}
