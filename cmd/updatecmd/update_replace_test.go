package updatecmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestReplaceRunningBinary_Atomicity pins the contract that replaceRunningBinary
// leaves execPath pointing at the stage's bytes after a successful call, and
// that the stage path no longer exists (it was consumed by the rename).
//
// This is the minimal shared-semantics regression across both OS build
// variants. The Windows variant does a rename-then-replace dance (see
// update_windows.go); this test exercises the Unix path directly and — on
// Windows — exercises the three-step dance on a non-running exe path, which
// is the exact code flow the updater runs.
//
// This regression exists because v1.0.6 audit flagged the bare os.Rename
// form as broken on Windows for the running-exe case. If a future refactor
// reintroduces the direct-rename pattern on Windows without the .old dance,
// a dedicated Windows CI job will fail; on Unix this test at least
// preserves the contract that replaceRunningBinary honors atomic swap.
func TestReplaceRunningBinary_Atomicity(t *testing.T) {
	tmp := t.TempDir()
	execPath := filepath.Join(tmp, "roboticus-test")
	stagePath := filepath.Join(tmp, ".roboticus-update-staging")

	if err := os.WriteFile(execPath, []byte("OLD_BINARY"), 0o755); err != nil {
		t.Fatalf("seed exec: %v", err)
	}
	if err := os.WriteFile(stagePath, []byte("NEW_BINARY"), 0o755); err != nil {
		t.Fatalf("seed stage: %v", err)
	}

	if err := replaceRunningBinary(stagePath, execPath); err != nil {
		t.Fatalf("replaceRunningBinary: %v", err)
	}

	// The exec path must now hold the new bytes.
	got, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatalf("read exec after replace: %v", err)
	}
	if string(got) != "NEW_BINARY" {
		t.Fatalf("exec path holds %q, want NEW_BINARY", string(got))
	}

	// The stage path must no longer exist — it was consumed by the rename.
	if _, err := os.Stat(stagePath); !os.IsNotExist(err) {
		t.Fatalf("stage path still exists after replace (err=%v)", err)
	}

	// Platform-specific: on Windows we expect a .old sidecar to exist
	// (the running exe was renamed aside before stage took its place).
	// On Unix the .old is not created — os.Rename covers the full swap.
	oldPath := execPath + ".old"
	_, oldErr := os.Stat(oldPath)
	switch runtime.GOOS {
	case "windows":
		if oldErr != nil {
			t.Fatalf("windows path should leave %s sidecar from rename-aside dance; got err=%v", oldPath, oldErr)
		}
	default:
		if oldErr == nil {
			t.Fatalf("unix path should NOT create %s; os.Rename alone suffices", oldPath)
		}
	}
}
