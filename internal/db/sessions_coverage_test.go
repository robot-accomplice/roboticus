package db

import (
	"context"
	"testing"
)

func TestFindOrCreateSession_New(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	session, err := store.FindOrCreateSession(ctx, "agent1", "scope1")
	if err != nil {
		t.Fatalf("find or create: %v", err)
	}
	if session == nil {
		t.Fatal("should return session")
	}
	if session.ID == "" {
		t.Error("session ID should not be empty")
	}
}

func TestFindOrCreateSession_Existing(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	s1, _ := store.FindOrCreateSession(ctx, "agent1", "scope1")
	s2, _ := store.FindOrCreateSession(ctx, "agent1", "scope1")

	if s1.ID != s2.ID {
		t.Errorf("should return same session: %s != %s", s1.ID, s2.ID)
	}
}

func TestListSessions_Coverage(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	_, _ = store.FindOrCreateSession(ctx, "agent1", "scope1")
	_, _ = store.FindOrCreateSession(ctx, "agent1", "scope2")

	sessions, err := store.ListSessions(ctx, "agent1", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) < 2 {
		t.Errorf("sessions = %d, want >= 2", len(sessions))
	}
}

func TestInsertMessage_Coverage(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	session, _ := store.FindOrCreateSession(ctx, "agent1", "scope1")
	_, err := store.InsertMessage(ctx, session.ID, "user", "hello")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Verify.
	var count int
	row := store.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_messages WHERE session_id = ?`, session.ID)
	_ = row.Scan(&count)
	if count != 1 {
		t.Errorf("count = %d", count)
	}
}

func TestGetSession_Coverage(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	created, _ := store.FindOrCreateSession(ctx, "agent1", "scope1")
	got, err := store.GetSession(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("should find session")
	}
	if got.ID != created.ID {
		t.Errorf("id mismatch: %s != %s", got.ID, created.ID)
	}
}

func TestArchiveSession_Coverage(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	session, _ := store.FindOrCreateSession(ctx, "agent1", "scope1")
	err := store.ArchiveSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}

	// Should not find active session anymore.
	active, _ := store.FindActiveSession(ctx, "agent1", "scope1")
	if active != nil {
		t.Error("archived session should not be found as active")
	}
}
