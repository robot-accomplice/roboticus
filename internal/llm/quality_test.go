package llm

import (
	"sync"
	"testing"
)

func TestQualityTracker_RecordAndRetrieve(t *testing.T) {
	qt := NewQualityTracker(10)

	qt.Record("gpt-4", 0.8)
	qt.Record("gpt-4", 0.6)
	qt.Record("gpt-4", 1.0)

	got := qt.EstimatedQuality("gpt-4")
	want := 0.8 // (0.8 + 0.6 + 1.0) / 3
	if diff := got - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("EstimatedQuality = %f, want %f", got, want)
	}

	if count := qt.ObservationCount("gpt-4"); count != 3 {
		t.Errorf("ObservationCount = %d, want 3", count)
	}
}

func TestQualityTracker_DefaultQuality(t *testing.T) {
	qt := NewQualityTracker(10)

	got := qt.EstimatedQuality("unknown-model")
	if got != 0.5 {
		t.Errorf("EstimatedQuality for unknown model = %f, want 0.5", got)
	}

	if count := qt.ObservationCount("unknown-model"); count != 0 {
		t.Errorf("ObservationCount for unknown model = %d, want 0", count)
	}
}

func TestQualityTracker_SlidingWindowWraparound(t *testing.T) {
	qt := NewQualityTracker(3)

	// Fill the window: [0.2, 0.4, 0.6]
	qt.Record("m", 0.2)
	qt.Record("m", 0.4)
	qt.Record("m", 0.6)

	got := qt.EstimatedQuality("m")
	want := 0.4 // (0.2 + 0.4 + 0.6) / 3
	if diff := got - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("before wrap: EstimatedQuality = %f, want %f", got, want)
	}

	// Push one more — oldest (0.2) is overwritten: [0.9, 0.4, 0.6]
	qt.Record("m", 0.9)

	got = qt.EstimatedQuality("m")
	want = (0.9 + 0.4 + 0.6) / 3.0
	if diff := got - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("after wrap: EstimatedQuality = %f, want %f", got, want)
	}

	if count := qt.ObservationCount("m"); count != 3 {
		t.Errorf("ObservationCount after wrap = %d, want 3", count)
	}

	// Push two more — buffer fully cycled: [0.9, 0.1, 0.3]
	qt.Record("m", 0.1)
	qt.Record("m", 0.3)

	got = qt.EstimatedQuality("m")
	want = (0.9 + 0.1 + 0.3) / 3.0
	if diff := got - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("after full cycle: EstimatedQuality = %f, want %f", got, want)
	}
}

func TestQualityTracker_ClampValues(t *testing.T) {
	qt := NewQualityTracker(10)

	qt.Record("m", -0.5) // should clamp to 0
	qt.Record("m", 1.5)  // should clamp to 1

	got := qt.EstimatedQuality("m")
	want := 0.5 // (0 + 1) / 2
	if diff := got - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("EstimatedQuality with clamped values = %f, want %f", got, want)
	}
}

func TestQualityTracker_MultipleModels(t *testing.T) {
	qt := NewQualityTracker(10)

	qt.Record("fast", 0.3)
	qt.Record("fast", 0.5)
	qt.Record("smart", 0.9)
	qt.Record("smart", 0.7)

	fastQ := qt.EstimatedQuality("fast")
	smartQ := qt.EstimatedQuality("smart")

	if fastQ >= smartQ {
		t.Errorf("expected fast (%f) < smart (%f)", fastQ, smartQ)
	}
}

func TestQualityTracker_CanonicalizesProviderQualifiedModels(t *testing.T) {
	qt := NewQualityTracker(10)
	qt.Record("openrouter/openai/gpt-4o-mini", 0.7)
	qt.Record("openai/gpt-4o-mini", 0.9)

	got := qt.EstimatedQuality("openai/gpt-4o-mini")
	want := 0.8
	if diff := got - want; diff > 0.001 || diff < -0.001 {
		t.Fatalf("EstimatedQuality = %f, want %f", got, want)
	}
	if count := qt.ObservationCount("openrouter/openai/gpt-4o-mini"); count != 2 {
		t.Fatalf("ObservationCount = %d, want 2", count)
	}
}

func TestQualityTracker_ConcurrentAccess(t *testing.T) {
	qt := NewQualityTracker(100)

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 200

	// Concurrent writers.
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			model := "model-a"
			if id%2 == 0 {
				model = "model-b"
			}
			for i := 0; i < iterations; i++ {
				qt.Record(model, float64(i%10)/10.0)
			}
		}(g)
	}

	// Concurrent readers.
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = qt.EstimatedQuality("model-a")
				_ = qt.EstimatedQuality("model-b")
				_ = qt.ObservationCount("model-a")
			}
		}()
	}

	wg.Wait()

	// Verify no data corruption — counts should be positive and quality in range.
	for _, model := range []string{"model-a", "model-b"} {
		q := qt.EstimatedQuality(model)
		if q < 0 || q > 1 {
			t.Errorf("%s: quality %f out of range", model, q)
		}
		count := qt.ObservationCount(model)
		if count <= 0 || count > 100 {
			t.Errorf("%s: unexpected count %d", model, count)
		}
	}
}

func TestQualityTracker_DefaultWindowSize(t *testing.T) {
	qt := NewQualityTracker(0)
	if qt.windowSize != 100 {
		t.Errorf("default window size = %d, want 100", qt.windowSize)
	}

	qt2 := NewQualityTracker(-5)
	if qt2.windowSize != 100 {
		t.Errorf("negative window size defaulted to %d, want 100", qt2.windowSize)
	}
}

func TestQualityTracker_ClearModelAndAll(t *testing.T) {
	qt := NewQualityTracker(10)
	qt.Record("local-model", 0.2)
	qt.Record("local-model", 0.4)
	qt.Record("cloud-model", 0.9)

	if cleared := qt.ClearModel("local-model"); cleared != 2 {
		t.Fatalf("ClearModel(local-model) = %d, want 2", cleared)
	}
	if got := qt.ObservationCount("local-model"); got != 0 {
		t.Fatalf("ObservationCount(local-model) = %d, want 0", got)
	}
	if got := qt.ObservationCount("cloud-model"); got != 1 {
		t.Fatalf("ObservationCount(cloud-model) = %d, want 1", got)
	}

	if cleared := qt.ClearAll(); cleared != 1 {
		t.Fatalf("ClearAll() = %d, want 1", cleared)
	}
	if got := qt.ObservationCount("cloud-model"); got != 0 {
		t.Fatalf("ObservationCount(cloud-model) = %d, want 0 after ClearAll", got)
	}
}

func TestService_ResetQualityScores(t *testing.T) {
	svc := &Service{quality: NewQualityTracker(10)}
	svc.quality.Record("local-model", 0.2)
	svc.quality.Record("local-model", 0.5)
	svc.quality.Record("cloud-model", 0.9)

	if cleared := svc.ResetQualityScores("local-model"); cleared != 2 {
		t.Fatalf("ResetQualityScores(local-model) = %d, want 2", cleared)
	}
	if got := svc.quality.ObservationCount("local-model"); got != 0 {
		t.Fatalf("local-model observations = %d, want 0", got)
	}
	if got := svc.quality.ObservationCount("cloud-model"); got != 1 {
		t.Fatalf("cloud-model observations = %d, want 1", got)
	}

	if cleared := svc.ResetQualityScores(""); cleared != 1 {
		t.Fatalf("ResetQualityScores(all) = %d, want 1", cleared)
	}
}

func TestQualityFromResponse(t *testing.T) {
	tests := []struct {
		name string
		resp *Response
		want float64
	}{
		{"nil response", nil, 0},
		{"zero tokens", &Response{Usage: Usage{OutputTokens: 0}, Content: ""}, 0.15},
		{"50 tokens", &Response{Usage: Usage{OutputTokens: 50}}, 0.60},
		{"100 tokens", &Response{Usage: Usage{OutputTokens: 100}}, 0.60},
		{"200 tokens caps at 1", &Response{Usage: Usage{OutputTokens: 200}}, 0.60},
		{"fallback to content length", &Response{Content: "a]b]c]d]"}, 0.35},
		{"truncated response is penalized", &Response{Usage: Usage{OutputTokens: 60}, Content: "truncated", FinishReason: "length"}, 0.40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := qualityFromResponse(tt.resp)
			if diff := got - tt.want; diff > 0.01 || diff < -0.01 {
				t.Errorf("qualityFromResponse() = %f, want %f", got, tt.want)
			}
		})
	}
}
