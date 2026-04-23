package db

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

var quickCheckTreePattern = regexp.MustCompile(`Tree (\d+) page`)

var repairableDerivedObjects = map[string]struct{}{
	"pipeline_traces":                         {},
	"react_traces":                            {},
	"turn_diagnostic_events":                  {},
	"sqlite_autoindex_turn_diagnostic_events_1": {},
	"memory_fts":                              {},
	"memory_fts_data":                         {},
	"memory_fts_idx":                          {},
	"memory_fts_docsize":                      {},
	"memory_fts_content":                      {},
	"memory_fts_config":                       {},
}

type derivedRepairAssessment struct {
	ReportLines []string
	Objects     []string
	Repairable  bool
}

func isMalformedDatabaseError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "database disk image is malformed")
}

func (s *Store) maybeRepairDerivedCorruption(ctx context.Context, cause error) (bool, error) {
	if !isMalformedDatabaseError(cause) {
		return false, nil
	}

	s.repairMu.Lock()
	defer s.repairMu.Unlock()

	assessment, err := s.assessDerivedCorruption(ctx)
	if err != nil {
		return false, err
	}
	if len(assessment.ReportLines) == 1 && strings.EqualFold(strings.TrimSpace(assessment.ReportLines[0]), "ok") {
		return false, nil
	}
	if !assessment.Repairable {
		return false, fmt.Errorf("database corruption is not confined to rebuildable derived structures: %w", cause)
	}
	if err := s.backupDamagedDatabaseFiles(); err != nil {
		return false, err
	}
	if err := s.rebuildDerivedStructures(ctx); err != nil {
		return false, err
	}
	log.Warn().
		Strs("objects", assessment.Objects).
		Msg("repaired rebuildable derived database corruption")
	return true, nil
}

func (s *Store) assessDerivedCorruption(ctx context.Context) (derivedRepairAssessment, error) {
	lines, err := s.quickCheckLines(ctx)
	if err != nil {
		return derivedRepairAssessment{}, err
	}
	rootpages, err := s.rootpageNames(ctx)
	if err != nil {
		return derivedRepairAssessment{}, err
	}
	return s.assessQuickCheckReport(lines, rootpages)
}

func (s *Store) assessQuickCheckReport(lines []string, rootpages map[int]string) (derivedRepairAssessment, error) {
	if len(lines) == 1 && strings.EqualFold(strings.TrimSpace(lines[0]), "ok") {
		return derivedRepairAssessment{ReportLines: lines, Repairable: false}, nil
	}
	objects := make(map[string]struct{})
	for _, line := range lines {
		matches := quickCheckTreePattern.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			rootpage := strings.TrimSpace(match[1])
			for page, name := range rootpages {
				if fmt.Sprint(page) == rootpage {
					objects[name] = struct{}{}
					break
				}
			}
		}
	}

	if len(objects) == 0 {
		return derivedRepairAssessment{ReportLines: lines, Repairable: false}, nil
	}

	names := make([]string, 0, len(objects))
	for name := range objects {
		names = append(names, name)
		if _, ok := repairableDerivedObjects[name]; !ok {
			slices.Sort(names)
			return derivedRepairAssessment{ReportLines: lines, Objects: names, Repairable: false}, nil
		}
	}
	slices.Sort(names)
	return derivedRepairAssessment{ReportLines: lines, Objects: names, Repairable: true}, nil
}

func (s *Store) quickCheckLines(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA quick_check`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, errors.New("quick_check returned no rows")
	}
	return lines, nil
}

func (s *Store) rootpageNames(ctx context.Context) (map[int]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rootpage, name FROM sqlite_master WHERE rootpage > 0`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[int]string)
	for rows.Next() {
		var page int
		var name string
		if err := rows.Scan(&page, &name); err != nil {
			return nil, err
		}
		out[page] = name
	}
	return out, rows.Err()
}

func (s *Store) backupDamagedDatabaseFiles() error {
	if s.dbPath == "" || s.dbPath == ":memory:" || s.dbPath == "file::memory:" {
		return nil
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	for _, path := range []string{s.dbPath, s.dbPath + "-wal", s.dbPath + "-shm"} {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		backupPath := path + ".corrupt." + stamp + ".bak"
		if err := copyFile(path, backupPath); err != nil {
			return fmt.Errorf("backup damaged database file %s: %w", path, err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func (s *Store) rebuildDerivedStructures(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS react_traces`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS pipeline_traces`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS turn_diagnostic_events`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS memory_fts`); err != nil {
		return err
	}
	if err := s.initSchema(); err != nil {
		return err
	}
	if err := s.runMigrations(); err != nil {
		return err
	}
	if err := s.ensureOptionalColumns(); err != nil {
		return err
	}
	if err := s.rebuildMemoryFTS(ctx); err != nil {
		return err
	}
	lines, err := s.quickCheckLines(ctx)
	if err != nil {
		return err
	}
	if len(lines) != 1 || !strings.EqualFold(strings.TrimSpace(lines[0]), "ok") {
		return fmt.Errorf("quick_check still failing after derived repair: %s", strings.Join(lines, " | "))
	}
	return nil
}

func (s *Store) rebuildMemoryFTS(ctx context.Context) error {
	statements := []string{
		`DELETE FROM memory_fts`,
		`INSERT INTO memory_fts (content, category, source_table, source_id)
		 SELECT content, classification, 'episodic_memory', id
		   FROM episodic_memory`,
		`INSERT INTO memory_fts (content, source_table, source_id, category)
		 SELECT COALESCE(category, '') || ' ' || COALESCE(key, '') || ': ' || value,
		        'semantic_memory', id, category
		   FROM semantic_memory`,
		`INSERT INTO memory_fts (content, category, source_table, source_id)
		 SELECT name || ': ' || steps, 'procedural', 'procedural_memory', id
		   FROM procedural_memory`,
		`INSERT INTO memory_fts (content, category, source_table, source_id)
		 SELECT COALESCE(entity_name, '') || ': ' || COALESCE(interaction_summary, ''),
		        'relationship', 'relationship_memory', id
		   FROM relationship_memory`,
		`INSERT INTO memory_fts (content, category, source_table, source_id)
		 SELECT subject || ' ' || relation || ' ' || object,
		        'knowledge_facts', 'knowledge_facts', id
		   FROM knowledge_facts`,
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
