package db

import (
	"context"
	"testing"
)

// TestFTSTriggerCompleteness_AllCoveredTiersStaySynchronized is the M3.1
// regression. It verifies that every tier currently covered by an FTS
// trigger pipeline (episodic, semantic, procedural, relationship) keeps
// memory_fts synchronized across INSERT, UPDATE, and DELETE — proving the
// gaps fixed by migration 048 stay closed and that future migrations can't
// silently regress the contract without this test failing.
//
// One subtest per tier; each runs the full insert/update/delete cycle and
// asserts memory_fts after each step. The assertions intentionally check
// BOTH presence (count) AND content (the FTS row reflects current
// underlying values), because a present-but-stale FTS row is exactly the
// failure mode the missing UPDATE triggers used to produce.
func TestFTSTriggerCompleteness_AllCoveredTiersStaySynchronized(t *testing.T) {
	for _, tc := range coveredFTSTiers() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			store := testTempStore(t)

			id := tc.insert(t, store, "alpha-version")
			assertFTSRow(t, store, tc.sourceTable, id, tc.expectedContent("alpha-version"))

			tc.update(t, store, id, "beta-version")
			assertFTSRow(t, store, tc.sourceTable, id, tc.expectedContent("beta-version"))

			tc.delete(t, store, id)
			assertFTSAbsent(t, store, tc.sourceTable, id)
		})
	}
}

// TestFTSTriggerCompleteness_BackfillIsIdempotent verifies that the
// migration 048 backfill is safe to re-run. We simulate the "applied
// twice" scenario by running the backfill SQL a second time after the
// migration has already populated memory_fts. The NOT IN guard must
// produce zero new rows.
func TestFTSTriggerCompleteness_BackfillIsIdempotent(t *testing.T) {
	store := testTempStore(t)

	// Seed one row per backfilled tier through the normal INSERT path so
	// the trigger handles the first FTS write.
	for _, tc := range coveredFTSTiers() {
		_ = tc.insert(t, store, "seed")
	}

	before := countFTS(t, store)

	// Re-run the exact backfill statements from migration 048. They
	// must be no-ops on already-current data.
	mustExec(t, store, `INSERT INTO memory_fts (content, source_table, source_id, category)
	                    SELECT value, 'semantic_memory', id, category
	                      FROM semantic_memory
	                     WHERE id NOT IN (
	                       SELECT source_id FROM memory_fts WHERE source_table = 'semantic_memory'
	                     )`)
	mustExec(t, store, `INSERT INTO memory_fts (content, category, source_table, source_id)
	                    SELECT name || ': ' || steps, 'procedural', 'procedural_memory', id
	                      FROM procedural_memory
	                     WHERE id NOT IN (
	                       SELECT source_id FROM memory_fts WHERE source_table = 'procedural_memory'
	                     )`)
	mustExec(t, store, `INSERT INTO memory_fts (content, category, source_table, source_id)
	                    SELECT COALESCE(entity_name, '') || ': ' || COALESCE(interaction_summary, ''),
	                           'relationship', 'relationship_memory', id
	                      FROM relationship_memory
	                     WHERE id NOT IN (
	                       SELECT source_id FROM memory_fts WHERE source_table = 'relationship_memory'
	                     )`)

	after := countFTS(t, store)
	if before != after {
		t.Fatalf("expected backfill to be a no-op on already-current data; before=%d after=%d", before, after)
	}
}

// ftsTierCase bundles the per-tier SQL needed to exercise the full
// insert/update/delete cycle plus the expected FTS content shape.
type ftsTierCase struct {
	name            string
	sourceTable     string
	insert          func(t *testing.T, store *Store, marker string) string
	update          func(t *testing.T, store *Store, id, marker string)
	delete          func(t *testing.T, store *Store, id string)
	expectedContent func(marker string) string
}

func coveredFTSTiers() []ftsTierCase {
	return []ftsTierCase{
		{
			name:        "episodic_memory",
			sourceTable: "episodic_memory",
			insert: func(t *testing.T, store *Store, marker string) string {
				id := NewID()
				mustExec(t, store,
					`INSERT INTO episodic_memory (id, classification, content) VALUES (?, ?, ?)`,
					id, "test", marker)
				return id
			},
			update: func(t *testing.T, store *Store, id, marker string) {
				mustExec(t, store, `UPDATE episodic_memory SET content = ? WHERE id = ?`, marker, id)
			},
			delete: func(t *testing.T, store *Store, id string) {
				mustExec(t, store, `DELETE FROM episodic_memory WHERE id = ?`, id)
			},
			expectedContent: func(marker string) string { return marker },
		},
		{
			// This is the headline M3.1 fix: semantic_memory previously had
			// no INSERT trigger, so this subtest used to fail at the very
			// first assertion. After migration 048 it passes end-to-end.
			name:        "semantic_memory",
			sourceTable: "semantic_memory",
			insert: func(t *testing.T, store *Store, marker string) string {
				id := NewID()
				mustExec(t, store,
					`INSERT INTO semantic_memory (id, category, key, value) VALUES (?, ?, ?, ?)`,
					id, "policy", "key-"+id, marker)
				return id
			},
			update: func(t *testing.T, store *Store, id, marker string) {
				mustExec(t, store, `UPDATE semantic_memory SET value = ? WHERE id = ?`, marker, id)
			},
			delete: func(t *testing.T, store *Store, id string) {
				mustExec(t, store, `DELETE FROM semantic_memory WHERE id = ?`, id)
			},
			expectedContent: func(marker string) string { return marker },
		},
		{
			// Second M3.1 fix: procedural_memory had no UPDATE trigger, so
			// the post-update FTS-row content assertion would have caught a
			// stale FTS row pointing at the original steps text.
			name:        "procedural_memory",
			sourceTable: "procedural_memory",
			insert: func(t *testing.T, store *Store, marker string) string {
				id := NewID()
				mustExec(t, store,
					`INSERT INTO procedural_memory (id, name, steps) VALUES (?, ?, ?)`,
					id, "wf-"+id, marker)
				return id
			},
			update: func(t *testing.T, store *Store, id, marker string) {
				mustExec(t, store, `UPDATE procedural_memory SET steps = ? WHERE id = ?`, marker, id)
			},
			delete: func(t *testing.T, store *Store, id string) {
				mustExec(t, store, `DELETE FROM procedural_memory WHERE id = ?`, id)
			},
			expectedContent: func(marker string) string { return marker },
		},
		{
			// Third M3.1 fix: relationship_memory had no UPDATE trigger.
			name:        "relationship_memory",
			sourceTable: "relationship_memory",
			insert: func(t *testing.T, store *Store, marker string) string {
				id := NewID()
				mustExec(t, store,
					`INSERT INTO relationship_memory (id, entity_id, entity_name, interaction_summary)
					 VALUES (?, ?, ?, ?)`,
					id, "ent-"+id, "name-"+id, marker)
				return id
			},
			update: func(t *testing.T, store *Store, id, marker string) {
				mustExec(t, store,
					`UPDATE relationship_memory SET interaction_summary = ? WHERE id = ?`,
					marker, id)
			},
			delete: func(t *testing.T, store *Store, id string) {
				mustExec(t, store, `DELETE FROM relationship_memory WHERE id = ?`, id)
			},
			expectedContent: func(marker string) string { return marker },
		},
	}
}

// assertFTSRow checks that exactly one memory_fts row exists for
// (sourceTable, sourceID) AND that its content contains the expected
// substring. The substring check rather than exact-equals is deliberate:
// some triggers prefix the content with name-or-key plus ":", so the
// marker we wrote will appear inside a slightly larger string.
func assertFTSRow(t *testing.T, store *Store, sourceTable, sourceID, expectedSubstr string) {
	t.Helper()
	rows, err := store.QueryContext(context.Background(),
		`SELECT content FROM memory_fts WHERE source_table = ? AND source_id = ?`,
		sourceTable, sourceID)
	if err != nil {
		t.Fatalf("query memory_fts: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var matches []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			t.Fatalf("scan memory_fts content: %v", err)
		}
		matches = append(matches, content)
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly one memory_fts row for %s/%s; got %d: %+v",
			sourceTable, sourceID, len(matches), matches)
	}
	if !contains(matches[0], expectedSubstr) {
		t.Fatalf("expected memory_fts content for %s/%s to contain %q; got %q",
			sourceTable, sourceID, expectedSubstr, matches[0])
	}
}

func assertFTSAbsent(t *testing.T, store *Store, sourceTable, sourceID string) {
	t.Helper()
	var count int
	if err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM memory_fts WHERE source_table = ? AND source_id = ?`,
		sourceTable, sourceID).Scan(&count); err != nil {
		t.Fatalf("query memory_fts count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 memory_fts rows for %s/%s after delete; got %d",
			sourceTable, sourceID, count)
	}
}

func countFTS(t *testing.T, store *Store) int {
	t.Helper()
	var n int
	if err := store.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM memory_fts`).Scan(&n); err != nil {
		t.Fatalf("count memory_fts: %v", err)
	}
	return n
}

func mustExec(t *testing.T, store *Store, query string, args ...any) {
	t.Helper()
	if _, err := store.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
