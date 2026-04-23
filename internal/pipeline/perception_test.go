package pipeline

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildPerception_ClassifiesFinancialActionAsHighRisk(t *testing.T) {
	synthesis := TaskSynthesis{
		Intent:          "financial_action",
		Complexity:      "complex",
		PlannedAction:   "execute_directly",
		RetrievalNeeded: true,
	}
	art := BuildPerception("Process the refund for order 42 in production.", synthesis)
	if art.Risk != RiskHigh {
		t.Fatalf("expected RiskHigh, got %s", art.Risk)
	}
	if !containsTier(art.RequiredMemoryTiers, "semantic") {
		t.Fatalf("expected semantic in required tiers for high-risk, got %+v", art.RequiredMemoryTiers)
	}
	if !containsTier(art.RequiredMemoryTiers, "relationship") {
		t.Fatalf("expected relationship in required tiers for high-risk, got %+v", art.RequiredMemoryTiers)
	}
}

func TestBuildPerception_ClassifiesPolicyAsSemanticSourceOfTruth(t *testing.T) {
	synthesis := TaskSynthesis{Intent: "question", Complexity: "simple", RetrievalNeeded: true}
	art := BuildPerception("What is the refund policy for unused purchases?", synthesis)
	if art.SourceOfTruth != SourceSemantic {
		t.Fatalf("expected SourceSemantic, got %s", art.SourceOfTruth)
	}
	if !containsTier(art.RequiredMemoryTiers, "semantic") {
		t.Fatalf("expected semantic tier required, got %+v", art.RequiredMemoryTiers)
	}
}

func TestBuildPerception_ClassifiesHowToAsProceduralSourceOfTruth(t *testing.T) {
	synthesis := TaskSynthesis{Intent: "question", Complexity: "moderate", RetrievalNeeded: true}
	art := BuildPerception("How do I deploy the canary to production?", synthesis)
	// "production" triggers high-risk classification which is correct; source
	// should still be procedural because the intent is procedural.
	if art.SourceOfTruth != SourceProcedural {
		t.Fatalf("expected SourceProcedural, got %s", art.SourceOfTruth)
	}
	if !containsTier(art.RequiredMemoryTiers, "procedural") {
		t.Fatalf("expected procedural tier required, got %+v", art.RequiredMemoryTiers)
	}
}

func TestBuildPerception_ClassifiesDependencyAsRelationshipSourceOfTruth(t *testing.T) {
	synthesis := TaskSynthesis{Intent: "question", Complexity: "moderate", RetrievalNeeded: true}
	art := BuildPerception("Who owns the billing service and what depends on it?", synthesis)
	if art.SourceOfTruth != SourceRelationship {
		t.Fatalf("expected SourceRelationship, got %s", art.SourceOfTruth)
	}
}

func TestBuildPerception_ClassifiesCurrentAsExternalSource(t *testing.T) {
	synthesis := TaskSynthesis{Intent: "question", Complexity: "simple", RetrievalNeeded: true}
	art := BuildPerception("What is the current price of bitcoin?", synthesis)
	if art.SourceOfTruth != SourceExternal {
		t.Fatalf("expected SourceExternal, got %s", art.SourceOfTruth)
	}
	if !art.FreshnessRequired {
		t.Fatalf("expected freshness_required=true for current-state query")
	}
}

func TestBuildPerception_SkipsRetrievalForConversationalTurns(t *testing.T) {
	synthesis := TaskSynthesis{Intent: "conversational", Complexity: "simple", RetrievalNeeded: false}
	art := BuildPerception("hello there", synthesis)
	if art.SourceOfTruth != SourceNone {
		t.Fatalf("expected SourceNone for conversational turn, got %s", art.SourceOfTruth)
	}
	if len(art.RequiredMemoryTiers) != 0 {
		t.Fatalf("expected no required tiers, got %+v", art.RequiredMemoryTiers)
	}
}

func TestBuildPerception_DecompositionForcesProceduralTier(t *testing.T) {
	synthesis := TaskSynthesis{Intent: "task", Complexity: "complex", RetrievalNeeded: true}
	art := BuildPerception("I'd like you to migrate the auth service and document the rollout.", synthesis)
	if !art.DecompositionNeeded {
		t.Fatalf("expected decomposition_needed for complex task")
	}
	if !containsTier(art.RequiredMemoryTiers, "procedural") {
		t.Fatalf("expected procedural tier required on decomposition, got %+v", art.RequiredMemoryTiers)
	}
}

func TestBuildPerception_ProceduralUncertaintyPullsProceduralAndEpisodic(t *testing.T) {
	synthesis := TaskSynthesis{
		Intent:                "task",
		Complexity:            "moderate",
		RetrievalNeeded:       true,
		ProceduralUncertainty: true,
		RetrievalReason:       "applied_learning_uncertainty",
	}
	art := BuildPerception("Set up a canary release workflow for the auth service with rollout and rollback gates.", synthesis)
	if art.SourceOfTruth != SourceProcedural {
		t.Fatalf("expected SourceProcedural, got %s", art.SourceOfTruth)
	}
	if !containsTier(art.RequiredMemoryTiers, "procedural") {
		t.Fatalf("expected procedural tier required, got %+v", art.RequiredMemoryTiers)
	}
	if !containsTier(art.RequiredMemoryTiers, "episodic") {
		t.Fatalf("expected episodic tier required for prior outcome evidence, got %+v", art.RequiredMemoryTiers)
	}
}

func TestAnnotatePerceptionTrace_EmitsFullArtifact(t *testing.T) {
	tr := NewTraceRecorder()
	tr.BeginSpan("task_synthesis")
	art := PerceptionArtifact{
		Intent:                "financial_action",
		Risk:                  RiskHigh,
		SourceOfTruth:         SourceSemantic,
		RequiredMemoryTiers:   []string{"semantic", "relationship"},
		DecompositionNeeded:   true,
		ProceduralUncertainty: true,
		FreshnessRequired:     true,
		Confidence:            0.8,
	}
	AnnotatePerceptionTrace(tr, art)
	tr.EndSpan("ok")

	meta := tr.Finish("turn-1", "test").Stages[0].Metadata
	for _, key := range []string{
		"perception.intent", "perception.risk", "perception.source_of_truth",
		"perception.required_tiers", "perception.decomposition_needed",
		"perception.procedural_uncertainty", "perception.freshness_required", "perception.confidence",
	} {
		if _, ok := meta[key]; !ok {
			t.Fatalf("expected %s annotation, got %+v", key, meta)
		}
	}

	// Serialising the metadata to JSON should not fail.
	if _, err := json.Marshal(meta); err != nil {
		t.Fatalf("metadata not JSON-serialisable: %v", err)
	}
	if got, _ := meta["perception.risk"].(string); got != "high" {
		t.Fatalf("expected risk=high annotation, got %q", got)
	}
}

func TestBuildPerception_ReturnsDeterministicOutput(t *testing.T) {
	synthesis := TaskSynthesis{Intent: "question", Complexity: "moderate", RetrievalNeeded: true}
	art1 := BuildPerception("What is the current refund policy?", synthesis)
	art2 := BuildPerception("What is the current refund policy?", synthesis)

	if art1.Risk != art2.Risk || art1.SourceOfTruth != art2.SourceOfTruth {
		t.Fatalf("expected deterministic output, got %+v vs %+v", art1, art2)
	}
	// Required tiers must compare equal.
	if strings.Join(art1.RequiredMemoryTiers, ",") != strings.Join(art2.RequiredMemoryTiers, ",") {
		t.Fatalf("required tiers not deterministic, got %+v vs %+v", art1.RequiredMemoryTiers, art2.RequiredMemoryTiers)
	}
}

func containsTier(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
