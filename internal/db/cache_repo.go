package db

import (
	"context"
	"database/sql"
)

// CacheRow represents a row in the semantic_cache table.
type CacheRow struct {
	ID        string
	PromptHash string
	Response  string
	Model     string
	HitCount  int
	CreatedAt string
}

// CacheRepository handles semantic-cache persistence.
type CacheRepository struct {
	q Querier
}

// NewCacheRepository creates a cache repository.
func NewCacheRepository(q Querier) *CacheRepository {
	return &CacheRepository{q: q}
}

// Lookup retrieves a cached response by prompt hash. Returns nil if not found.
func (r *CacheRepository) Lookup(ctx context.Context, promptHash string) (*CacheRow, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, prompt_hash, response, model, hit_count, created_at
		 FROM semantic_cache WHERE prompt_hash = ?`,
		promptHash)
	var c CacheRow
	err := row.Scan(&c.ID, &c.PromptHash, &c.Response, &c.Model, &c.HitCount, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Store inserts or replaces a cached response.
func (r *CacheRepository) Store(ctx context.Context, id, promptHash, response, model string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT OR REPLACE INTO semantic_cache (id, prompt_hash, response, model) VALUES (?, ?, ?, ?)`,
		id, promptHash, response, model)
	return err
}

// IncrementHits bumps the hit_count for a cached entry.
func (r *CacheRepository) IncrementHits(ctx context.Context, promptHash string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE semantic_cache SET hit_count = hit_count + 1 WHERE prompt_hash = ?`, promptHash)
	return err
}

// Evict removes entries older than maxAge (SQLite datetime string).
func (r *CacheRepository) Evict(ctx context.Context, maxAge string) (int64, error) {
	result, err := r.q.ExecContext(ctx,
		`DELETE FROM semantic_cache WHERE created_at < ?`, maxAge)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// Stats returns total entry count and sum of hit_count.
func (r *CacheRepository) Stats(ctx context.Context) (total int, totalHits int, err error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(hit_count), 0) FROM semantic_cache`)
	err = row.Scan(&total, &totalHits)
	return
}
