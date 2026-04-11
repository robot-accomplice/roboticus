// Tool embedding cache — stores pre-computed tool description embeddings
// keyed by (tool_name, description_hash) so re-embedding only happens on change.
//
// Ported from Rust: crates/roboticus-db/src/tool_embeddings.rs

package db

import (
	"context"
	"database/sql"
	"encoding/binary"
	"math"
)

// ToolEmbeddingsRepository manages tool description embedding cache.
type ToolEmbeddingsRepository struct {
	q Querier
}

// NewToolEmbeddingsRepository creates a tool embeddings repository.
func NewToolEmbeddingsRepository(q Querier) *ToolEmbeddingsRepository {
	return &ToolEmbeddingsRepository{q: q}
}

// SaveToolEmbedding stores a tool's embedding vector in the cache.
func (r *ToolEmbeddingsRepository) SaveToolEmbedding(
	ctx context.Context,
	toolName, descHash string,
	embedding []float32,
) error {
	blob := EmbeddingToBlob(embedding)
	_, err := r.q.ExecContext(ctx,
		`INSERT OR REPLACE INTO tool_embeddings (tool_name, description_hash, embedding, dimensions)
		 VALUES (?, ?, ?, ?)`,
		toolName, descHash, blob, len(embedding))
	return err
}

// GetToolEmbedding loads a tool's embedding from the cache.
// Returns nil if no entry exists for this (toolName, descHash) pair.
func (r *ToolEmbeddingsRepository) GetToolEmbedding(
	ctx context.Context,
	toolName, descHash string,
) ([]float32, error) {
	var blob []byte
	err := r.q.QueryRowContext(ctx,
		`SELECT embedding FROM tool_embeddings
		 WHERE tool_name = ? AND description_hash = ?`,
		toolName, descHash).Scan(&blob)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return BlobToEmbedding(blob), nil
}

// EmbeddingToBlob serializes []float32 to compact little-endian bytes.
// Rust parity: embeddings.rs embedding_to_blob() — 4-byte LE IEEE 754 per f32.
func EmbeddingToBlob(embedding []float32) []byte {
	buf := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// BlobToEmbedding deserializes a BLOB back to []float32.
// Rust parity: embeddings.rs blob_to_embedding().
func BlobToEmbedding(blob []byte) []float32 {
	n := len(blob) / 4
	result := make([]float32, n)
	for i := 0; i < n; i++ {
		bits := binary.LittleEndian.Uint32(blob[i*4:])
		result[i] = math.Float32frombits(bits)
	}
	return result
}
