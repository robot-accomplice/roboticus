package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"goboticus/internal/db"
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
	store   *db.Store // L2: persistent
}

type cacheEntry struct {
	Response  *Response
	CreatedAt time.Time
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
func NewCache(cfg CacheConfig, store *db.Store) *Cache {
	return &Cache{
		mem:     make(map[string]*cacheEntry),
		maxSize: cfg.MaxEntries,
		ttl:     cfg.TTL,
		store:   store,
	}
}

// Get checks L1 then L2 for a cached response. Returns nil on miss.
func (c *Cache) Get(ctx context.Context, req *Request) *Response {
	hash := hashRequest(req)

	// L1: in-memory.
	c.mu.RLock()
	if entry, ok := c.mem[hash]; ok {
		c.mu.RUnlock()
		if time.Since(entry.CreatedAt) < c.ttl {
			log.Debug().Str("hash", hash[:12]).Msg("cache hit (L1)")
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
	c.store.ExecContext(ctx,
		`UPDATE semantic_cache SET hit_count = hit_count + 1 WHERE prompt_hash = ?`, hash)

	log.Debug().Str("hash", hash[:12]).Msg("cache hit (L2)")
	return &resp
}

// Put stores a response in both L1 and L2.
func (c *Cache) Put(ctx context.Context, req *Request, resp *Response) {
	hash := hashRequest(req)
	now := time.Now()
	c.put(hash, resp, now)

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

	c.store.ExecContext(ctx,
		`INSERT OR REPLACE INTO semantic_cache
		 (id, prompt_hash, response, model, tokens_saved, hit_count, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, 0, datetime('now'), ?)`,
		id, hash, string(respJSON), resp.Model, tokensSaved, expires,
	)
}

// put adds an entry to L1 with LRU eviction.
func (c *Cache) put(hash string, resp *Response, created time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.mem[hash] = &cacheEntry{Response: resp, CreatedAt: created}
	c.order = append(c.order, hash)

	// Evict oldest if over capacity.
	for len(c.mem) > c.maxSize && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.mem, oldest)
	}
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
