package testutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"roboticus/internal/core"
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

// BackupAndRestoreRoboticus creates a backup of ~/.roboticus before a test
// and restores it when the test finishes. Use this for any test that might
// modify the live database or config (e.g., production smoke tests).
func BackupAndRestoreRoboticus(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home dir: %v", err)
		return
	}
	roboticusDir := filepath.Join(home, ".roboticus")
	if _, err := os.Stat(roboticusDir); os.IsNotExist(err) {
		return // Nothing to back up.
	}

	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "roboticus-backup")

	// Copy the entire .roboticus directory.
	if err := copyDir(roboticusDir, backupPath); err != nil {
		t.Fatalf("backup ~/.roboticus: %v", err)
	}
	t.Logf("backed up ~/.roboticus to %s", backupPath)

	t.Cleanup(func() {
		// Restore from backup.
		if err := os.RemoveAll(roboticusDir); err != nil {
			t.Logf("warning: remove ~/.roboticus failed: %v", err)
		}
		if err := copyDir(backupPath, roboticusDir); err != nil {
			t.Logf("warning: restore ~/.roboticus failed: %v", err)
		} else {
			t.Log("restored ~/.roboticus from backup")
		}
	})
}

// copyDir recursively copies a directory.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(src, path)
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, info.Mode())
	})
}

// BGWorker creates a background worker tied to the test's lifecycle.
// When the test ends, the worker's context is cancelled and Drain is called,
// preventing goroutine leaks and TempDir cleanup races.
//
// This follows the orDone pattern: worker goroutines select on their work
// and the test context, exiting when either completes.
func BGWorker(t *testing.T, concurrency int) *core.BackgroundWorker {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	bgw := core.NewBackgroundWorkerWithContext(ctx, concurrency)
	t.Cleanup(func() {
		cancel()
		bgw.Drain(5 * time.Second)
	})
	return bgw
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
