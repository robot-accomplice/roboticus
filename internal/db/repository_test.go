package db

import (
	"context"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSessionRepository_CreateAndFind(t *testing.T) {
	store := tempStore(t)
	repo := NewSessionRepository(store)
	ctx := context.Background()

	id := NewID()
	if err := repo.CreateSession(ctx, id, "agent1", "api"); err != nil {
		t.Fatal(err)
	}

	found, err := repo.FindActiveSession(ctx, "agent1", "api")
	if err != nil {
		t.Fatal(err)
	}
	if found != id {
		t.Errorf("found = %q, want %q", found, id)
	}
}

func TestSessionRepository_FindNonexistent(t *testing.T) {
	store := tempStore(t)
	repo := NewSessionRepository(store)
	ctx := context.Background()

	found, err := repo.FindActiveSession(ctx, "nobody", "nowhere")
	if err != nil {
		t.Fatal(err)
	}
	if found != "" {
		t.Errorf("expected empty, got %q", found)
	}
}

func TestSessionRepository_SetNickname(t *testing.T) {
	store := tempStore(t)
	repo := NewSessionRepository(store)
	ctx := context.Background()

	id := NewID()
	_ = repo.CreateSession(ctx, id, "agent1", "api")
	if err := repo.SetNickname(ctx, id, "Test Chat"); err != nil {
		t.Fatal(err)
	}

	var nickname *string
	_ = store.QueryRowContext(ctx, `SELECT nickname FROM sessions WHERE id = ?`, id).Scan(&nickname)
	if nickname == nil || *nickname != "Test Chat" {
		t.Errorf("nickname = %v, want 'Test Chat'", nickname)
	}
}

func TestSessionRepository_StoreAndLoadMessages(t *testing.T) {
	store := tempStore(t)
	repo := NewSessionRepository(store)
	ctx := context.Background()

	sessionID := NewID()
	_ = repo.CreateSession(ctx, sessionID, "agent1", "api")

	_ = repo.StoreMessage(ctx, NewID(), sessionID, "user", "Hello")
	_ = repo.StoreMessage(ctx, NewID(), sessionID, "assistant", "Hi there!")
	_ = repo.StoreMessage(ctx, NewID(), sessionID, "user", "How are you?")

	msgs, err := repo.LoadMessages(ctx, sessionID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "Hello" {
		t.Errorf("msg[0] = %v", msgs[0])
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("msg[1].Role = %q, want assistant", msgs[1].Role)
	}
}

func TestSessionRepository_LoadMessages_Empty(t *testing.T) {
	store := tempStore(t)
	repo := NewSessionRepository(store)
	ctx := context.Background()

	msgs, err := repo.LoadMessages(ctx, "nonexistent", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestSessionRepository_RecordInferenceCost(t *testing.T) {
	store := tempStore(t)
	repo := NewSessionRepository(store)
	ctx := context.Background()

	err := repo.RecordInferenceCost(ctx, NewID(), "gpt-4o", "openai", 100, 50, 0.015)
	if err != nil {
		t.Fatal(err)
	}

	var count int
	_ = store.QueryRowContext(ctx, `SELECT COUNT(*) FROM inference_costs`).Scan(&count)
	if count != 1 {
		t.Errorf("inference_costs count = %d, want 1", count)
	}
}

func TestHippocampusRegistry_SyncAndList(t *testing.T) {
	store := tempStore(t)
	hippo := NewHippocampusRegistry(store)
	ctx := context.Background()

	if err := hippo.SyncBuiltinTables(ctx); err != nil {
		t.Fatal(err)
	}

	tables, err := hippo.ListTables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) < 10 {
		t.Errorf("expected >= 10 tables, got %d", len(tables))
	}

	// Check a specific table.
	found := false
	for _, tbl := range tables {
		if tbl.Name == "sessions" {
			found = true
			if tbl.Description == "" {
				t.Error("sessions description should not be empty")
			}
			break
		}
	}
	if !found {
		t.Error("sessions table not found in hippocampus")
	}
}

func TestHippocampusRegistry_RegisterCustomTable(t *testing.T) {
	store := tempStore(t)
	hippo := NewHippocampusRegistry(store)
	ctx := context.Background()

	err := hippo.RegisterTable(ctx, "custom_data", "Agent-created data store", `["id","value","created_at"]`)
	if err != nil {
		t.Fatal(err)
	}

	tables, _ := hippo.ListTables(ctx)
	found := false
	for _, tbl := range tables {
		if tbl.Name == "custom_data" {
			found = true
			if tbl.Description != "Agent-created data store" {
				t.Errorf("description = %q", tbl.Description)
			}
		}
	}
	if !found {
		t.Error("custom table not registered")
	}

	// Update should work (upsert).
	_ = hippo.RegisterTable(ctx, "custom_data", "Updated description", `["id","value","updated_at"]`)
	tables, _ = hippo.ListTables(ctx)
	for _, tbl := range tables {
		if tbl.Name == "custom_data" && tbl.Description != "Updated description" {
			t.Error("upsert did not update description")
		}
	}
}
