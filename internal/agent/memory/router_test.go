package memory

import "testing"

func TestRouter_MemoryQueryIntent(t *testing.T) {
	r := NewRouter(500)
	plan := r.Plan("what do you remember about the project?", []IntentSignal{
		{Label: "memory_query", Confidence: 0.9},
	})

	if len(plan.Targets) == 0 {
		t.Fatal("expected non-empty plan for memory_query intent")
	}
	if plan.Targets[0].Tier != TierSemantic {
		t.Errorf("memory_query should route primarily to semantic, got %s", plan.Targets[0].Tier)
	}
}

func TestRouter_ExecutionIntent(t *testing.T) {
	r := NewRouter(500)
	plan := r.Plan("deploy the new version", []IntentSignal{
		{Label: "execution", Confidence: 0.85},
	})

	if len(plan.Targets) == 0 {
		t.Fatal("expected non-empty plan for execution intent")
	}
	if plan.Targets[0].Tier != TierProcedural {
		t.Errorf("execution should route primarily to procedural, got %s", plan.Targets[0].Tier)
	}
}

func TestRouter_TemporalKeywords(t *testing.T) {
	r := NewRouter(500)
	plan := r.Plan("when did we last deploy to production?", nil)

	if len(plan.Targets) == 0 {
		t.Fatal("expected non-empty plan for temporal query")
	}
	if plan.Targets[0].Tier != TierEpisodic {
		t.Errorf("temporal query should route primarily to episodic, got %s", plan.Targets[0].Tier)
	}
}

func TestRouter_ProceduralKeywords(t *testing.T) {
	r := NewRouter(500)
	plan := r.Plan("how to set up the staging environment?", nil)

	if len(plan.Targets) == 0 {
		t.Fatal("expected non-empty plan for procedural query")
	}
	if plan.Targets[0].Tier != TierProcedural {
		t.Errorf("procedural query should route primarily to procedural, got %s", plan.Targets[0].Tier)
	}
}

func TestRouter_RelationshipKeywords(t *testing.T) {
	r := NewRouter(500)
	plan := r.Plan("who is responsible for the auth service?", nil)

	if len(plan.Targets) == 0 {
		t.Fatal("expected non-empty plan for relationship query")
	}
	if plan.Targets[0].Tier != TierRelationship {
		t.Errorf("relationship query should route primarily to relationship, got %s", plan.Targets[0].Tier)
	}
}

func TestRouter_PolicyKeywords(t *testing.T) {
	r := NewRouter(500)
	plan := r.Plan("what is our refund policy?", nil)

	if len(plan.Targets) == 0 {
		t.Fatal("expected non-empty plan for policy query")
	}
	if plan.Targets[0].Tier != TierSemantic {
		t.Errorf("policy query should route to semantic (canonical), got %s", plan.Targets[0].Tier)
	}
	// Policy queries should use keyword mode for exact match.
	if plan.Targets[0].Mode != RetrievalKeyword {
		t.Errorf("policy query should use keyword mode, got %s", plan.Targets[0].Mode)
	}
}

func TestRouter_DebuggingKeywords(t *testing.T) {
	r := NewRouter(500)
	plan := r.Plan("why did the deployment fail again?", nil)

	if len(plan.Targets) == 0 {
		t.Fatal("expected non-empty plan for debugging query")
	}
	if plan.Targets[0].Tier != TierEpisodic {
		t.Errorf("debugging query should route primarily to episodic, got %s", plan.Targets[0].Tier)
	}
	// Should also include procedural and semantic.
	tiers := make(map[MemoryTier]bool)
	for _, t := range plan.Targets {
		tiers[t.Tier] = true
	}
	if !tiers[TierProcedural] {
		t.Error("debugging plan should include procedural tier")
	}
	if !tiers[TierSemantic] {
		t.Error("debugging plan should include semantic tier")
	}
}

func TestRouter_DefaultPlan(t *testing.T) {
	r := NewRouter(500)
	plan := r.Plan("tell me about the weather", nil)

	if len(plan.Targets) < 2 {
		t.Fatalf("default plan should have at least 2 tiers, got %d", len(plan.Targets))
	}
	// Default includes semantic and episodic.
	tiers := make(map[MemoryTier]bool)
	for _, t := range plan.Targets {
		tiers[t.Tier] = true
	}
	if !tiers[TierSemantic] {
		t.Error("default plan should include semantic")
	}
	if !tiers[TierEpisodic] {
		t.Error("default plan should include episodic")
	}
}

func TestRouter_NeverTargetsWorkingMemory(t *testing.T) {
	r := NewRouter(500)
	queries := []string{
		"what are you working on?",
		"current goal status",
		"what do you remember?",
		"deploy the service",
		"who is the admin?",
	}
	for _, q := range queries {
		plan := r.Plan(q, nil)
		for _, target := range plan.Targets {
			if target.Tier == TierWorking {
				t.Errorf("router should NEVER target working memory, but plan for %q includes it", q)
			}
		}
	}
}

func TestRouter_BudgetsSumToOne(t *testing.T) {
	r := NewRouter(500)
	queries := []string{
		"what is our policy?",
		"how to deploy?",
		"when did we last fail?",
		"debug the crash",
		"general question",
	}
	for _, q := range queries {
		plan := r.Plan(q, nil)
		total := 0.0
		for _, target := range plan.Targets {
			total += target.Budget
		}
		if total < 0.99 || total > 1.01 {
			t.Errorf("plan for %q has budget sum %.2f, want ~1.0", q, total)
		}
	}
}

func TestRouter_IntentOverridesKeywords(t *testing.T) {
	r := NewRouter(500)
	// Query has "error" keyword (debugging) but intent says memory_query.
	// Intent should win because it has high confidence.
	plan := r.Plan("what do you remember about the error?", []IntentSignal{
		{Label: "memory_query", Confidence: 0.9},
	})

	if plan.Targets[0].Tier != TierSemantic {
		t.Errorf("intent should override keywords — expected semantic, got %s", plan.Targets[0].Tier)
	}
}
