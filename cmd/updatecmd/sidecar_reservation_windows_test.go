//go:build windows

package updatecmd

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// TestReserveOldSidecar_FallsBackWhenCanonicalLocked_Windows is the
// Windows-native version of the sidecar fallback regression. The POSIX
// variant in sidecar_reservation_test.go simulates "unremovable file"
// via a RO parent directory (chmod 0o555), a trick that does NOT work
// on Windows — Windows lets admin-equivalent users remove files from
// read-only directories the same account owns.
//
// The correct Windows simulation is to hold an open handle on the
// canonical `.old` file with FILE_SHARE_NONE, so any attempted
// os.Remove on that file gets ERROR_SHARING_VIOLATION
// ("The process cannot access the file because it is being used by
// another process"). That's exactly the real-world state the P1-F
// fix was filed against: an antivirus scan, another updater
// instance, or Explorer's Properties dialog holding an open handle.
//
// Pre-v1.0.6 self-audit: the fallback test had a blanket
// `t.Skip("Windows needs a real handle lock")` — so Windows CI ran
// the fallback path ZERO times. This file closes that coverage gap.
func TestReserveOldSidecar_FallsBackWhenCanonicalLocked_Windows(t *testing.T) {
	tmp := t.TempDir()
	exe := filepath.Join(tmp, "roboticus.exe")
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatalf("seed exe: %v", err)
	}
	stuckOld := exe + ".old"
	if err := os.WriteFile(stuckOld, []byte("stuck"), 0o644); err != nil {
		t.Fatalf("seed stuck old: %v", err)
	}

	// Open the stuck file with exclusive sharing (dwShareMode=0)
	// so os.Remove will fail with ERROR_SHARING_VIOLATION.
	handle, err := openExclusiveHandle(stuckOld)
	if err != nil {
		t.Fatalf("open exclusive handle on %s: %v", stuckOld, err)
	}
	t.Cleanup(func() { _ = syscall.CloseHandle(handle) })

	got, err := reserveOldSidecar(exe)
	if err != nil {
		t.Fatalf("reserve under locked canonical: %v", err)
	}

	// Must not return canonical — it's locked.
	if got == stuckOld {
		t.Fatalf("expected timestamped fallback when canonical is handle-locked; got canonical %s", got)
	}
	// Fallback shape: `<exe>.old-<timestamp>`.
	if !strings.HasPrefix(got, exe+".old-") {
		t.Fatalf("fallback shape wrong: want prefix %s; got %s", exe+".old-", got)
	}

	// Stuck file must still be intact (the fallback never touched it).
	if _, err := os.Stat(stuckOld); err != nil {
		t.Fatalf("stuck file should still exist; got err=%v", err)
	}
}

// openExclusiveHandle opens path with FILE_SHARE_NONE (dwShareMode=0)
// so any other process's attempt to open or delete the file fails
// with ERROR_SHARING_VIOLATION. Returns the handle; caller must close
// it via syscall.CloseHandle.
func openExclusiveHandle(path string) (syscall.Handle, error) {
	utf16Path, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	createFileW := kernel32.NewProc("CreateFileW")

	const (
		GENERIC_READ          = 0x80000000
		OPEN_EXISTING         = 3
		FILE_ATTRIBUTE_NORMAL = 0x80
		// dwShareMode = 0 — NO sharing, so os.Remove fails while
		// this handle is open.
	)

	ret, _, callErr := createFileW.Call(
		uintptr(unsafe.Pointer(utf16Path)),
		GENERIC_READ,
		0, // dwShareMode = 0 — exclusive
		0, // lpSecurityAttributes = NULL
		OPEN_EXISTING,
		FILE_ATTRIBUTE_NORMAL,
		0, // hTemplateFile = NULL
	)
	handle := syscall.Handle(ret)
	if handle == syscall.InvalidHandle {
		return 0, callErr
	}
	return handle, nil
}
