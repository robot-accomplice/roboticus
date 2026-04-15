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

// BootOptions controls the terminal display behavior during startup.
// Wired from CLI flags (--color, --theme, --nerdmode, --no-draw).
type BootOptions struct {
	ColorMode string // "auto", "always", "never"
	Theme     string // "ai-purple", "crt-green", "crt-orange", "terminal"
	NerdMode  bool   // ASCII-only icons, no typewrite animation
	NoDraw   bool   // Explicitly disable typewrite animation
}

// bootTheme is the active terminal theme for startup display.
// Initialized once in initBootTheme() and reused by all boot functions.
var bootTheme *core.Theme

func initBootTheme(opts BootOptions) {
	t := core.ResolveTheme(
		core.ParseColorMode(opts.ColorMode),
		core.ParseThemeVariant(opts.Theme),
	)
	if opts.NerdMode {
		_ = t.WithNerdMode(true).WithDraw(false)
	}
	if opts.NoDraw {
		_ = t.WithDraw(false)
	}
	bootTheme = t
}

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

	scan := 0
	if t.DrawEnabled() {
		scan = 55
	}

	fmt.Fprintln(os.Stderr)
	for _, line := range strings.Split(robotBanner, "\n") {
		if strings.Contains(line, "R O B O T I C U S") {
			before, _, found := strings.Cut(line, "R O B O T I C U S")
			if found {
				fmt.Fprintf(os.Stderr, "%s%s%s", p, before, r)
				t.TypewriteLine(fmt.Sprintf("%sR O B O T I C U S%s", p, r), 35)
			} else {
				fmt.Fprintln(os.Stderr, p+line+r)
			}
		} else if strings.Contains(line, "Autonomous Agent Runtime") {
			before, _, found := strings.Cut(line, "Autonomous Agent Runtime")
			if found {
				fmt.Fprintf(os.Stderr, "%s%s%s", p, before, r)
				t.TypewriteLine(fmt.Sprintf("%sAutonomous Agent Runtime v%s%s", d, version, r), 18)
			} else {
				fmt.Fprintln(os.Stderr, p+line+r)
			}
		} else if line == "" {
			fmt.Fprintln(os.Stderr)
		} else {
			fmt.Fprintln(os.Stderr, p+line+r)
			if scan > 0 {
				time.Sleep(time.Duration(scan) * time.Millisecond)
			}
		}
	}
	fmt.Fprintln(os.Stderr)
}

// bootStep prints a successful boot step: "  ✓ [ n/total] message"
func bootStep(n, total int, msg string) {
	t := theme()
	d, b, r := t.Dim(), t.Bold(), t.Reset()
	ok := t.IconOk()
	t.TypewriteLine(fmt.Sprintf("  %s %s[%2d/%d]%s %s%s%s", ok, d, n, total, r, b, msg, r), 4)
}

// bootStepWarn prints a warning boot step: "  ⚠ [ n/total] message"
func bootStepWarn(n, total int, msg string) {
	t := theme()
	d, r := t.Dim(), t.Reset()
	warn := t.IconWarn()
	t.TypewriteLine(fmt.Sprintf("  %s %s[%2d/%d]%s %s", warn, d, n, total, r, msg), 4)
}

// bootDetail prints a detail sub-line under a boot step: "       → label: value"
func bootDetail(label, value string) {
	t := theme()
	d, a, r := t.Dim(), t.Accent(), t.Reset()
	detail := t.IconDetail()
	t.TypewriteLine(fmt.Sprintf("       %s %s%s: %s%s%s", detail, d, label, a, value, r), 4)
}

// bootReady prints the final "Ready in Xms" line.
func bootReady(elapsed time.Duration) {
	t := theme()
	a, b, r := t.Accent(), t.Bold(), t.Reset()
	action := t.IconAction()
	fmt.Fprintln(os.Stderr)
	fmt.Fprint(os.Stderr, "  "+action+" ")
	t.Typewrite(fmt.Sprintf("%sReady%s in %s%s%s", b, r, a, elapsed.Round(time.Millisecond), r), 25)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr)
}

// bootSystemWarningsBanner prints any system warnings collected during
// boot (config defaults used, ambient DB creation, etc.) as a high-
// visibility block immediately before "Ready". The warnings have
// already been logged at zerolog Warn() level and recorded for the
// dashboard banner — this step is the *interactive* surface so an
// operator running `roboticus serve` in the foreground sees them
// before getting to the prompt.
//
// The block is deliberately styled to stand out: it uses
// bootStepWarn's icon, includes the title + remedy for each warning,
// and is preceded by a blank line so it doesn't blend with the boot
// step list above. Operators dismissing this as noise is a tradeoff
// we accept; the cost of NOT surfacing it (silent rogue-DB creation,
// wrong agent identity, etc.) was the v1.0.5 incident.
func bootSystemWarningsBanner(warnings []SystemWarningView) {
	if len(warnings) == 0 {
		return
	}
	t := theme()
	d, b, r := t.Dim(), t.Bold(), t.Reset()
	warn := t.IconWarn()
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  %s %sStartup Warnings%s (%d)\n", warn, b, r, len(warnings))
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "  %s %s%s%s\n", warn, b, w.Title, r)
		if w.Detail != "" {
			fmt.Fprintf(os.Stderr, "       %s%s%s\n", d, w.Detail, r)
		}
		if w.Remedy != "" {
			fmt.Fprintf(os.Stderr, "       %s→ %s%s\n", d, w.Remedy, r)
		}
	}
	fmt.Fprintln(os.Stderr)
}

// SystemWarningView is the daemon-side projection of
// core.SystemWarning. Defined here (not imported as core.SystemWarning
// directly) so the banner package stays decoupled from core's
// concrete type — easier to swap the surface later if needed.
type SystemWarningView struct {
	Title  string
	Detail string
	Remedy string
}
