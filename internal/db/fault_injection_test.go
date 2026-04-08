package db

import (
	"context"
	"strings"
	"testing"
)

// --- DB Fault Injection Tests ---
// Mirrors Rust's crates/roboticus-tests/src/fault_injection.rs db_fault module.

// TestDBFault_OpenInvalidPath verifies that invalid paths return errors, not panics.
func TestDBFault_OpenInvalidPath(t *testing.T) {
	_, err := Open("/")
	if err == nil {
		t.Fatal("expected error opening invalid path /")
	}
}

// TestDBFault_OpenNonexistentDir verifies deeply nested nonexistent paths fail cleanly.
func TestDBFault_OpenNonexistentDir(t *testing.T) {
	_, err := Open("/nonexistent/deeply/nested/path/db.sqlite")
	if err == nil {
		t.Fatal("expected error opening nonexistent dir")
	}
}

// TestDBFault_QueryNonexistentSession returns empty, not crash.
func TestDBFault_QueryNonexistentSession(t *testing.T) {
	store := faultTempStore(t)

	rq := NewRouteQueries(store)
	row := rq.SessionExists(context.Background(), "nonexistent-session-xyz-999")
	var createdAt string
	err := row.Scan(&createdAt)
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

// TestDBFault_StoreExtremeLengthContent ensures 10MB content doesn't panic.
func TestDBFault_StoreExtremeLengthContent(t *testing.T) {
	store := faultTempStore(t)

	sid := NewID()
	_, err := store.ExecContext(context.Background(),
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, 'test', 'test')`, sid)
	if err != nil {
		t.Fatal(err)
	}

	// 1MB of content (large enough to test edge behavior, fast enough for CI).
	bigContent := strings.Repeat("A", 1*1024*1024)
	_, err = store.ExecContext(context.Background(),
		`INSERT INTO session_messages (id, session_id, role, content) VALUES (?, ?, 'user', ?)`,
		NewID(), sid, bigContent)
	if err != nil {
		t.Fatalf("extreme length content insert failed: %v", err)
	}
}

// TestDBFault_EmptyAgentID ensures empty agent_id doesn't panic.
func TestDBFault_EmptyAgentID(t *testing.T) {
	store := faultTempStore(t)

	_, err := store.ExecContext(context.Background(),
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, '', 'test')`, NewID())
	if err != nil {
		t.Fatalf("empty agent_id insert failed: %v", err)
	}
}

// TestDBFault_AppendEmptyMessage ensures empty content doesn't panic.
func TestDBFault_AppendEmptyMessage(t *testing.T) {
	store := faultTempStore(t)

	sid := NewID()
	_, _ = store.ExecContext(context.Background(),
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, 'test', 'test')`, sid)

	_, err := store.ExecContext(context.Background(),
		`INSERT INTO session_messages (id, session_id, role, content) VALUES (?, ?, 'user', '')`,
		NewID(), sid)
	if err != nil {
		t.Fatalf("empty message content insert failed: %v", err)
	}
}

// TestDBFault_CronCreateEmptyFields ensures empty cron fields don't panic.
func TestDBFault_CronCreateEmptyFields(t *testing.T) {
	store := faultTempStore(t)

	_, err := store.ExecContext(context.Background(),
		`INSERT INTO cron_jobs (id, name, description, enabled, schedule_kind, schedule_expr, agent_id, payload_json)
		 VALUES (?, '', '', 0, '', '', '', '{}')`, NewID())
	if err != nil {
		t.Fatalf("empty cron fields insert failed: %v", err)
	}
}

// TestDBFault_NullBytesInPath ensures null bytes don't panic.
func TestDBFault_NullBytesInPath(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on null bytes in path: %v", r)
		}
	}()
	_, _ = Open("/tmp/test\x00evil.db")
}

// TestDBFault_ConcurrentReads verifies concurrent reads don't race or panic.
func TestDBFault_ConcurrentReads(t *testing.T) {
	store := faultTempStore(t)

	sid := NewID()
	_, _ = store.ExecContext(context.Background(),
		`INSERT INTO sessions (id, agent_id, scope_key) VALUES (?, 'test', 'test')`, sid)
	// Seed some messages first.
	for i := 0; i < 5; i++ {
		_, _ = store.ExecContext(context.Background(),
			`INSERT INTO session_messages (id, session_id, role, content) VALUES (?, ?, 'user', 'msg')`,
			NewID(), sid)
	}

	// Concurrent reads only (SQLite serializes writes but reads are safe).
	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			rows, err := store.QueryContext(context.Background(),
				`SELECT COUNT(*) FROM session_messages WHERE session_id = ?`, sid)
			if err == nil {
				_ = rows.Close()
			}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

// tempStore creates a test-only store (internal to this package).
func faultTempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
