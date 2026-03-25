package db

import (
	"context"
	"testing"
)

func TestFindOrCreateSession(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// First call creates a new session.
	sess, err := store.FindOrCreateSession(ctx, "agent-1", "telegram:user123")
	if err != nil {
		t.Fatalf("FindOrCreateSession() error: %v", err)
	}
	if sess == nil {
		t.Fatal("session should not be nil")
	}
	if sess.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", sess.AgentID, "agent-1")
	}
	if sess.ScopeKey != "telegram:user123" {
		t.Errorf("ScopeKey = %q, want %q", sess.ScopeKey, "telegram:user123")
	}
	if sess.Status != "active" {
		t.Errorf("Status = %q, want %q", sess.Status, "active")
	}

	// Second call returns the same session.
	sess2, err := store.FindOrCreateSession(ctx, "agent-1", "telegram:user123")
	if err != nil {
		t.Fatalf("FindOrCreateSession() second call error: %v", err)
	}
	if sess2.ID != sess.ID {
		t.Errorf("second call should return same session ID: got %q, want %q", sess2.ID, sess.ID)
	}
}

func TestGetSession(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	sess, _ := store.FindOrCreateSession(ctx, "agent-1", "web:user1")

	got, err := store.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession() error: %v", err)
	}
	if got.ID != sess.ID {
		t.Errorf("GetSession() ID = %q, want %q", got.ID, sess.ID)
	}

	// Non-existent session returns nil.
	got, err = store.GetSession(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetSession() error: %v", err)
	}
	if got != nil {
		t.Error("GetSession() should return nil for nonexistent ID")
	}
}

func TestArchiveSession(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	sess, _ := store.FindOrCreateSession(ctx, "agent-1", "discord:guild1")

	if err := store.ArchiveSession(ctx, sess.ID); err != nil {
		t.Fatalf("ArchiveSession() error: %v", err)
	}

	got, _ := store.GetSession(ctx, sess.ID)
	if got.Status != "archived" {
		t.Errorf("Status = %q, want %q", got.Status, "archived")
	}

	// After archiving, FindActiveSession should return nil.
	active, _ := store.FindActiveSession(ctx, "agent-1", "discord:guild1")
	if active != nil {
		t.Error("archived session should not be found by FindActiveSession")
	}
}

func TestListSessions(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	_, _ = store.FindOrCreateSession(ctx, "agent-1", "scope1")
	_, _ = store.FindOrCreateSession(ctx, "agent-1", "scope2")
	_, _ = store.FindOrCreateSession(ctx, "agent-2", "scope1")

	sessions, err := store.ListSessions(ctx, "agent-1", 10)
	if err != nil {
		t.Fatalf("ListSessions() error: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("ListSessions() returned %d sessions, want 2", len(sessions))
	}
}

func TestInsertMessage(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	sess, _ := store.FindOrCreateSession(ctx, "agent-1", "web:test")

	msgID, err := store.InsertMessage(ctx, sess.ID, "user", "Hello, goboticus!")
	if err != nil {
		t.Fatalf("InsertMessage() error: %v", err)
	}
	if msgID == "" {
		t.Error("message ID should not be empty")
	}

	// Verify message was stored.
	var content string
	err = store.QueryRowContext(ctx,
		"SELECT content FROM session_messages WHERE id = ?", msgID).Scan(&content)
	if err != nil {
		t.Fatalf("message should exist: %v", err)
	}
	if content != "Hello, goboticus!" {
		t.Errorf("content = %q, want %q", content, "Hello, goboticus!")
	}
}
