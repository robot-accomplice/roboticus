package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssessDerivedCorruption_RepairableDerivedOnly(t *testing.T) {
	store := tempDerivedRepairStore(t)

	assessment, err := store.assessQuickCheckReport([]string{
		"*** in database main ***",
		"Tree 233 page 27405: btreeInitPage() returns error code 11",
		"Tree 234 page 27410: btreeInitPage() returns error code 11",
		"Tree 101 page 27407: btreeInitPage() returns error code 11",
		"Tree 25 page 25 right child: Bad ptr map entry key=27150 expected=(5,25) got=(5,25618)",
	}, map[int]string{
		233: "turn_diagnostic_events",
		234: "sqlite_autoindex_turn_diagnostic_events_1",
		101: "pipeline_traces",
		25:  "memory_fts_data",
	})
	if err != nil {
		t.Fatalf("assessQuickCheckReport: %v", err)
	}
	if !assessment.Repairable {
		t.Fatalf("expected repairable assessment, got %#v", assessment)
	}
	got := strings.Join(assessment.Objects, ",")
	for _, want := range []string{"memory_fts_data", "pipeline_traces", "turn_diagnostic_events"} {
		if !strings.Contains(got, want) {
			t.Fatalf("assessment objects %q missing %q", got, want)
		}
	}
}

func TestAssessDerivedCorruption_NonRepairableUnknownTree(t *testing.T) {
	store := tempDerivedRepairStore(t)

	assessment, err := store.assessQuickCheckReport([]string{
		"*** in database main ***",
		"Tree 17 page 88: btreeInitPage() returns error code 11",
	}, map[int]string{
		17: "sessions",
	})
	if err != nil {
		t.Fatalf("assessQuickCheckReport: %v", err)
	}
	if assessment.Repairable {
		t.Fatalf("expected non-repairable assessment, got %#v", assessment)
	}
}

func TestRebuildDerivedStructures_RecreatesTracesAndBackfillsFTS(t *testing.T) {
	store := tempDerivedRepairStore(t)
	ctx := context.Background()

	mustExecDB(t, store, `INSERT INTO episodic_memory (id, classification, content) VALUES ('ep1', 'observation', 'observed content')`)
	mustExecDB(t, store, `INSERT INTO semantic_memory (id, category, key, value) VALUES ('sem1', 'project', 'name', 'roboticus')`)
	mustExecDB(t, store, `INSERT INTO procedural_memory (id, name, steps) VALUES ('proc1', 'deploy', 'step one')`)
	mustExecDB(t, store, `INSERT INTO relationship_memory (id, entity_id, entity_name, interaction_summary) VALUES ('rel1', 'ent1', 'john', 'spoke yesterday')`)
	mustExecDB(t, store, `INSERT INTO knowledge_facts (id, subject, relation, object) VALUES ('kf1', 'roboticus', 'uses', 'sqlite')`)
	mustExecDB(t, store, `INSERT INTO pipeline_traces (id, turn_id, session_id, channel, total_ms, stages_json) VALUES ('pt1', 'turn1', 'sess1', 'api', 10, '[]')`)
	mustExecDB(t, store, `INSERT INTO turn_diagnostic_events (id, turn_id, seq, event_type, at_ms) VALUES ('ev1', 'turn1', 1, 'foo', 0)`)
	mustExecDB(t, store, `INSERT INTO react_traces (id, pipeline_trace_id, react_json, created_at) VALUES ('rt1', 'pt1', '{}', datetime('now'))`)

	if err := store.rebuildDerivedStructures(ctx); err != nil {
		t.Fatalf("rebuildDerivedStructures: %v", err)
	}

	var traceCount int
	if err := store.QueryRowContext(ctx, `SELECT COUNT(*) FROM pipeline_traces`).Scan(&traceCount); err != nil {
		t.Fatalf("count pipeline_traces: %v", err)
	}
	if traceCount != 0 {
		t.Fatalf("pipeline_traces rows = %d, want 0", traceCount)
	}

	var eventCount int
	if err := store.QueryRowContext(ctx, `SELECT COUNT(*) FROM turn_diagnostic_events`).Scan(&eventCount); err != nil {
		t.Fatalf("count turn_diagnostic_events: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("turn_diagnostic_events rows = %d, want 0", eventCount)
	}

	var ftsCount int
	if err := store.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_fts`).Scan(&ftsCount); err != nil {
		t.Fatalf("count memory_fts: %v", err)
	}
	if ftsCount < 5 {
		t.Fatalf("memory_fts rows = %d, want at least 5", ftsCount)
	}
}

func TestBackupDamagedDatabaseFiles_CopiesDatabaseArtifacts(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	sqlDB, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE demo (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create demo: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	store := &Store{dbPath: dbPath}
	if err := store.backupDamagedDatabaseFiles(); err != nil {
		t.Fatalf("backupDamagedDatabaseFiles: %v", err)
	}

	matches, err := filepath.Glob(dbPath + ".corrupt.*.bak")
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected backup copy of damaged db")
	}
}

func mustExecDB(t *testing.T, store *Store, query string, args ...any) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func tempDerivedRepairStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
