package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// ---------------------------------------------------------------------------
// Pipeline utility functions — mirrors Rust's crates/roboticus-pipeline/src/utils.rs
// ---------------------------------------------------------------------------

// IsModelProxyRole checks if a subagent role designates a model-proxy
// (not a taskable agent). Case-insensitive comparison.
func IsModelProxyRole(role string) bool {
	return strings.EqualFold(role, "model-proxy")
}

// SkillRegistryNamesFromDB collects all known skill names from the database.
// Returns a deduplicated set of skill names (lowercased) from enabled
// database-registered skills.
func SkillRegistryNamesFromDB(store *db.Store) map[string]struct{} {
	names := make(map[string]struct{})

	rows, err := store.QueryContext(context.Background(),
		`SELECT name FROM skills WHERE enabled = 1`)
	if err != nil {
		log.Warn().Err(err).Msg("pipeline: failed to query skill names from DB")
		return names
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		names[strings.ToLower(name)] = struct{}{}
	}
	return names
}

// SkillCapabilityLexiconFromDB collects capability-bearing enabled skill text
// from the authoritative DB inventory. Name and description are both included
// so capability-fit logic can match installed concepts like "obsidian" and
// "vault" from an `obsidian-vault` skill instead of pretending they are
// missing because the skill name contains punctuation.
func SkillCapabilityLexiconFromDB(store *db.Store) []string {
	if store == nil {
		return nil
	}

	rows, err := store.QueryContext(context.Background(),
		`SELECT name, COALESCE(description, ''), COALESCE(source_path, '') FROM skills WHERE enabled = 1`)
	if err != nil {
		log.Warn().Err(err).Msg("pipeline: failed to query capability lexicon from DB")
		return nil
	}
	defer func() { _ = rows.Close() }()

	var corpus []string
	for rows.Next() {
		var name, description, sourcePath string
		if err := rows.Scan(&name, &description, &sourcePath); err != nil {
			continue
		}
		sourcePath = strings.TrimSpace(sourcePath)
		if sourcePath != "" && !fileExists(sourcePath) {
			log.Debug().Str("skill", name).Str("path", sourcePath).Msg("pipeline: omitting enabled skill with missing source path from capability lexicon")
			continue
		}
		corpus = append(corpus, strings.TrimSpace(name), strings.TrimSpace(description))
		if sourcePath != "" {
			corpus = append(corpus, filepath.Base(filepath.Dir(sourcePath)))
		}
	}
	return corpus
}

// RuntimeCapabilityLexiconFromPruner returns capability-bearing text from the
// live tool surface when the configured pruner exposes it. Task synthesis uses
// this alongside DB-backed skill text so runtime tools are not misclassified as
// missing skills before pruning/execution has a chance to run.
func RuntimeCapabilityLexiconFromPruner(pruner ToolPruner) []string {
	if pruner == nil {
		return nil
	}
	provider, ok := pruner.(ToolCapabilityLexiconProvider)
	if !ok {
		return nil
	}
	return provider.ToolCapabilityLexicon()
}

// EnabledSkillSourcePathsFromDB returns concrete skill source files for enabled
// skills that still have a loadable on-disk artifact.
func EnabledSkillSourcePathsFromDB(store *db.Store) []string {
	if store == nil {
		return nil
	}
	rows, err := store.QueryContext(context.Background(),
		`SELECT COALESCE(source_path, '') FROM skills WHERE enabled = 1 AND COALESCE(source_path, '') <> ''`)
	if err != nil {
		log.Warn().Err(err).Msg("pipeline: failed to query skill source paths from DB")
		return nil
	}
	defer func() { _ = rows.Close() }()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			continue
		}
		if fileExists(path) {
			paths = append(paths, path)
		}
	}
	return paths
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ParseSkillsJSON parses a JSON skills array (from a subagent's skills_json
// column). Returns an empty slice on nil input, invalid JSON, or non-array JSON.
func ParseSkillsJSON(raw *string) []string {
	if raw == nil || *raw == "" {
		return nil
	}

	var result []string
	if err := json.Unmarshal([]byte(*raw), &result); err != nil {
		log.Warn().Err(err).Str("raw", *raw).Msg("pipeline: failed to parse skills JSON")
		return nil
	}
	return result
}
