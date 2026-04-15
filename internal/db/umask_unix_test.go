//go:build unix

// umask_unix_test.go provides the setUmaskForTest helper used by
// store_permissions_test.go to verify that db.Open() tightens file
// permissions to 0o600 even when the process inherits a permissive
// umask. Built only on Unix because syscall.Umask is Unix-only — and
// because the database permission story is moot on Windows, which uses
// ACLs rather than POSIX modes for the equivalent protection.

package db

import (
	"syscall"
	"testing"
)

// setUmaskForTest sets the process umask to mask and returns the
// previous value so the caller can restore it via defer. The
// process-wide nature of umask means concurrent tests that both call
// this would race; that's acceptable here because the perms tests are
// fast and don't run with t.Parallel().
func setUmaskForTest(t *testing.T, mask int) int {
	t.Helper()
	return syscall.Umask(mask)
}
