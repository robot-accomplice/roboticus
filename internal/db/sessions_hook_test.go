package db

import (
	"context"
	"testing"
)

func TestArchiveSession_CallsPostHook(t *testing.T) {
	store := testTempStore(t)
	ctx := context.Background()

	sessionID := NewID()
	store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, sessionID)

	var hookedID string
	store.OnSessionArchived(func(ctx context.Context, id string) {
		hookedID = id
	})

	if err := store.ArchiveSession(ctx, sessionID); err != nil {
		t.Fatalf("ArchiveSession() error: %v", err)
	}
	if hookedID != sessionID {
		t.Errorf("hook should have received sessionID %s, got %q", sessionID, hookedID)
	}
}

func TestArchiveSession_HookFailureDoesNotBlockArchival(t *testing.T) {
	store := testTempStore(t)
	ctx := context.Background()

	sessionID := NewID()
	store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, sessionID)

	store.OnSessionArchived(func(ctx context.Context, id string) {
		panic("hook explosion")
	})

	if err := store.ArchiveSession(ctx, sessionID); err != nil {
		t.Fatalf("ArchiveSession() should succeed despite panicking hook: %v", err)
	}

	var status string
	store.QueryRowContext(ctx,
		`SELECT status FROM sessions WHERE id = ?`, sessionID).Scan(&status)
	if status != "archived" {
		t.Errorf("session should be archived, got %q", status)
	}
}

func TestArchiveSession_MultipleHooks(t *testing.T) {
	store := testTempStore(t)
	ctx := context.Background()

	sessionID := NewID()
	store.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status) VALUES (?, 'test', 'active')`, sessionID)

	var calls int
	store.OnSessionArchived(func(ctx context.Context, id string) { calls++ })
	store.OnSessionArchived(func(ctx context.Context, id string) { calls++ })

	store.ArchiveSession(ctx, sessionID)

	if calls != 2 {
		t.Errorf("expected 2 hook calls, got %d", calls)
	}
}
