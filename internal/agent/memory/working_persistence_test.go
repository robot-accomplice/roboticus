package memory

import (
	"context"
	"testing"
	"time"

	"roboticus/internal/db"
	"roboticus/testutil"
)

func TestPersistWorkingMemory(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	mgr := NewManager(DefaultConfig(), store)

	sessionID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, sessionID)

	// Seed working memory entries.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO working_memory (id, session_id, entry_type, content, importance)
		 VALUES (?, ?, 'goal', 'finish deployment', 8)`, db.NewID(), sessionID)
	_, _ = store.ExecContext(ctx,
		`INSERT INTO working_memory (id, session_id, entry_type, content, importance)
		 VALUES (?, ?, 'note', 'checking logs', 2)`, db.NewID(), sessionID)

	// Persist should mark entries.
	mgr.PersistWorkingMemory(ctx)

	var count int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM working_memory WHERE persisted_at IS NOT NULL`).Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 persisted entries, got %d", count)
	}
}

func TestVetWorkingMemory_DiscardsLowImportance(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	mgr := NewManager(DefaultConfig(), store)

	sessionID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, sessionID)

	// High-importance goal.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO working_memory (id, session_id, entry_type, content, importance, persisted_at)
		 VALUES (?, ?, 'goal', 'finish deployment', 8, datetime('now'))`, db.NewID(), sessionID)

	// Low-importance note.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO working_memory (id, session_id, entry_type, content, importance, persisted_at)
		 VALUES (?, ?, 'note', 'minor thought', 1, datetime('now'))`, db.NewID(), sessionID)

	result := mgr.VetWorkingMemory(ctx, DefaultVetConfig())

	if result.Retained != 1 {
		t.Errorf("expected 1 retained (the goal), got %d", result.Retained)
	}
	if result.Discarded < 1 {
		t.Errorf("expected at least 1 discarded (the note), got %d", result.Discarded)
	}
}

func TestVetWorkingMemory_RetainsGoals(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	mgr := NewManager(DefaultConfig(), store)

	sessionID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, sessionID)

	// Goal with low importance — should still be retained because it's a goal.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO working_memory (id, session_id, entry_type, content, importance, persisted_at)
		 VALUES (?, ?, 'goal', 'important goal', 2, datetime('now'))`, db.NewID(), sessionID)

	result := mgr.VetWorkingMemory(ctx, DefaultVetConfig())

	if result.Retained != 1 {
		t.Errorf("goals should be retained regardless of importance, got retained=%d", result.Retained)
	}
}

func TestVetWorkingMemory_DiscardsOldEntries(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	mgr := NewManager(DefaultConfig(), store)

	sessionID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, sessionID)

	// Entry from 2 days ago (exceeds 24h MaxAge).
	_, _ = store.ExecContext(ctx,
		`INSERT INTO working_memory (id, session_id, entry_type, content, importance, created_at, persisted_at)
		 VALUES (?, ?, 'goal', 'old goal', 8, datetime('now', '-2 days'), datetime('now'))`,
		db.NewID(), sessionID)

	// Fresh entry.
	_, _ = store.ExecContext(ctx,
		`INSERT INTO working_memory (id, session_id, entry_type, content, importance, persisted_at)
		 VALUES (?, ?, 'goal', 'fresh goal', 8, datetime('now'))`, db.NewID(), sessionID)

	result := mgr.VetWorkingMemory(ctx, VetConfig{
		MaxAge:        24 * time.Hour,
		MinImportance: 0,
		RetainTypes:   []string{"goal"},
	})

	if result.Retained != 1 {
		t.Errorf("expected 1 retained (fresh goal), got %d", result.Retained)
	}
}

func TestVetWorkingMemory_NoPersisted(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	mgr := NewManager(DefaultConfig(), store)

	result := mgr.VetWorkingMemory(ctx, DefaultVetConfig())

	if result.Retained != 0 || result.Discarded != 0 {
		t.Errorf("expected 0/0 for no persisted entries, got %d/%d",
			result.Retained, result.Discarded)
	}
}

func TestVetWorkingMemory_ClearsPersistedFlag(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	mgr := NewManager(DefaultConfig(), store)

	sessionID := db.NewID()
	_, _ = store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, sessionID)

	_, _ = store.ExecContext(ctx,
		`INSERT INTO working_memory (id, session_id, entry_type, content, importance, persisted_at)
		 VALUES (?, ?, 'goal', 'survive vet', 8, datetime('now'))`, db.NewID(), sessionID)

	mgr.VetWorkingMemory(ctx, DefaultVetConfig())

	// After vet, persisted_at should be cleared on survivors.
	var count int
	_ = store.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM working_memory WHERE persisted_at IS NOT NULL`).Scan(&count)
	if count != 0 {
		t.Errorf("persisted_at should be cleared after vet, got %d still set", count)
	}
}
