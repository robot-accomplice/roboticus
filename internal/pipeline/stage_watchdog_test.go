// stage_watchdog_test.go pins the v1.0.6 contract for the pipeline
// stage liveness watchdog: any single stage that runs longer than
// stageLivenessThreshold must produce a periodic warning log
// identifying the in-flight stage and how long it's been running.
//
// This is what closes the diagnostic gap that produced the v1.0.5
// fresh-state cold-start hang report ("first live pipeline turn
// started, soak did not progress past the first scenario even after
// extended wait"). With the watchdog in place, an operator running
// `roboticus serve` and watching the daemon's log output sees:
//
//   pipeline stage running longer than expected — possible
//     cold-start latency or hang
//   { "stage": "stage_inference", "running_for": "20s" }
//
// repeated every probe interval. Identifies the stuck stage without
// needing to kill -QUIT and parse a goroutine dump.
//
// What this test does and doesn't cover:
//   * TraceRecorder.CurrentSpan returns the in-flight span — yes
//   * CurrentSpan returns empty between spans — yes
//   * The watchdog goroutine fires logs at the documented
//     thresholds — covered structurally (the log call lives in
//     runStageWatchdog after the threshold check); not asserted
//     end-to-end here because intercepting zerolog output in a
//     unit test is fiddly and brittle relative to what it'd
//     catch. The CurrentSpan + threshold logic is the part most
//     likely to regress, so that's where the assertions live.

package pipeline

import (
	"testing"
	"time"
)

// TestTraceRecorder_CurrentSpan_EmptyBetweenSpans verifies that
// CurrentSpan returns the zero value when no span is active —
// otherwise the watchdog would log "stage  running for X" with an
// empty stage name during inter-stage gaps, which is just noise.
func TestTraceRecorder_CurrentSpan_EmptyBetweenSpans(t *testing.T) {
	tr := NewTraceRecorder()

	cs := tr.CurrentSpan()
	if cs.Name != "" {
		t.Fatalf("CurrentSpan on fresh recorder must be empty; got %q", cs.Name)
	}

	tr.BeginSpan("test_stage")
	cs = tr.CurrentSpan()
	if cs.Name != "test_stage" {
		t.Fatalf("CurrentSpan after BeginSpan must return that name; got %q", cs.Name)
	}

	tr.EndSpan("ok")
	cs = tr.CurrentSpan()
	if cs.Name != "" {
		t.Fatalf("CurrentSpan after EndSpan must be empty again; got %q", cs.Name)
	}
}

// TestTraceRecorder_CurrentSpan_DurationGrows verifies that the
// reported Duration is the live wall-clock duration since BeginSpan,
// not a snapshot frozen at start. The watchdog's threshold check
// depends on this — without a live-growing duration, the watchdog
// would never see the stage exceed the threshold and would never
// log.
func TestTraceRecorder_CurrentSpan_DurationGrows(t *testing.T) {
	tr := NewTraceRecorder()
	tr.BeginSpan("slow_stage")

	cs1 := tr.CurrentSpan()
	if cs1.Name != "slow_stage" {
		t.Fatalf("expected slow_stage; got %q", cs1.Name)
	}

	time.Sleep(50 * time.Millisecond)

	cs2 := tr.CurrentSpan()
	if cs2.Duration <= cs1.Duration {
		t.Fatalf("CurrentSpan.Duration must reflect live wall-clock; cs1=%v cs2=%v", cs1.Duration, cs2.Duration)
	}
	if cs2.Duration < 50*time.Millisecond {
		t.Fatalf("expected duration ≥ 50ms after sleep; got %v", cs2.Duration)
	}
}

// TestTraceRecorder_CurrentSpan_ConcurrentSafe verifies that
// CurrentSpan can be called concurrently with BeginSpan/EndSpan
// without races. The watchdog goroutine reads CurrentSpan while
// the pipeline goroutine is mutating spans; both must be safe.
//
// Run with -race to catch any missing synchronization.
func TestTraceRecorder_CurrentSpan_ConcurrentSafe(t *testing.T) {
	tr := NewTraceRecorder()
	stop := make(chan struct{})

	// Reader: simulates the watchdog goroutine.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
				_ = tr.CurrentSpan()
			}
		}
	}()

	// Writer: simulates the pipeline goroutine cycling through stages.
	for i := 0; i < 100; i++ {
		tr.BeginSpan("stage_" + string(rune('A'+i%26)))
		tr.Annotate("iteration", i)
		tr.EndSpan("ok")
	}
	close(stop)
	<-readerDone
}
