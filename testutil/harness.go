package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"roboticus/internal/db"
)

// TempStore creates an in-memory SQLite store for testing. The store is
// automatically closed when the test completes.
func TempStore(t *testing.T) *db.Store {
	t.Helper()

	// Use a temp file rather than :memory: so the embedded migrations
	// directory is resolved correctly.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TempDir creates a temporary directory for test artifacts.
func TempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "roboticus-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
