package agent

import (
	"testing"
)

func TestRecommendationEngine_MemoryRecommendation(t *testing.T) {
	re := NewRecommendationEngine(0.5)
	recs := re.Suggest("do you remember what we discussed?", nil, nil)
	if len(recs) == 0 {
		t.Fatal("expected at least one recommendation")
	}
	found := false
	for _, r := range recs {
		if r.Action == "search_memory" && r.Category == "memory" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected search_memory recommendation")
	}
}

func TestRecommendationEngine_ToolRecommendation_Filesystem(t *testing.T) {
	re := NewRecommendationEngine(0.5)
	recs := re.Suggest("read the file and write the output", nil, nil)
	found := false
	for _, r := range recs {
		if r.Action == "use_filesystem_tools" && r.Category == "tool" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected use_filesystem_tools recommendation")
	}
}

func TestRecommendationEngine_ToolRecommendation_Search(t *testing.T) {
	re := NewRecommendationEngine(0.5)
	recs := re.Suggest("search for information about Go", nil, nil)
	found := false
	for _, r := range recs {
		if r.Action == "use_web_search" && r.Category == "tool" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected use_web_search recommendation")
	}
}

func TestRecommendationEngine_EscalationRecommendation(t *testing.T) {
	re := NewRecommendationEngine(0.5)
	state := &OperatingState{
		Confidence:  0.3,
		CanEscalate: true,
	}
	recs := re.Suggest("help me with this task", state, nil)
	found := false
	for _, r := range recs {
		if r.Action == "escalate_model" && r.Category == "escalation" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected escalate_model recommendation")
	}
}

func TestRecommendationEngine_NoEscalationWhenHighConfidence(t *testing.T) {
	re := NewRecommendationEngine(0.5)
	state := &OperatingState{
		Confidence:  0.9,
		CanEscalate: true,
	}
	recs := re.Suggest("help", state, nil)
	for _, r := range recs {
		if r.Action == "escalate_model" {
			t.Error("should not escalate when confidence is high")
		}
	}
}

func TestRecommendationEngine_MinConfidenceFiltering(t *testing.T) {
	// Set very high min confidence — should filter out all standard recs
	re := NewRecommendationEngine(0.99)
	recs := re.Suggest("remember, search, read file", nil, nil)
	if len(recs) != 0 {
		t.Errorf("expected no recommendations above 0.99 confidence, got %d", len(recs))
	}
}

func TestRecommendationEngine_EmptyInput(t *testing.T) {
	re := NewRecommendationEngine(0.5)
	recs := re.Suggest("", nil, nil)
	if recs == nil {
		// nil is fine for empty input with no matches
	}
	// Should not panic and should return empty or nil
	_ = recs
}
