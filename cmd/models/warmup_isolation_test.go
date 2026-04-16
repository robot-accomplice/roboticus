package models

import "testing"

// TestIsCloudModel_HonorsIsLocal pins the v1.0.6 warm-up-skip
// contract: cloud models (providers where is_local=false OR is_local
// unset) skip warm-up because their cold-start is server-side and
// opaque; local models go through warm-up.
//
// This test is the tripwire against a future config-shape change
// that accidentally flips the default behavior. If the contract
// regresses, baseline runs will either waste 2× longer on cloud
// models (warm-up fires unnecessarily) or fail to warm local models
// (polluting scored data with cold-start latency — the exact bug
// v1.0.6 was filed to fix).
func TestIsCloudModel_HonorsIsLocal(t *testing.T) {
	cases := []struct {
		name  string
		cfg   map[string]any
		model string
		want  bool
	}{
		{
			name: "local provider: NOT cloud",
			cfg: map[string]any{
				"providers": map[string]any{
					"ollama": map[string]any{"is_local": true},
				},
			},
			model: "ollama/qwen32b-tq",
			want:  false,
		},
		{
			name: "explicit non-local provider: IS cloud",
			cfg: map[string]any{
				"providers": map[string]any{
					"anthropic": map[string]any{"is_local": false},
				},
			},
			model: "anthropic/claude-sonnet-4",
			want:  true,
		},
		{
			name: "provider without is_local key: IS cloud (conservative default)",
			cfg: map[string]any{
				"providers": map[string]any{
					"openai": map[string]any{"url": "https://api.openai.com"},
				},
			},
			model: "openai/gpt-4",
			want:  true,
		},
		{
			name:  "no providers map at all: NOT cloud (unknown; default to local-style warm-up)",
			cfg:   map[string]any{},
			model: "ollama/qwen32b-tq",
			want:  false,
		},
		{
			name: "unknown provider name: NOT cloud (unknown; default to local-style warm-up)",
			cfg: map[string]any{
				"providers": map[string]any{
					"ollama": map[string]any{"is_local": true},
				},
			},
			model: "mystery/some-model",
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isCloudModel(tc.cfg, tc.model)
			if got != tc.want {
				t.Fatalf("isCloudModel(%q) = %v; want %v", tc.model, got, tc.want)
			}
		})
	}
}
