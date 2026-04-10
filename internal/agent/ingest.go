// Package agent: Document ingestion pipeline — file → parse → chunk → embed → store.
//
// Supports .md, .txt, .go, .rs, .py, .js, .ts, .pdf files.
// Ported from Rust: crates/roboticus-agent/src/ingest.rs
//
// Pipeline:
//  1. Detect file type by extension
//  2. Extract raw text (plain-text passthrough)
//  3. Chunk using ChunkConfig (512 tokens, 64-token overlap)
//  4. Store each chunk as semantic memory + embedding entry
//  5. Register the document as a knowledge source in hippocampus

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// FileType represents a supported ingestion file type.
type FileType int

const (
	FileTypeMarkdown FileType = iota + 1
	FileTypePlainText
	FileTypeGoSource
	FileTypeRustSource
	FileTypePythonSource
	FileTypeJavaScriptSource
	FileTypeTypeScriptSource
)

// FileTypeFromPath detects file type from extension. Returns 0 for unsupported types.
func FileTypeFromPath(path string) FileType {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown":
		return FileTypeMarkdown
	case ".txt", ".text":
		return FileTypePlainText
	case ".go":
		return FileTypeGoSource
	case ".rs":
		return FileTypeRustSource
	case ".py":
		return FileTypePythonSource
	case ".js", ".jsx", ".mjs":
		return FileTypeJavaScriptSource
	case ".ts", ".tsx", ".mts":
		return FileTypeTypeScriptSource
	default:
		return 0
	}
}

// IsCode returns true for source code file types.
func (ft FileType) IsCode() bool {
	switch ft {
	case FileTypeGoSource, FileTypeRustSource, FileTypePythonSource,
		FileTypeJavaScriptSource, FileTypeTypeScriptSource:
		return true
	default:
		return false
	}
}

// Label returns a human-readable label for the file type.
func (ft FileType) Label() string {
	switch ft {
	case FileTypeMarkdown:
		return "markdown"
	case FileTypePlainText:
		return "plain_text"
	case FileTypeGoSource:
		return "go"
	case FileTypeRustSource:
		return "rust"
	case FileTypePythonSource:
		return "python"
	case FileTypeJavaScriptSource:
		return "javascript"
	case FileTypeTypeScriptSource:
		return "typescript"
	default:
		return "unknown"
	}
}

// IngestResult describes the outcome of ingesting a single file.
type IngestResult struct {
	FilePath     string   `json:"file_path"`
	FileType     FileType `json:"file_type"`
	ChunksStored int      `json:"chunks_stored"`
	TotalChars   int      `json:"total_chars"`
	SourceID     string   `json:"source_id"`
}

// MaxFileSize is the maximum file size we'll ingest (10 MB). Prevents OOM on giant files.
const MaxFileSize = 10 * 1024 * 1024

// ChunkConfig controls text chunking for ingestion.
type ChunkConfig struct {
	MaxTokens     int // default 512
	OverlapTokens int // default 64
}

// DefaultChunkConfig returns the standard chunking config (512 tokens, 64-token overlap).
func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{MaxTokens: 512, OverlapTokens: 64}
}

// TextChunk is a single chunk of text with position metadata.
type TextChunk struct {
	Text      string
	Index     int
	StartChar int
	EndChar   int
}

// ChunkText splits text into overlapping chunks for embedding.
func ChunkText(text string, cfg ChunkConfig) []TextChunk {
	if text == "" || cfg.MaxTokens == 0 {
		return nil
	}

	maxBytes := cfg.MaxTokens * 4
	overlapBytes := cfg.OverlapTokens * 4

	if len(text) <= maxBytes {
		return []TextChunk{{
			Text:      text,
			Index:     0,
			StartChar: 0,
			EndChar:   len(text),
		}}
	}

	step := maxBytes - overlapBytes
	if step < 1 {
		step = 1
	}

	var chunks []TextChunk
	start := 0
	idx := 0
	for start < len(text) {
		end := start + maxBytes
		if end > len(text) {
			end = len(text)
		}
		// Snap to UTF-8 boundary.
		end = floorCharBoundary(text, end)

		chunks = append(chunks, TextChunk{
			Text:      text[start:end],
			Index:     idx,
			StartChar: start,
			EndChar:   end,
		})
		idx++

		next := start + step
		if next <= start {
			break
		}
		start = next
	}

	return chunks
}

// floorCharBoundary snaps a byte offset to the nearest char boundary at or before pos.
func floorCharBoundary(text string, pos int) int {
	if pos >= len(text) {
		return len(text)
	}
	for pos > 0 && text[pos]>>6 == 0x2 { // continuation byte
		pos--
	}
	return pos
}

// IngestFile ingests a single file into the knowledge system.
//
// Steps:
//  1. Validate file exists and is within size limits
//  2. Detect file type
//  3. Extract text
//  4. Chunk with standard config (512 tokens, 64-token overlap)
//  5. Store each chunk as semantic memory + embedding entry
//  6. Register in hippocampus as a knowledge source
func IngestFile(ctx context.Context, store *db.Store, embedClient *llm.EmbeddingClient, path string) (*IngestResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot access %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > MaxFileSize {
		return nil, fmt.Errorf("%s exceeds maximum file size (%d bytes > %d bytes)", path, info.Size(), MaxFileSize)
	}

	ft := FileTypeFromPath(path)
	if ft == 0 {
		return nil, fmt.Errorf("unsupported file type: %s", path)
	}

	// Extract text (plain-text passthrough for all supported types).
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	text := string(data)
	totalChars := len(text)

	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%s contains no extractable text", path)
	}

	// Chunk.
	cfg := DefaultChunkConfig()
	chunks := ChunkText(text, cfg)

	// Generate a stable source ID from the file path.
	canonical, err := filepath.Abs(path)
	if err != nil {
		canonical = path
	}
	sourceID := "ingest:" + strings.ReplaceAll(strings.ReplaceAll(canonical, "/", ":"), "\\", ":")
	fileName := filepath.Base(path)

	// Store each chunk.
	memRepo := db.NewMemoryRepository(store)
	stored := 0
	for _, chunk := range chunks {
		chunkID := fmt.Sprintf("%s:chunk:%d", sourceID, chunk.Index)

		preview := chunk.Text
		if len(preview) > 200 {
			preview = safeUTF8TruncateIngest(preview, 200) + "..."
		}

		// Store in semantic memory for FTS5 retrieval.
		category := "ingested_document"
		if ft.IsCode() {
			category = "ingested_code"
		}
		key := fmt.Sprintf("%s:%d", fileName, chunk.Index)

		smID := db.NewID()
		if err := memRepo.StoreSemantic(ctx, smID, category, key, chunk.Text, 0.8); err != nil {
			log.Warn().Err(err).Int("chunk", chunk.Index).Msg("failed to store semantic memory for chunk")
			continue
		}

		// Persist a deterministic embedding immediately so ingested knowledge
		// participates in vector search without waiting for a backfill job.
		var embedding []float32
		if embedClient != nil {
			results, embedErr := embedClient.Embed(ctx, []string{chunk.Text})
			if embedErr == nil && len(results) > 0 {
				embedding = results[0]
			}
		}
		if len(embedding) > 0 {
			embBlob, _ := json.Marshal(embedding)
			if _, dbErr := store.ExecContext(ctx,
				`INSERT INTO embeddings (id, source_table, source_id, content_preview, embedding_blob, dimensions, created_at)
				 VALUES (?, 'ingested_knowledge', ?, ?, ?, ?, datetime('now'))`,
				chunkID, sourceID, preview, string(embBlob), len(embedding)); dbErr != nil {
				log.Warn().Err(dbErr).Int("chunk", chunk.Index).Msg("failed to store embedding entry for chunk")
				continue
			}
		}

		stored++
	}

	// Register in hippocampus as a knowledge source.
	hippo := db.NewHippocampusRegistry(store)
	description := fmt.Sprintf("Ingested %s (%s, %d chunks)", fileName, ft.Label(), stored)
	tableName := fmt.Sprintf("knowledge:%s", fileName)
	if err := hippo.RegisterTable(ctx, tableName, description, "[]"); err != nil {
		log.Warn().Err(err).Msg("failed to register ingested document in hippocampus")
	}

	return &IngestResult{
		FilePath:     path,
		FileType:     ft,
		ChunksStored: stored,
		TotalChars:   totalChars,
		SourceID:     sourceID,
	}, nil
}

// IngestDirectory ingests all supported files in a directory (non-recursive).
func IngestDirectory(ctx context.Context, store *db.Store, embedClient *llm.EmbeddingClient, dir string) ([]IngestResult, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory %s: %w", dir, err)
	}

	var results []IngestResult
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if FileTypeFromPath(path) == 0 {
			continue
		}
		result, err := IngestFile(ctx, store, embedClient, path)
		if err != nil {
			log.Warn().Err(err).Str("file", path).Msg("skipping file during directory ingestion")
			continue
		}
		results = append(results, *result)
	}

	return results, nil
}

// safeUTF8Truncate is reused from memory/manager.go — truncates to maxBytes
// while respecting UTF-8 boundaries. Defined here to avoid circular imports.
func safeUTF8TruncateIngest(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	p := maxBytes
	for p > 0 && s[p]>>6 == 0x2 {
		p--
	}
	return s[:p]
}
