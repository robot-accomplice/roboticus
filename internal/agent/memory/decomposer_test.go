package memory

import "testing"

func TestDecompose_SimpleQuery(t *testing.T) {
	subgoals := Decompose("What is our refund policy?")

	if len(subgoals) != 1 {
		t.Fatalf("simple query should produce 1 subgoal, got %d", len(subgoals))
	}
	if subgoals[0].Question != "What is our refund policy?" {
		t.Errorf("simple query should be unchanged, got %q", subgoals[0].Question)
	}
}

func TestDecompose_EmptyQuery(t *testing.T) {
	if subgoals := Decompose(""); subgoals != nil {
		t.Errorf("empty query should return nil, got %v", subgoals)
	}
}

func TestDecompose_MultipleQuestions(t *testing.T) {
	subgoals := Decompose("What is the refund policy? When did we last update it?")

	if len(subgoals) != 2 {
		t.Fatalf("two questions should produce 2 subgoals, got %d", len(subgoals))
	}
	// First question is about policy (semantic), second is temporal (episodic).
	if subgoals[0].TargetTier != TierSemantic {
		t.Errorf("policy question should target semantic, got %s", subgoals[0].TargetTier)
	}
	if subgoals[1].TargetTier != TierEpisodic {
		t.Errorf("temporal question should target episodic, got %s", subgoals[1].TargetTier)
	}
}

func TestDecompose_SemicolonSplit(t *testing.T) {
	subgoals := Decompose("find the deploy procedure; check who last ran it")

	if len(subgoals) != 2 {
		t.Fatalf("semicolon compound should produce 2 subgoals, got %d", len(subgoals))
	}
	if subgoals[0].TargetTier != TierProcedural {
		t.Errorf("procedure question should target procedural, got %s", subgoals[0].TargetTier)
	}
	if subgoals[1].TargetTier != TierRelationship {
		t.Errorf("'who' question should target relationship, got %s", subgoals[1].TargetTier)
	}
}

func TestDecompose_ConjunctionSplit(t *testing.T) {
	subgoals := Decompose("What did we decide about the auth refactor and how does it affect the deployment process?")

	if len(subgoals) != 2 {
		t.Fatalf("conjunction compound should produce 2 subgoals, got %d", len(subgoals))
	}
	// First part is about decisions (episodic), second about process (procedural).
	if subgoals[0].TargetTier != TierEpisodic {
		t.Errorf("decision question should target episodic, got %s", subgoals[0].TargetTier)
	}
}

func TestDecompose_ShortQueryNotSplit(t *testing.T) {
	// Short queries with "and" should NOT be split.
	subgoals := Decompose("find docs and policies")

	if len(subgoals) != 1 {
		t.Errorf("short query should not be split, got %d subgoals", len(subgoals))
	}
}

func TestDecompose_TierClassification(t *testing.T) {
	tests := []struct {
		question string
		want     MemoryTier
	}{
		{"when did the server crash?", TierEpisodic},
		{"what happened last time?", TierEpisodic},
		{"how to deploy to production?", TierProcedural},
		{"what are the steps for rollback?", TierProcedural},
		{"who is responsible for auth?", TierRelationship},
		{"who are the stakeholders?", TierRelationship},
		{"what is the refund policy?", TierSemantic},
		{"define the SLA terms", TierSemantic},
	}
	for _, tt := range tests {
		tier := classifySubgoalTier(tt.question)
		if tier != tt.want {
			t.Errorf("classifySubgoalTier(%q) = %s, want %s", tt.question, tier, tt.want)
		}
	}
}
