package scripts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPromptCompressionSoakScript_PinsQualityGate guards the release contract
// for prompt compression:
//  1. comparison is lane-based (compression OFF vs ON),
//  2. it exercises the live model path rather than cache replay,
//  3. it only runs where the script can safely force isolated config state,
//  4. it treats pass->fail drift as a regression.
//
// This is intentionally a source-level invariant test rather than a full
// end-to-end execution test; the paired soak itself is long-running and belongs
// in the L4 release gate, while this test prevents the harness contract from
// silently drifting in everyday CI.
func TestPromptCompressionSoakScript_PinsQualityGate(t *testing.T) {
	testDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	path := filepath.Join(testDir, "run-prompt-compression-soak.py")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(data)

	required := []string{
		`run_lane("baseline", "off"`,
		`run_lane("compressed", "on"`,
		`env.setdefault("SOAK_CLEAR_CACHE", "1")`,
		`env.setdefault("SOAK_BYPASS_CACHE", "1")`,
		`if not report_path.exists():`,
		`"harness_error": (`,
		`if SERVER_MODE not in {"clone", "fresh"}`,
		`compression caused a pass->fail regression`,
		`PASS no compression-specific regressions detected`,
	}
	for _, marker := range required {
		if !strings.Contains(text, marker) {
			t.Fatalf("prompt-compression soak script missing invariant marker %q", marker)
		}
	}
}
