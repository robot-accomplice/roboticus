package daemon

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"time"

	"roboticus/internal/core"
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

// bootTheme is the active terminal theme for startup display.
// Initialized once in printBanner() and reused by all boot functions.
var bootTheme *core.Theme

func theme() *core.Theme {
	if bootTheme == nil {
		bootTheme = core.DetectTheme()
	}
	return bootTheme
}

// printBanner renders the ASCII robot banner to stderr with accent coloring.
// Lines containing "R O B O T I C U S" get the title treatment;
// the "Autonomous Agent Runtime" line gets the version appended.
func printBanner() {
	t := theme()
	p := t.Accent()
	d := t.Dim()
	r := t.Reset()

	fmt.Fprintln(os.Stderr)
	for _, line := range strings.Split(robotBanner, "\n") {
		if strings.Contains(line, "Autonomous Agent Runtime") {
			versioned := strings.Replace(line, "Autonomous Agent Runtime",
				fmt.Sprintf("Autonomous Agent Runtime v%s", version), 1)
			// Art in accent, version text in dim.
			before, after, found := strings.Cut(versioned, "Autonomous")
			if found {
				fmt.Fprintf(os.Stderr, "%s%s%s%sAutonomous%s%s\n", p, before, r, d, after, r)
			} else {
				fmt.Fprintln(os.Stderr, p+versioned+r)
			}
		} else if line == "" {
			fmt.Fprintln(os.Stderr)
		} else {
			fmt.Fprintln(os.Stderr, p+line+r)
		}
	}
	fmt.Fprintln(os.Stderr)
}

// bootStep prints a successful boot step: "  ✓ [ n/total] message"
func bootStep(n, total int, msg string) {
	t := theme()
	d, b, r := t.Dim(), t.Bold(), t.Reset()
	ok := t.IconOk()
	fmt.Fprintf(os.Stderr, "  %s %s[%2d/%d]%s %s%s%s\n", ok, d, n, total, r, b, msg, r)
}

// bootStepWarn prints a warning boot step: "  ⚠ [ n/total] message"
func bootStepWarn(n, total int, msg string) {
	t := theme()
	d, r := t.Dim(), t.Reset()
	warn := t.IconWarn()
	fmt.Fprintf(os.Stderr, "  %s %s[%2d/%d]%s %s\n", warn, d, n, total, r, msg)
}

// bootDetail prints a detail sub-line under a boot step: "       → label: value"
func bootDetail(label, value string) {
	t := theme()
	d, a, r := t.Dim(), t.Accent(), t.Reset()
	detail := t.IconDetail()
	fmt.Fprintf(os.Stderr, "       %s %s%s: %s%s%s\n", detail, d, label, a, value, r)
}

// bootReady prints the final "Ready in Xms" line.
func bootReady(elapsed time.Duration) {
	t := theme()
	a, b, r := t.Accent(), t.Bold(), t.Reset()
	action := t.IconAction()
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  %s %sReady%s in %s%s%s\n", action, b, r, a, elapsed.Round(time.Millisecond), r)
	fmt.Fprintln(os.Stderr)
}
