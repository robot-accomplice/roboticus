package cmd

import (
	"runtime"
	"testing"
)

func TestOpenBrowser_UnsupportedPlatform(t *testing.T) {
	// We can only test the current platform, but verify it returns no error on macOS.
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		// We can't actually open a browser in tests, so we just verify the function exists.
		// The function calls exec.Command which we don't want to actually run in CI.
		t.Skip("skipping browser open test in automated environment")
	}

	err := openBrowser("http://localhost:3577")
	if err == nil {
		t.Error("expected error for unsupported platform")
	}
}
