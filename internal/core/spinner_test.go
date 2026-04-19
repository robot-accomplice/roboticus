package core

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSpinWhileBlocking_NonTerminalIsNoop pins the v1.0.6 rule that
// non-TTY output (pipes, file redirection, io.Discard, bytes.Buffer)
// produces ZERO spinner output. Otherwise log files and CI runs get
// cluttered with `\b` escape sequences and spinner characters.
func TestSpinWhileBlocking_NonTerminalIsNoop(t *testing.T) {
	var buf bytes.Buffer
	stop := SpinWhileBlocking(&buf)
	// Even with a brief delay, no spinner should be emitted — buf is
	// not a terminal.
	time.Sleep(200 * time.Millisecond)
	stop()
	if buf.Len() != 0 {
		t.Fatalf("non-TTY writer received %d bytes of spinner output; want 0 (content=%q)", buf.Len(), buf.String())
	}
}

// TestRunWithSpinner_NonTerminalStillRunsFn confirms the closure runs
// regardless of TTY status — the spinner is display-only, not a
// precondition for the work to execute.
func TestRunWithSpinner_NonTerminalStillRunsFn(t *testing.T) {
	var buf bytes.Buffer
	ran := false
	RunWithSpinner(&buf, "prefix: ", func() {
		ran = true
	})
	if !ran {
		t.Fatal("RunWithSpinner did not invoke fn on non-TTY writer; work must always run")
	}
	// Prefix should still appear even though spinner doesn't.
	if !strings.Contains(buf.String(), "prefix: ") {
		t.Fatalf("prefix missing from non-TTY output; got %q", buf.String())
	}
}

// TestRunWithSpinner_EmptyPrefixIsFine covers the degenerate case
// where a caller wants the spinner but no prefix. Should not panic,
// should not print a stray prefix, should still run fn.
func TestRunWithSpinner_EmptyPrefixIsFine(t *testing.T) {
	var buf bytes.Buffer
	ran := false
	RunWithSpinner(&buf, "", func() {
		ran = true
	})
	if !ran {
		t.Fatal("fn did not run with empty prefix")
	}
	if buf.Len() != 0 {
		t.Fatalf("empty prefix should produce zero output on non-TTY; got %q", buf.String())
	}
}

// TestSpinStopIsIdempotent confirms the returned stop function can
// be called multiple times safely. Callers using `defer stop()`
// plus an explicit `stop()` won't panic or deadlock.
func TestSpinStopIsIdempotent(t *testing.T) {
	var buf bytes.Buffer
	stop := SpinWhileBlocking(&buf)
	stop()
	stop() // must not panic, must not deadlock
	stop()
}

// TestSpinWhileBlocking_ConcurrentStop exercises the goroutine-safety
// of SpinStop from parallel callers. Not expected in real use but a
// cheap guarantee to make: internal channel close + sync.Once prevent
// panics.
func TestSpinWhileBlocking_ConcurrentStop(t *testing.T) {
	var buf bytes.Buffer
	stop := SpinWhileBlocking(&buf)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stop()
		}()
	}
	wg.Wait()
}
