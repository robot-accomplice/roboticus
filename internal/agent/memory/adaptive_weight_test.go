package memory

import "testing"

func TestAdaptiveHybridWeight_Boundaries(t *testing.T) {
	tests := []struct {
		corpus int
		want   float64
	}{
		{0, 0.7},
		{500, 0.7},
		{999, 0.7},
		{1000, 0.5},
		{2500, 0.5},
		{4999, 0.5},
		{5000, 0.4},
		{7500, 0.4},
		{9999, 0.4},
		{10000, 0.3},
		{50000, 0.3},
		{100000, 0.3},
	}
	for _, tt := range tests {
		got := AdaptiveHybridWeight(tt.corpus)
		if got != tt.want {
			t.Errorf("AdaptiveHybridWeight(%d) = %f, want %f", tt.corpus, got, tt.want)
		}
	}
}

func TestAdaptiveHybridWeight_MonotonicallyDecreasing(t *testing.T) {
	// As corpus grows, vector weight should decrease (more FTS reliance).
	sizes := []int{100, 1000, 5000, 10000}
	prev := 1.0
	for _, size := range sizes {
		w := AdaptiveHybridWeight(size)
		if w > prev {
			t.Errorf("weight increased from %f to %f at corpus %d — should be monotonically decreasing", prev, w, size)
		}
		prev = w
	}
}
