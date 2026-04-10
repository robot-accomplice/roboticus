package core

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Terminal theme system — mirrors Rust's crates/roboticus-core/src/style.rs.
// Provides color codes, icons, and typewriter effects for user-facing output.
// ---------------------------------------------------------------------------

// ThemeVariant selects the color palette for terminal output.
type ThemeVariant int

const (
	// ThemeAiPurple is the default — purple accent with lavender body text.
	ThemeAiPurple ThemeVariant = iota
	// ThemeCrtGreen emulates a CRT terminal with green phosphor.
	ThemeCrtGreen
	// ThemeCrtOrange emulates a CRT terminal with orange/amber phosphor.
	ThemeCrtOrange
	// ThemeTerminal uses bold-only styling for maximum compatibility.
	ThemeTerminal
)

// ColorMode controls how color enablement is determined.
type ColorMode int

const (
	// ColorAuto detects from NO_COLOR env var and TTY status.
	ColorAuto ColorMode = iota
	// ColorAlways forces colors on regardless of environment.
	ColorAlways
	// ColorNever disables all color output.
	ColorNever
)

// Theme holds the resolved terminal style configuration.
type Theme struct {
	enabled  bool
	draw     bool
	variant  ThemeVariant
	nerdmode bool
}

// DetectTheme creates a Theme with automatic color/TTY detection.
func DetectTheme() *Theme {
	return ResolveTheme(ColorAuto, ThemeAiPurple)
}

// ResolveTheme creates a Theme with explicit color mode and variant.
func ResolveTheme(mode ColorMode, variant ThemeVariant) *Theme {
	var enabled bool
	switch mode {
	case ColorAlways:
		enabled = true
	case ColorNever:
		enabled = false
	case ColorAuto:
		noColor := os.Getenv("NO_COLOR")
		if noColor != "" {
			enabled = false
		} else {
			// Check if stderr is a terminal.
			fi, err := os.Stderr.Stat()
			if err == nil {
				enabled = fi.Mode()&os.ModeCharDevice != 0
			}
		}
	}
	return &Theme{
		enabled: enabled,
		draw:    false, // draw is opt-in via --nerdmode; typewrite adds ~6s to boot
		variant: variant,
	}
}

// PlainTheme returns a Theme with all styling disabled.
func PlainTheme() *Theme {
	return &Theme{}
}

// WithDraw enables or disables typewriter effects.
func (t *Theme) WithDraw(draw bool) *Theme {
	t.draw = draw
	return t
}

// WithNerdMode enables ASCII-only icons.
func (t *Theme) WithNerdMode(nerd bool) *Theme {
	t.nerdmode = nerd
	return t
}

// ColorsEnabled reports whether ANSI color codes are active.
func (t *Theme) ColorsEnabled() bool { return t.enabled }

// DrawEnabled reports whether typewriter effects are active.
func (t *Theme) DrawEnabled() bool { return t.draw }

// Variant returns the active color palette.
func (t *Theme) Variant() ThemeVariant { return t.variant }

// ---------------------------------------------------------------------------
// Icons — Rust parity: nerdmode gives ASCII, default gives unicode/emoji.
// ---------------------------------------------------------------------------

// IconOk returns the success icon.
func (t *Theme) IconOk() string {
	if t.nerdmode {
		return "[OK]"
	}
	return "✓"
}

// IconAction returns the action/progress icon.
func (t *Theme) IconAction() string {
	if t.nerdmode {
		return "[>>]"
	}
	return "→"
}

// IconWarn returns the warning icon.
func (t *Theme) IconWarn() string {
	if t.nerdmode {
		return "[!!]"
	}
	return "⚠"
}

// IconError returns the error icon.
func (t *Theme) IconError() string {
	if t.nerdmode {
		return "[XX]"
	}
	return "✗"
}

// IconDetail returns the detail/sub-item icon.
func (t *Theme) IconDetail() string {
	if t.nerdmode {
		return ">"
	}
	return "→"
}

// ---------------------------------------------------------------------------
// ANSI color codes — variant-specific palette.
// ---------------------------------------------------------------------------

// Accent returns the emphasis color escape code.
func (t *Theme) Accent() string {
	if !t.enabled {
		return ""
	}
	switch t.variant {
	case ThemeAiPurple:
		return "\x1b[38;5;177m"
	case ThemeCrtGreen:
		return "\x1b[38;5;46m"
	case ThemeCrtOrange:
		return "\x1b[38;5;208m"
	case ThemeTerminal:
		return "\x1b[1m"
	}
	return ""
}

// Dim returns the body-text color escape code.
func (t *Theme) Dim() string {
	if !t.enabled {
		return ""
	}
	switch t.variant {
	case ThemeAiPurple:
		return "\x1b[38;5;146m"
	case ThemeCrtGreen:
		return "\x1b[38;5;40m"
	case ThemeCrtOrange:
		return "\x1b[38;5;172m"
	case ThemeTerminal:
		return ""
	}
	return ""
}

// Bold returns the bold escape code.
func (t *Theme) Bold() string {
	if !t.enabled {
		return ""
	}
	return "\x1b[1m"
}

// Reset returns the soft-reset escape code (with variant tint).
func (t *Theme) Reset() string {
	if !t.enabled {
		return ""
	}
	switch t.variant {
	case ThemeAiPurple:
		return "\x1b[0m\x1b[38;5;146m"
	case ThemeCrtGreen:
		return "\x1b[0m\x1b[38;5;40m"
	case ThemeCrtOrange:
		return "\x1b[0m\x1b[38;5;172m"
	case ThemeTerminal:
		return "\x1b[0m"
	}
	return "\x1b[0m"
}

// HardReset returns the plain ANSI reset (no tint).
func (t *Theme) HardReset() string {
	if !t.enabled {
		return ""
	}
	return "\x1b[0m"
}

// Warn returns the warning color (bright yellow).
func (t *Theme) Warn() string {
	if !t.enabled {
		return ""
	}
	return "\x1b[93m"
}

// Error returns the error color (bright red).
func (t *Theme) Error() string {
	if !t.enabled {
		return ""
	}
	return "\x1b[91m"
}

// Success returns the success color (bright green).
func (t *Theme) Success() string {
	if !t.enabled {
		return ""
	}
	return "\x1b[92m"
}

// Info returns the info color (bright cyan).
func (t *Theme) Info() string {
	if !t.enabled {
		return ""
	}
	return "\x1b[96m"
}

// ---------------------------------------------------------------------------
// Typewriter effects — character-by-character output.
// ---------------------------------------------------------------------------

// Typewrite prints text to stderr character-by-character.
// ANSI escape sequences are emitted instantly (no per-char delay).
func (t *Theme) Typewrite(text string, delayMs int) {
	if !t.draw || delayMs == 0 {
		fmt.Fprint(os.Stderr, text)
		return
	}

	w := os.Stderr
	inEscape := false
	for _, ch := range text {
		_, _ = fmt.Fprintf(w, "%c", ch)
		if ch == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if ch == 'm' {
				inEscape = false
			}
			continue
		}
		if ch == '\n' {
			continue
		}
		flush(w)
		time.Sleep(time.Duration(delayMs) * time.Millisecond)
	}
}

// TypewriteLine prints text with typewriter effect followed by a newline.
func (t *Theme) TypewriteLine(text string, delayMs int) {
	t.Typewrite(text, delayMs)
	fmt.Fprintln(os.Stderr)
}

// flush syncs the writer if it supports Sync (os.File does).
func flush(w io.Writer) {
	if f, ok := w.(*os.File); ok {
		_ = f.Sync()
	}
}

// ---------------------------------------------------------------------------
// Spinner frames for activity indicators.
// ---------------------------------------------------------------------------

// SpinnerFrames is the braille spinner animation sequence.
var SpinnerFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

// SpinnerFrame returns the spinner character for the given tick.
func SpinnerFrame(tick int) rune {
	return SpinnerFrames[tick%len(SpinnerFrames)]
}

// ---------------------------------------------------------------------------
// ParseThemeVariant resolves a string flag to a ThemeVariant.
// ---------------------------------------------------------------------------

// ParseThemeVariant converts a CLI flag string to a ThemeVariant.
func ParseThemeVariant(s string) ThemeVariant {
	switch strings.ToLower(s) {
	case "green", "crt-green":
		return ThemeCrtGreen
	case "orange", "crt-orange":
		return ThemeCrtOrange
	case "terminal", "plain":
		return ThemeTerminal
	default:
		return ThemeAiPurple
	}
}

// ParseColorMode converts a CLI flag string to a ColorMode.
func ParseColorMode(s string) ColorMode {
	switch strings.ToLower(s) {
	case "always":
		return ColorAlways
	case "never":
		return ColorNever
	default:
		return ColorAuto
	}
}
