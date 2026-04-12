package db

import (
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// runMigrations applies any SQL migration files with version numbers greater
// than the current schema version.
func (s *Store) runMigrations() error {
	var currentVersion int
	err := s.db.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&currentVersion)
	if err != nil {
		return core.WrapError(core.ErrDatabase, "failed to read schema version", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		// No migrations directory embedded — nothing to apply.
		return nil
	}

	// Sort by filename to ensure order.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Extract version number from filename: "003_context_checkpoint.sql" → 3
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			continue
		}
		ver, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}

		if ver <= currentVersion {
			continue
		}

		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return core.WrapError(core.ErrDatabase, fmt.Sprintf("failed to read migration %s", name), err)
		}

		sql := string(data)
		_, err = s.db.Exec(sql)
		if err != nil {
			return core.WrapError(core.ErrDatabase, fmt.Sprintf("migration %s failed", name), err)
		}

		_, err = s.db.Exec("INSERT INTO schema_version (version) VALUES (?)", ver)
		if err != nil {
			return core.WrapError(core.ErrDatabase, fmt.Sprintf("failed to record migration %d", ver), err)
		}

		log.Info().Str("file", name).Int("version", ver).Msg("applied migration")
	}

	return nil
}
