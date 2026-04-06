package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// ObsidianScanner ingests Obsidian vault markdown files into semantic memory.
type ObsidianScanner struct {
	VaultPath string
}

// NewObsidianScanner creates a scanner for the given vault directory.
func NewObsidianScanner(vaultPath string) *ObsidianScanner {
	return &ObsidianScanner{VaultPath: vaultPath}
}

// Scan walks the vault directory and upserts new or modified files into semantic memory.
// Returns the number of files ingested.
func (s *ObsidianScanner) Scan(ctx context.Context, store *db.Store) int {
	if s.VaultPath == "" {
		return 0
	}

	info, err := os.Stat(s.VaultPath)
	if err != nil || !info.IsDir() {
		log.Debug().Str("vault", s.VaultPath).Msg("obsidian: vault path not found or not a directory")
		return 0
	}

	ingested := 0
	walkErr := filepath.Walk(s.VaultPath, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // skip errors
		}
		if fi.IsDir() {
			name := fi.Name()
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(fi.Name()), ".md") {
			return nil
		}

		relPath, _ := filepath.Rel(s.VaultPath, path)
		relPath = filepath.ToSlash(relPath)

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		content := string(data)
		if strings.TrimSpace(content) == "" {
			return nil
		}

		contentHash := hashContent(content)

		// Check if we already have this file with the same hash.
		var existingHash string
		err := store.QueryRowContext(ctx,
			`SELECT value FROM semantic_memory
			 WHERE category = 'obsidian' AND key = ?`, relPath).Scan(&existingHash)

		// We store the hash as the first line of the value to detect changes.
		if err == nil {
			// Entry exists; check if hash prefix matches.
			if strings.HasPrefix(existingHash, "hash:"+contentHash+"\n") {
				return nil // unchanged
			}
		}

		// Truncate content to a reasonable size for memory storage.
		storedValue := "hash:" + contentHash + "\n" + truncateContent(content, 4000)

		_, upsertErr := store.ExecContext(ctx,
			`INSERT INTO semantic_memory (id, category, key, value, confidence, memory_state, state_reason)
			 VALUES (?, 'obsidian', ?, ?, 0.8, 'active', 'obsidian vault scan')
			 ON CONFLICT(category, key) DO UPDATE SET
			   value = excluded.value,
			   confidence = MAX(confidence, 0.8),
			   memory_state = 'active',
			   state_reason = 'obsidian vault scan',
			   updated_at = datetime('now')`,
			db.NewID(), relPath, storedValue)
		if upsertErr != nil {
			log.Warn().Err(upsertErr).Str("path", relPath).Msg("obsidian: failed to upsert note")
			return nil
		}
		ingested++
		return nil
	})

	if walkErr != nil {
		log.Warn().Err(walkErr).Msg("obsidian: vault walk error")
	}

	if ingested > 0 {
		log.Info().Int("ingested", ingested).Str("vault", s.VaultPath).Msg("obsidian: vault scan complete")
	}

	return ingested
}

func hashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:8])
}

func truncateContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen]
}
