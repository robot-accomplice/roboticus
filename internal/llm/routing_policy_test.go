package llm

import "testing"

func TestInferRouteTargetTier(t *testing.T) {
	tests := []struct {
		model string
		want  ModelTier
	}{
		{model: "gpt-4o-mini", want: TierSmall},
		{model: "gemma3:12b", want: TierMedium},
		{model: "qwen2.5:32b", want: TierLarge},
		{model: "mixtral:8x7b", want: TierFrontier},
		{model: "kimi-k2-turbo-preview", want: TierFrontier},
	}

	for _, tc := range tests {
		if got := inferRouteTargetTier(tc.model); got != tc.want {
			t.Fatalf("inferRouteTargetTier(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}
