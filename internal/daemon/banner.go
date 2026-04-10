package daemon

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Boot display — user-facing startup output to stderr.
// Matches Rust's banner.rs + serve.rs boot-step formatting.
// All output goes to stderr (not the structured logger) so it's visible
// in terminals but doesn't pollute JSON log streams.
// ---------------------------------------------------------------------------

//go:embed banner.txt
var robotBanner string

// version is set at build time via -ldflags.
var version = "dev"

// printBanner renders the ASCII robot banner to stderr.
// Lines containing "R O B O T I C U S" get the title treatment;
// the "Autonomous Agent Runtime" line gets the version appended.
func printBanner() {
	fmt.Fprintln(os.Stderr)
	for _, line := range strings.Split(robotBanner, "\n") {
		if strings.Contains(line, "Autonomous Agent Runtime") {
			// Replace the static text with the versioned form.
			versioned := strings.Replace(line, "Autonomous Agent Runtime",
				fmt.Sprintf("Autonomous Agent Runtime v%s", version), 1)
			fmt.Fprintln(os.Stderr, versioned)
		} else {
			fmt.Fprintln(os.Stderr, line)
		}
	}
	fmt.Fprintln(os.Stderr)
}

// bootStep prints a successful boot step: "  ✓ [ n/total] message"
func bootStep(n, total int, msg string) {
	fmt.Fprintf(os.Stderr, "  ✓ [%2d/%d] %s\n", n, total, msg)
}

// bootStepWarn prints a warning boot step: "  ⚠ [ n/total] message"
func bootStepWarn(n, total int, msg string) {
	fmt.Fprintf(os.Stderr, "  ⚠ [%2d/%d] %s\n", n, total, msg)
}

// bootDetail prints a detail sub-line under a boot step: "       → label: value"
func bootDetail(label, value string) {
	fmt.Fprintf(os.Stderr, "       → %s: %s\n", label, value)
}

// bootReady prints the final "Ready in Xms" line.
func bootReady(elapsed time.Duration) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  → Ready in %s\n", elapsed.Round(time.Millisecond))
	fmt.Fprintln(os.Stderr)
}
