package llm

import "testing"

func TestMetascore_HighQualityPreferred(t *testing.T) {
	high := ModelProfile{Quality: 0.9, Availability: 1.0, Cost: 0.01, Locality: 0, Confidence: 1.0}
	low := ModelProfile{Quality: 0.3, Availability: 1.0, Cost: 0.01, Locality: 0, Confidence: 1.0}
	if high.Metascore() <= low.Metascore() {
		t.Errorf("high quality (%f) should score higher than low (%f)", high.Metascore(), low.Metascore())
	}
}

func TestMetascore_UnavailablePenalized(t *testing.T) {
	available := ModelProfile{Quality: 0.5, Availability: 1.0, Cost: 0.01, Confidence: 1.0}
	broken := ModelProfile{Quality: 0.5, Availability: 0.0, Cost: 0.01, Confidence: 1.0}
	if broken.Metascore() >= available.Metascore() {
		t.Error("unavailable model should score lower")
	}
}

func TestMetascore_CheaperPreferred(t *testing.T) {
	cheap := ModelProfile{Quality: 0.5, Availability: 1.0, Cost: 0.0, Confidence: 1.0}
	expensive := ModelProfile{Quality: 0.5, Availability: 1.0, Cost: 1.0, Confidence: 1.0}
	if cheap.Metascore() <= expensive.Metascore() {
		t.Error("cheaper model should score higher")
	}
}

func TestBuildModelProfiles(t *testing.T) {
	targets := []RouteTarget{
		{Model: "gpt-4", Provider: "openai", Cost: 0.00006, IsLocal: false},
		{Model: "qwen", Provider: "ollama", Cost: 0, IsLocal: true},
	}
	qt := NewQualityTracker(100)
	qt.Record("gpt-4", 0.9)
	qt.Record("qwen", 0.6)

	profiles := BuildModelProfiles(targets, qt, nil, nil, nil)
	if len(profiles) != 2 {
		t.Fatalf("got %d profiles, want 2", len(profiles))
	}

	for _, p := range profiles {
		if p.Quality <= 0 {
			t.Errorf("model %s has zero quality", p.Model)
		}
	}
}

func TestSelectByMetascore(t *testing.T) {
	profiles := []ModelProfile{
		{Model: "weak", Provider: "a", Quality: 0.3, Availability: 1.0, Cost: 0, Confidence: 1.0},
		{Model: "strong", Provider: "b", Quality: 0.9, Availability: 1.0, Cost: 0, Confidence: 1.0},
	}
	best := SelectByMetascore(profiles, nil)
	if best == nil {
		t.Fatal("expected a selection")
	}
	if best.Model != "strong" {
		t.Errorf("got %s, want strong", best.Model)
	}
}
