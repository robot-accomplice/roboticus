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

func TestApplyIntentEvidence_CanonicalObservationOverridesPriorAndAvoidsUnexercised(t *testing.T) {
	iq := NewIntentQualityTracker(16)
	if seeded := iq.SeedIntentBaselines([]IntentBaseline{{
		Model:       "openai/gpt-4o-mini",
		IntentClass: "TOOL_USE",
		Quality:     0.60,
	}}); seeded != 1 {
		t.Fatalf("seeded = %d, want 1", seeded)
	}
	iq.RecordWithIntent("openrouter/openai/gpt-4o-mini", IntentToolUse.String(), 0.82)

	profile := ModelProfile{
		Model:                  "openai/gpt-4o-mini",
		GlobalObservationCount: 3,
		Confidence:             1.0,
	}
	applyIntentEvidence(&profile, IntentToolUse.String(), iq)

	if profile.IntentObservationCount != 1 {
		t.Fatalf("IntentObservationCount = %d, want 1", profile.IntentObservationCount)
	}
	if profile.CapabilityEvidence != "observed_for_intent" {
		t.Fatalf("CapabilityEvidence = %q, want observed_for_intent", profile.CapabilityEvidence)
	}
}

func TestApplyIntentEvidence_ProviderAwareAliasResolvesBareLocalRouteName(t *testing.T) {
	iq := NewIntentQualityTracker(16)
	iq.RecordWithIntent("ollama/gemma4", IntentToolUse.String(), 0.74)

	profile := ModelProfile{
		Model:                  "gemma4",
		Provider:               "ollama",
		GlobalObservationCount: 1,
		Confidence:             1.0,
	}
	applyIntentEvidence(&profile, IntentToolUse.String(), iq)

	if profile.IntentObservationCount != 1 {
		t.Fatalf("IntentObservationCount = %d, want 1", profile.IntentObservationCount)
	}
	if profile.CapabilityEvidence != "observed_for_intent" {
		t.Fatalf("CapabilityEvidence = %q, want observed_for_intent", profile.CapabilityEvidence)
	}
}

func TestBuildModelProfiles_ProviderAwareAliasUsesQualifiedEvidenceForBareRouteTarget(t *testing.T) {
	targets := []RouteTarget{{
		Model:    "gemma4",
		Provider: "ollama",
		Tier:     TierMedium,
		IsLocal:  true,
		Cost:     0.0,
	}}
	qt := NewQualityTracker(16)
	qt.Record("ollama/gemma4", 0.72)
	qt.Record("ollama/gemma4", 0.82)

	profiles := BuildModelProfiles(targets, qt, nil, nil, nil)
	if len(profiles) != 1 {
		t.Fatalf("len(profiles) = %d, want 1", len(profiles))
	}
	if profiles[0].GlobalObservationCount != 2 {
		t.Fatalf("GlobalObservationCount = %d, want 2", profiles[0].GlobalObservationCount)
	}
	if profiles[0].Quality < 0.76 || profiles[0].Quality > 0.79 {
		t.Fatalf("Quality = %f, want around 0.77", profiles[0].Quality)
	}
}
