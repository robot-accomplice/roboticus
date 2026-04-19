// spinner.go provides a reusable terminal spinner for indeterminate
// waits. Formalized in v1.0.6 as a platform rule: any CLI operation
// that blocks an operator for more than a few seconds MUST run under
// SpinWhileBlocking (or an equivalent feedback mechanism). Silent
// waits look like the terminal froze; operators cancel runs they
// should have let finish, or report non-bugs as bugs.
//
// The spinner animation characters live next door in style.go
// (SpinnerFrames / SpinnerFrame) — this file adds the goroutine
// machinery around the animation plus a TTY-detection gate so
// non-interactive output (pipes, CI, file redirection) stays clean.
//
// Usage pattern (inline with a long-running call):
//
//   fmt.Fprintf(w, "    Warm-up 1/2 (cold): ")
//   stop := core.SpinWhileBlocking(w)
//   result := doSomethingSlow()
//   stop()
//   fmt.Fprintf(w, "%s\n", renderResult(result))
//
// The spinner writes a single animated character immediately after
// the prefix. stop() erases the character and blocks until the
// animation goroutine has exited, so the subsequent write lands at
// the expected cursor position.

package core

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// spinnerTickInterval is how often a spinner frame redraws. 120ms is
// a sweet spot: fast enough to look active, slow enough to avoid
// flicker and excessive terminal write volume.
const spinnerTickInterval = 120 * time.Millisecond

// SpinStop is returned by SpinWhileBlocking. Calling it stops the
// animation, erases the current spinner character, and blocks until
// the goroutine has fully exited — so any subsequent write to the
// same writer appears where the prefix ended, not interleaved with a
// final spinner frame.
type SpinStop func()

// SpinWhileBlocking starts a spinner goroutine that writes animation
// frames to w every ~120ms until the returned SpinStop is called. If
// w is not a terminal (piped output, file redirection, io.Discard),
// SpinWhileBlocking returns a no-op SpinStop and does NOT emit any
// output — preserves clean log capture.
//
// Concurrency contract: the returned SpinStop is idempotent and safe
// to call from any goroutine. The spinner writes are serialized
// internally so callers don't need to lock w themselves.
func SpinWhileBlocking(w io.Writer) SpinStop {
	if !isTerminalWriter(w) {
		return func() {}
	}

	// Use ASCII fallback for non-braille-capable terminals. The
	// braille spinner is visually richer but some terminals (older
	// Windows console, SSH with limited fonts) render it as boxes.
	// The ASCII form is universal and still conveys activity.
	useASCII := os.Getenv("ROBOTICUS_SPINNER_ASCII") == "1"
	frames := SpinnerFrames
	if useASCII {
		frames = []rune{'|', '/', '-', '\\'}
	}

	var mu sync.Mutex
	stopCh := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once

	// Hide the cursor while the spinner is active. Without this the
	// terminal's blinking cursor sits immediately after the spinner
	// character and visually competes with it — exactly the
	// "confusing and unattractive" effect v1.0.6 flagged.
	// \033[?25l hides, \033[?25h restores. Universally supported
	// on modern terminal emulators (iTerm2, Terminal.app, Windows
	// Terminal, gnome-terminal, xterm, tmux/screen).
	//
	// Emit the first frame AFTER the cursor-hide so there's no
	// visible cursor-beside-spinner flicker between the two writes.
	mu.Lock()
	_, _ = fmt.Fprint(w, "\x1b[?25l")
	_, _ = fmt.Fprintf(w, "%c", frames[0])
	mu.Unlock()

	go func() {
		defer close(done)
		// Defense-in-depth cursor restore: if the goroutine panics
		// before hitting the normal stop path, still restore the
		// cursor so the operator's terminal doesn't end up with
		// cursor permanently hidden after a crash.
		defer func() {
			mu.Lock()
			_, _ = fmt.Fprint(w, "\x1b[?25h")
			mu.Unlock()
		}()

		ticker := time.NewTicker(spinnerTickInterval)
		defer ticker.Stop()
		i := 1
		for {
			select {
			case <-stopCh:
				// Erase the spinner character: backspace, space,
				// backspace. Works for ASCII frames. For Unicode
				// braille frames on some terminals this may leave
				// artifacts, but the follow-up print from the
				// caller overwrites the position anyway. Cursor
				// restore happens in the deferred func above.
				mu.Lock()
				_, _ = fmt.Fprint(w, "\b \b")
				mu.Unlock()
				return
			case <-ticker.C:
				mu.Lock()
				_, _ = fmt.Fprintf(w, "\b%c", frames[i%len(frames)])
				mu.Unlock()
				i++
			}
		}
	}()

	return func() {
		once.Do(func() {
			close(stopCh)
			<-done
		})
	}
}

// RunWithSpinner is the closure-wrapped convenience API around
// SpinWhileBlocking. It prints prefix (no newline) to w, starts the
// spinner, runs fn, and stops the spinner — all in one call. Use it
// when the slow operation is a self-contained closure and the
// spinner lifetime matches the function's lifetime exactly.
//
// Pattern:
//
//	var result SomeResult
//	core.RunWithSpinner(os.Stdout, "    Loading config: ", func() {
//	    result = slowLoad()
//	})
//	fmt.Printf("%v\n", result)
//
// Prefer SpinWhileBlocking directly when the spinner's start and
// stop happen in different callbacks (e.g., before/after pairs in
// an orchestrated flow), since the closure wrapper doesn't straddle
// call-boundary splits cleanly.
//
// Passing an empty prefix is allowed — RunWithSpinner behaves
// identically to SpinWhileBlocking in that case, but with the
// guaranteed-stop discipline of the defer.
func RunWithSpinner(w io.Writer, prefix string, fn func()) {
	if prefix != "" {
		_, _ = fmt.Fprint(w, prefix)
	}
	stop := SpinWhileBlocking(w)
	defer stop()
	fn()
}

// isTerminalWriter reports whether w is a terminal (stdout/stderr on
// a tty). Falls back to "not a terminal" for anything we can't stat
// — the conservative answer, since writing spinner escape sequences
// to a file is ugly.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
