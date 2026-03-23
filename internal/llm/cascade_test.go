package llm

import (
	"testing"
	"time"
)

func TestCascadeOptimizer_UnknownClassDefaultsCascade(t *testing.T) {
	co := NewCascadeOptimizer(50)
	if got := co.ShouldCascade("unknown"); got != StrategyCascade {
		t.Errorf("unknown class: got %v, want Cascade", got)
	}
}

func TestCascadeOptimizer_HighSuccessRateFavorsCascade(t *testing.T) {
	co := NewCascadeOptimizer(50)
	for i := 0; i < 20; i++ {
		co.Record(CascadeOutcome{
			QueryClass:    "simple",
			WeakModelUsed: true,
			WeakSucceeded: true,
			WeakLatency:   50 * time.Millisecond,
			StrongLatency: 500 * time.Millisecond,
		})
	}
	if got := co.ShouldCascade("simple"); got != StrategyCascade {
		t.Errorf("high success rate: got %v, want Cascade", got)
	}
}

func TestCascadeOptimizer_LowSuccessRateFavorsDirect(t *testing.T) {
	co := NewCascadeOptimizer(50)
	for i := 0; i < 20; i++ {
		co.Record(CascadeOutcome{
			QueryClass:    "complex",
			WeakModelUsed: true,
			WeakSucceeded: false,
			WeakLatency:   200 * time.Millisecond,
			StrongLatency: 300 * time.Millisecond,
		})
	}
	if got := co.ShouldCascade("complex"); got != StrategyDirect {
		t.Errorf("low success rate: got %v, want Direct", got)
	}
}

func TestCascadeOptimizer_WindowEviction(t *testing.T) {
	co := NewCascadeOptimizer(10)
	for i := 0; i < 20; i++ {
		co.Record(CascadeOutcome{QueryClass: "test", WeakModelUsed: true, WeakSucceeded: true})
	}
	_, size := co.Stats("test")
	if size > 10 {
		t.Errorf("window size = %d, want <= 10", size)
	}
}

func TestCascadeOptimizer_Stats(t *testing.T) {
	co := NewCascadeOptimizer(50)
	rate, size := co.Stats("empty")
	if rate != 0 || size != 0 {
		t.Errorf("empty stats: rate=%f, size=%d", rate, size)
	}

	co.Record(CascadeOutcome{QueryClass: "q", WeakModelUsed: true, WeakSucceeded: true})
	co.Record(CascadeOutcome{QueryClass: "q", WeakModelUsed: true, WeakSucceeded: false})
	rate, size = co.Stats("q")
	if size != 2 {
		t.Errorf("size = %d, want 2", size)
	}
	if rate != 0.5 {
		t.Errorf("rate = %f, want 0.5", rate)
	}
}

func TestCascadeOptimizer_FewSamplesDefaultsCascade(t *testing.T) {
	co := NewCascadeOptimizer(50)
	co.Record(CascadeOutcome{QueryClass: "sparse", WeakModelUsed: true, WeakSucceeded: false})
	co.Record(CascadeOutcome{QueryClass: "sparse", WeakModelUsed: true, WeakSucceeded: false})
	// Only 2 samples, below the 3-sample minimum.
	if got := co.ShouldCascade("sparse"); got != StrategyCascade {
		t.Errorf("few samples: got %v, want Cascade", got)
	}
}
