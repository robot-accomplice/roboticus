// store_permissions_test.go pins the v1.0.6 contract: every database file
// the codebase opens via db.Open() ends up with 0o600 mode after the call
// returns, regardless of the caller's umask and regardless of whether the
// file existed beforehand.
//
// Background — why this matters:
//
//   - SQLite (modernc.org/sqlite) creates the underlying file lazily,
//     using the process umask. macOS default umask is 022 → the new file
//     ends up at 0644 (world-readable). The DB contains conversation
//     history, working-memory contents that may include credentials or
//     PII the agent has observed, and lives alongside wallet.enc and
//     plugin keys.
//   - The roboticus codebase pre-v1.0.6 never explicitly chmod'd the
//     DB file. A cosmetic audit of ~/.roboticus showed the primary
//     state.db at 0600 (presumably manually chmod'd at some point)
//     but every other DB at 0644. Surfacing this as a default-shaped
//     bug rather than relying on every operator to remember.
//
// The chmod inside Open() is best-effort: a file owned by another user
// (e.g., the result of a prior sudo invocation) will EPERM out, and
// Open() logs a warning rather than failing the boot. That branch is
// intentionally not tested here — running tests as a different user
// would be deeply unportable. The contract these tests pin is the
// happy path: when Open() can chmod, it MUST end up at 0o600.

package db

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpen_FreshDatabaseHasRestrictiveMode verifies the v1.0.6 perms
// contract on a fresh install: a brand-new DB file created by Open()
// MUST end up at mode 0o600, even when the process umask would otherwise
// produce 0o644.
func TestOpen_FreshDatabaseHasRestrictiveMode(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fresh.db")

	// Force the test process to inherit a permissive umask so the test
	// is exercising the chmod path, not a coincidentally-restrictive
	// umask. 0022 is the macOS default.
	prevUmask := setUmaskForTest(t, 0o022)
	defer setUmaskForTest(t, prevUmask)

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat fresh DB: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("fresh DB mode = %o; want 0600 (umask was 022, so default would be 0644 without explicit chmod)", got)
	}
}

// TestOpen_TightensExistingDatabaseWithLoosePermissions verifies the
// upgrader-friendly behavior: an existing DB file at the loose 0o644
// mode (the bug-shaped state for every install pre-v1.0.6) gets
// tightened to 0o600 on the next Open(). Operators don't have to
// remember to chmod existing files.
func TestOpen_TightensExistingDatabaseWithLoosePermissions(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "preexisting.db")

	// Simulate an upgrader: file exists with the loose permissions the
	// pre-v1.0.6 default umask would have produced.
	if err := os.WriteFile(dbPath, []byte{}, 0o644); err != nil {
		t.Fatalf("seed loose-perm DB: %v", err)
	}

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat opened DB: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected Open() to tighten existing 0644 file to 0600; got %o", got)
	}
}

// TestOpen_TightensWALSidecarFiles verifies that the WAL-mode sidecar
// files (`<path>-wal` and `<path>-shm`) created by SQLite when the
// pragma is set also receive 0o600 permissions. These files can hold
// uncommitted page data and are equally sensitive.
func TestOpen_TightensWALSidecarFiles(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "with-wal.db")

	prevUmask := setUmaskForTest(t, 0o022)
	defer setUmaskForTest(t, prevUmask)

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Force a write so WAL sidecars are actually created. Without a
	// write, SQLite may not flush a -wal file at all, and the test
	// would silently skip a meaningful assertion.
	if _, err := store.db.Exec(`CREATE TABLE IF NOT EXISTS perms_test (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("write to force WAL flush: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO perms_test DEFAULT VALUES`); err != nil {
		t.Fatalf("insert to force WAL flush: %v", err)
	}

	for _, sidecar := range []string{dbPath + "-wal", dbPath + "-shm"} {
		info, err := os.Stat(sidecar)
		if err != nil {
			// WAL sidecars may not exist on every platform/run combo;
			// only assert when present.
			continue
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("WAL sidecar %s mode = %o; want 0600 (sidecars hold uncommitted page data and warrant the same protection as the main DB)",
				filepath.Base(sidecar), got)
		}
	}
}

// TestOpen_InMemoryDatabaseSkipsChmodSilently is a defensive guard: the
// chmod path must not blow up when Open() is called with `:memory:`
// (the form testTempStore and many tests use). There's no on-disk file
// to chmod, so the helper short-circuits.
func TestOpen_InMemoryDatabaseSkipsChmodSilently(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}
	defer func() { _ = store.Close() }()
	// Reaching this line proves Open() didn't panic or error on the
	// chmod path. Nothing further to assert — there's no file to stat.
}
