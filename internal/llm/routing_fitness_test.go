package llm

import (
	"math"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 1. Metascore weight invariant tests
// ---------------------------------------------------------------------------

func TestMetascore_WeightsSum(t *testing.T) {
	// The weights are 0.35 + 0.25 + 0.20 + 0.10 + 0.10.
	// A profile with all dimensions at 1.0 should produce exactly 1.0.
	p := ModelProfile{
		Quality:      1.0,
		Availability: 1.0,
		Cost:         0.0, // cost is inverted: (1-cost), so 0.0 cost gives max contribution
		Locality:     1.0,
		Confidence:   1.0,
	}
	got := p.Metascore()
	if math.Abs(got-1.0) > 1e-12 {
		t.Fatalf("Metascore with all-max inputs = %f, want 1.0", got)
	}
}

func TestMetascore_QualityOnlyGivesExactWeight(t *testing.T) {
	p := ModelProfile{
		Quality:      1.0,
		Availability: 0.0,
		Cost:         1.0, // (1-1.0)=0
		Locality:     0.0,
		Confidence:   0.0,
	}
	got := p.Metascore()
	if math.Abs(got-0.35) > 1e-12 {
		t.Fatalf("Metascore(quality=1, rest=0) = %f, want 0.35", got)
	}
}

func TestMetascore_MonotonicallyIncreasingInEachDimension(t *testing.T) {
	base := ModelProfile{
		Quality:      0.5,
		Availability: 0.5,
		Cost:         0.5,
		Locality:     0.5,
		Confidence:   0.5,
	}
	baseScore := base.Metascore()

	// Increasing Quality should increase score.
	pQ := base
	pQ.Quality = 0.9
	if pQ.Metascore() <= baseScore {
		t.Error("increasing Quality did not increase Metascore")
	}

	// Increasing Availability should increase score.
	pA := base
	pA.Availability = 0.9
	if pA.Metascore() <= baseScore {
		t.Error("increasing Availability did not increase Metascore")
	}

	// Decreasing Cost should increase score (lower cost = better).
	pC := base
	pC.Cost = 0.1
	if pC.Metascore() <= baseScore {
		t.Error("decreasing Cost did not increase Metascore")
	}

	// Increasing Locality should increase score.
	pL := base
	pL.Locality = 0.9
	if pL.Metascore() <= baseScore {
		t.Error("increasing Locality did not increase Metascore")
	}

	// Increasing Confidence should increase score.
	pConf := base
	pConf.Confidence = 0.9
	if pConf.Metascore() <= baseScore {
		t.Error("increasing Confidence did not increase Metascore")
	}
}

func TestMetascore_RangeIsZeroToOne(t *testing.T) {
	// Test a large grid of input combinations.
	steps := []float64{0.0, 0.25, 0.5, 0.75, 1.0}
	for _, q := range steps {
		for _, a := range steps {
			for _, c := range steps {
				for _, l := range steps {
					for _, conf := range steps {
						p := ModelProfile{
							Quality:      q,
							Availability: a,
							Cost:         c,
							Locality:     l,
							Confidence:   conf,
						}
						s := p.Metascore()
						if s < 0 || s > 1.0+1e-12 {
							t.Fatalf("Metascore out of [0,1]: %f for q=%f a=%f c=%f l=%f conf=%f",
								s, q, a, c, l, conf)
						}
					}
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Complexity estimation regression tests (20+ cases)
// ---------------------------------------------------------------------------

func TestEstimateComplexity_Regression(t *testing.T) {
	tests := []struct {
		name     string
		req      *Request
		wantTier ModelTier
	}{
		// Simple signals -> TierSmall
		{name: "hello", req: &Request{Messages: []Message{{Role: "user", Content: "hello"}}}, wantTier: TierSmall},
		{name: "yes", req: &Request{Messages: []Message{{Role: "user", Content: "yes"}}}, wantTier: TierSmall},
		{name: "no", req: &Request{Messages: []Message{{Role: "user", Content: "no"}}}, wantTier: TierSmall},
		{name: "ok", req: &Request{Messages: []Message{{Role: "user", Content: "ok"}}}, wantTier: TierSmall},
		{name: "thanks", req: &Request{Messages: []Message{{Role: "user", Content: "thanks"}}}, wantTier: TierSmall},
		{name: "hi", req: &Request{Messages: []Message{{Role: "user", Content: "hi"}}}, wantTier: TierSmall},

		// Medium-length typical messages
		{name: "short question", req: &Request{Messages: []Message{
			{Role: "user", Content: "What is the capital of France?"},
		}}, wantTier: TierSmall},

		{name: "summarize a note", req: &Request{Messages: []Message{
			{Role: "user", Content: "Can you summarize the following paragraph for me? " + strings.Repeat("word ", 80)},
		}}, wantTier: TierSmall},

		// Complex content signals
		{name: "analyze architecture", req: &Request{Messages: []Message{
			{Role: "user", Content: "analyze the architecture trade-offs between microservices and monolith for our payment system"},
		}}, wantTier: TierSmall}, // short message, one complexity signal -> 0.05+0.1 = 0.15 -> TierSmall

		// Long message bumps tier
		{name: "long message 3500 chars", req: &Request{Messages: []Message{
			{Role: "user", Content: strings.Repeat("a", 3500)},
		}}, wantTier: TierMedium}, // 0.2 -> TierMedium

		{name: "15000 char message", req: &Request{Messages: []Message{
			{Role: "user", Content: strings.Repeat("a", 15000)},
		}}, wantTier: TierMedium}, // 0.3 -> TierMedium

		// Tools present
		{name: "few tools", req: &Request{
			Messages: []Message{{Role: "user", Content: "run the build"}},
			Tools:    makeToolDefs(3),
		}, wantTier: TierSmall}, // 0.15 -> TierSmall

		{name: "6 tools", req: &Request{
			Messages: []Message{{Role: "user", Content: "run the build"}},
			Tools:    makeToolDefs(6),
		}, wantTier: TierMedium}, // 0.15 + 0.10 = 0.25 -> TierMedium

		{name: "12 tools", req: &Request{
			Messages: []Message{{Role: "user", Content: "run the build"}},
			Tools:    makeToolDefs(12),
		}, wantTier: TierMedium}, // 0.15 + 0.10 = 0.25 -> TierMedium

		// Many conversation turns
		{name: "9 turns", req: &Request{
			Messages: makeMessages(9, "short message"),
		}, wantTier: TierSmall}, // 0.1 -> TierSmall (9 turns > 8)

		{name: "25 turns", req: &Request{
			Messages: makeMessages(25, "short message"),
		}, wantTier: TierMedium}, // 0.2 -> TierMedium (25 > 20)

		{name: "30 turns with tools", req: &Request{
			Messages: makeMessages(30, "short message"),
			Tools:    makeToolDefs(8),
		}, wantTier: TierLarge}, // 0.2 + 0.15 + 0.10 = 0.45 -> TierLarge

		// Long message + many turns + tools + complexity signal -> Frontier
		{name: "everything combined frontier", req: &Request{
			Messages: append(makeMessages(24, strings.Repeat("word ", 200)),
				Message{Role: "user", Content: "now analyze the performance implications of " + strings.Repeat("detail ", 1500)}),
			Tools: makeToolDefs(8),
		}, wantTier: TierFrontier}, // 0.3 + 0.2 + 0.15 + 0.10 + 0.05 = 0.80 -> TierFrontier

		// Mixed signals: simple words but many tools
		{name: "simple words many tools", req: &Request{
			Messages: []Message{{Role: "user", Content: "ok"}},
			Tools:    makeToolDefs(10),
		}, wantTier: TierSmall}, // -0.2 + 0.15 + 0.10 = 0.05 -> TierSmall

		// Long message + analyze -> TierMedium+
		{name: "long analyze request", req: &Request{Messages: []Message{
			{Role: "user", Content: "Please analyze the following code and explain why it fails: " + strings.Repeat("x", 5000)},
		}}, wantTier: TierMedium}, // 0.2 (3000+) + 0.05 (analyze) = 0.25 -> TierMedium

		// Empty request
		{name: "empty messages", req: &Request{Messages: []Message{}}, wantTier: TierSmall},

		// Multiple messages, last one is simple but total chars > 10000
		{name: "complex history simple last", req: &Request{
			Messages: append(makeMessages(5, strings.Repeat("detail ", 300)),
				Message{Role: "user", Content: "yes"}),
		}, wantTier: TierMedium}, // totalChars >10000 -> 0.3; "yes" simple signal doesn't fire because totalChars >100

		// Design keyword
		{name: "design keyword mid-length", req: &Request{Messages: []Message{
			{Role: "user", Content: "design a new API endpoint for user authentication with OAuth2 support " + strings.Repeat("detail ", 100)},
		}}, wantTier: TierSmall}, // 0.1 (>500 chars) + 0.05 (design) = 0.15 -> TierSmall
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := estimateComplexity(tc.req)
			tier := tierForComplexity(c)
			if tier != tc.wantTier {
				t.Errorf("complexity=%f tier=%d, want tier %d", float64(c), tier, tc.wantTier)
			}
		})
	}
}

// Verify the last case separately since the table entry comment was uncertain.
func TestEstimateComplexity_DesignKeywordMidLength(t *testing.T) {
	msg := "design a new API endpoint for user authentication with OAuth2 support " + strings.Repeat("detail ", 100)
	req := &Request{Messages: []Message{{Role: "user", Content: msg}}}
	c := estimateComplexity(req)
	// msg > 500 chars -> 0.1, "design" signal -> 0.05 = 0.15 -> TierSmall
	tier := tierForComplexity(c)
	if tier > TierMedium {
		t.Errorf("mid-length design request should be TierSmall or TierMedium, got %d", tier)
	}
}

// ---------------------------------------------------------------------------
// 3. Router selection correctness
// ---------------------------------------------------------------------------

func TestRouter_CostAwareSelectsCheapest(t *testing.T) {
	targets := []RouteTarget{
		{Model: "expensive", Provider: "p1", Tier: TierSmall, Cost: 0.05},
		{Model: "cheap", Provider: "p2", Tier: TierSmall, Cost: 0.001},
	}
	router := NewRouter(targets, RouterConfig{CostAware: true})
	req := &Request{Messages: []Message{{Role: "user", Content: "hello"}}}
	got := router.Select(req)
	if got.Model != "cheap" {
		t.Fatalf("cost-aware should select cheapest model, got %q", got.Model)
	}
}

func TestRouter_LocalFirstPrefersLocal(t *testing.T) {
	targets := []RouteTarget{
		{Model: "cloud", Provider: "p1", Tier: TierSmall, Cost: 0.001},
		{Model: "local", Provider: "p2", Tier: TierSmall, Cost: 0.01, IsLocal: true},
	}
	router := NewRouter(targets, RouterConfig{LocalFirst: true})
	req := &Request{Messages: []Message{{Role: "user", Content: "hello"}}}
	got := router.Select(req)
	if got.Model != "local" {
		t.Fatalf("local-first should prefer local model, got %q", got.Model)
	}
}

func TestRouter_LocalFirstRequiresAtOrAboveTier(t *testing.T) {
	targets := []RouteTarget{
		{Model: "local-small", Provider: "p1", Tier: TierSmall, IsLocal: true},
		{Model: "cloud-large", Provider: "p2", Tier: TierLarge},
	}
	router := NewRouter(targets, RouterConfig{LocalFirst: true})
	// Build a request that needs TierLarge (complexity >= 0.4).
	req := &Request{
		Messages: makeMessages(25, strings.Repeat("x", 200)),
		Tools:    makeToolDefs(8),
	}
	got := router.Select(req)
	// Local model is TierSmall, but >= targetTier is false for TierLarge.
	// Should not select local-small since TierSmall < TierLarge.
	if got.Model == "local-small" {
		t.Fatal("local-first should not pick a local model below the target tier")
	}
}

func TestRouter_RoundRobinDistributesEvenly(t *testing.T) {
	targets := []RouteTarget{
		{Model: "a", Provider: "p1", Tier: TierSmall},
		{Model: "b", Provider: "p2", Tier: TierSmall},
		{Model: "c", Provider: "p3", Tier: TierSmall},
	}
	router := NewRouter(targets, RouterConfig{RoundRobin: true})
	req := &Request{Messages: []Message{{Role: "user", Content: "x"}}}

	counts := map[string]int{}
	for i := 0; i < 300; i++ {
		got := router.Select(req)
		counts[got.Model]++
	}

	for _, model := range []string{"a", "b", "c"} {
		if counts[model] != 100 {
			t.Errorf("round-robin: model %q got %d selections, want 100", model, counts[model])
		}
	}
}

func TestRouter_OverrideBypassesAllRouting(t *testing.T) {
	targets := []RouteTarget{
		{Model: "primary", Provider: "p1", Tier: TierSmall, Cost: 0.001},
		{Model: "override-target", Provider: "p2", Tier: TierFrontier, Cost: 0.1},
	}
	router := NewRouter(targets, RouterConfig{CostAware: true})
	router.SetOverride("override-target")

	req := &Request{Messages: []Message{{Role: "user", Content: "hello"}}}
	got := router.Select(req)
	if got.Model != "override-target" {
		t.Fatalf("override should bypass routing, got %q", got.Model)
	}

	// Clear override restores normal routing.
	router.ClearOverride()
	got = router.Select(req)
	if got.Model != "primary" {
		t.Fatalf("after clear override, should route normally, got %q", got.Model)
	}
}

func TestRouter_OverrideUnknownModelReturnsFabricatedTarget(t *testing.T) {
	targets := []RouteTarget{
		{Model: "primary", Provider: "p1", Tier: TierSmall},
	}
	router := NewRouter(targets, RouterConfig{})
	router.SetOverride("nonexistent-model")

	req := &Request{Messages: []Message{{Role: "user", Content: "hi"}}}
	got := router.Select(req)
	if got.Model != "nonexistent-model" {
		t.Fatalf("override with unknown model should return it anyway, got %q", got.Model)
	}
}

func TestRouter_MetascoreOverridesHeuristic(t *testing.T) {
	targets := []RouteTarget{
		{Model: "small", Provider: "small", Tier: TierSmall, Cost: 0.001},
		{Model: "frontier", Provider: "frontier", Tier: TierFrontier, Cost: 0.05},
	}
	router := NewRouter(targets, RouterConfig{CostAware: true})

	qt := NewQualityTracker(32)
	seedQuality(qt, "small", 0.1, 16)
	seedQuality(qt, "frontier", 0.95, 16)
	router.EnableMetascoreRouting(qt, nil, nil)

	// "hello" would normally go to TierSmall, but metascore prefers frontier.
	req := &Request{Messages: []Message{{Role: "user", Content: "hello"}}}
	got := router.Select(req)
	if got.Model != "frontier" {
		t.Fatalf("metascore should override heuristic, got %q", got.Model)
	}
}

func TestRouter_BreakerBlockedSkippedInMetascore(t *testing.T) {
	targets := []RouteTarget{
		{Model: "fallback", Provider: "fallback", Tier: TierSmall, Cost: 0.001},
		{Model: "best", Provider: "best", Tier: TierFrontier, Cost: 0.05},
	}
	router := NewRouter(targets, RouterConfig{CostAware: true})

	qt := NewQualityTracker(32)
	seedQuality(qt, "fallback", 0.3, 16)
	seedQuality(qt, "best", 0.99, 16)
	breakers := NewBreakerRegistry(DefaultCircuitBreakerConfig())
	breakers.Get("best").ForceOpen()
	router.EnableMetascoreRouting(qt, nil, breakers)

	req := &Request{Messages: []Message{{Role: "user", Content: "hi"}}}
	got := router.Select(req)
	if got.Model != "fallback" {
		t.Fatalf("breaker-blocked model should be skipped, got %q", got.Model)
	}
}

func TestRouter_EmptyTargetsReturnsFallback(t *testing.T) {
	router := NewRouter(nil, RouterConfig{})
	req := &Request{Model: "requested-model", Messages: []Message{{Role: "user", Content: "hi"}}}
	got := router.Select(req)
	if got.Model != "requested-model" {
		t.Fatalf("empty targets should return request model as fallback, got %q", got.Model)
	}
}

func TestRouter_TierUpwardFallback(t *testing.T) {
	// Only a Large-tier model exists; target is Medium.
	targets := []RouteTarget{
		{Model: "large-only", Provider: "p1", Tier: TierLarge, Cost: 0.01},
	}
	router := NewRouter(targets, RouterConfig{})
	// Build a request that maps to TierMedium (complexity ~0.2-0.39).
	req := &Request{Messages: []Message{{Role: "user", Content: strings.Repeat("a", 3500)}}}
	got := router.Select(req)
	if got.Model != "large-only" {
		t.Fatalf("should fall back upward to Large when Medium is missing, got %q", got.Model)
	}
}

// ---------------------------------------------------------------------------
// 4. BuildModelProfiles correctness
// ---------------------------------------------------------------------------

func TestBuildModelProfiles_NilQualityDefaultsToHalf(t *testing.T) {
	targets := []RouteTarget{{Model: "m1", Provider: "p1"}}
	profiles := BuildModelProfiles(targets, nil, nil, nil)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].Quality != 0.5 {
		t.Errorf("nil quality tracker should default to 0.5, got %f", profiles[0].Quality)
	}
}

func TestBuildModelProfiles_NilBreakersDefaultsToFullAvailability(t *testing.T) {
	targets := []RouteTarget{{Model: "m1", Provider: "p1"}}
	profiles := BuildModelProfiles(targets, nil, nil, nil)
	if profiles[0].Availability != 1.0 {
		t.Errorf("nil breakers should default to 1.0 availability, got %f", profiles[0].Availability)
	}
}

func TestBuildModelProfiles_CircuitBreakerStates(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(cb *CircuitBreaker)
		wantAvail      float64
		wantAvailRange [2]float64 // [min, max] inclusive, used if wantAvail < 0
	}{
		{name: "closed", setup: func(cb *CircuitBreaker) {}, wantAvail: 1.0},
		{name: "forced open", setup: func(cb *CircuitBreaker) { cb.ForceOpen() }, wantAvail: 0.0},
		{name: "half-open via capacity pressure", setup: func(cb *CircuitBreaker) {
			cb.SetCapacityPressure(true)
		}, wantAvail: 0.5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			targets := []RouteTarget{{Model: "m", Provider: "prov"}}
			breakers := NewBreakerRegistry(DefaultCircuitBreakerConfig())
			cb := breakers.Get("prov")
			tc.setup(cb)

			profiles := BuildModelProfiles(targets, nil, nil, breakers)
			if profiles[0].Availability != tc.wantAvail {
				t.Errorf("availability = %f, want %f", profiles[0].Availability, tc.wantAvail)
			}
		})
	}
}

func TestBuildModelProfiles_CostNormalization(t *testing.T) {
	tests := []struct {
		name     string
		cost     float64
		wantCost float64
	}{
		{name: "zero cost", cost: 0.0, wantCost: 0.0},
		{name: "max cost ($0.0001/tok)", cost: 0.0001, wantCost: 1.0},
		{name: "over max capped", cost: 0.001, wantCost: 1.0},
		{name: "half max", cost: 0.00005, wantCost: 0.5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			targets := []RouteTarget{{Model: "m", Provider: "p", Cost: tc.cost}}
			profiles := BuildModelProfiles(targets, nil, nil, nil)
			if math.Abs(profiles[0].Cost-tc.wantCost) > 0.01 {
				t.Errorf("cost = %f, want %f", profiles[0].Cost, tc.wantCost)
			}
		})
	}
}

func TestBuildModelProfiles_Confidence(t *testing.T) {
	tests := []struct {
		name      string
		obs       int
		wantConf  float64
		tolerance float64
	}{
		{name: "0 observations", obs: 0, wantConf: 0.0, tolerance: 0.01},
		{name: "5 observations", obs: 5, wantConf: 0.5, tolerance: 0.01},
		{name: "10 observations", obs: 10, wantConf: 1.0, tolerance: 0.01},
		{name: "20 observations", obs: 20, wantConf: 1.0, tolerance: 0.01},
		{name: "1 observation", obs: 1, wantConf: 0.1, tolerance: 0.01},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			qt := NewQualityTracker(100)
			for i := 0; i < tc.obs; i++ {
				qt.Record("model", 0.8)
			}
			targets := []RouteTarget{{Model: "model", Provider: "p"}}
			profiles := BuildModelProfiles(targets, qt, nil, nil)
			if math.Abs(profiles[0].Confidence-tc.wantConf) > tc.tolerance {
				t.Errorf("confidence = %f, want %f (+/- %f)", profiles[0].Confidence, tc.wantConf, tc.tolerance)
			}
		})
	}
}

func TestBuildModelProfiles_NilQualityConfidenceDefault(t *testing.T) {
	targets := []RouteTarget{{Model: "m", Provider: "p"}}
	profiles := BuildModelProfiles(targets, nil, nil, nil)
	if profiles[0].Confidence != 0.5 {
		t.Errorf("nil quality tracker confidence should be 0.5, got %f", profiles[0].Confidence)
	}
}

func TestBuildModelProfiles_LocalityMapping(t *testing.T) {
	targets := []RouteTarget{
		{Model: "local", Provider: "p1", IsLocal: true},
		{Model: "cloud", Provider: "p2", IsLocal: false},
	}
	profiles := BuildModelProfiles(targets, nil, nil, nil)
	if profiles[0].Locality != 1.0 {
		t.Errorf("local model locality = %f, want 1.0", profiles[0].Locality)
	}
	if profiles[1].Locality != 0.0 {
		t.Errorf("cloud model locality = %f, want 0.0", profiles[1].Locality)
	}
}

// ---------------------------------------------------------------------------
// 5. End-to-end routing fitness suite
// ---------------------------------------------------------------------------

func TestRoutingFitness_EndToEnd(t *testing.T) {
	type scenario struct {
		name      string
		req       *Request
		wantModel string
	}

	// Setup: local model has poor quality, two cloud models with different
	// quality/cost trade-offs. Costs are set so the frontier model has the
	// highest metascore despite being more expensive.
	targets := []RouteTarget{
		{Model: "local-llm", Provider: "local-llm", Tier: TierSmall, IsLocal: true, Cost: 0.00001},
		{Model: "cloud-gpt", Provider: "cloud-gpt", Tier: TierLarge, Cost: 0.00006},
		{Model: "cloud-frontier", Provider: "cloud-frontier", Tier: TierFrontier, Cost: 0.00007},
	}

	qt := NewQualityTracker(64)
	seedQuality(qt, "local-llm", 0.15, 20)
	seedQuality(qt, "cloud-gpt", 0.88, 20)
	seedQuality(qt, "cloud-frontier", 0.98, 20)

	breakers := NewBreakerRegistry(DefaultCircuitBreakerConfig())

	router := NewRouter(targets, RouterConfig{CostAware: true, LocalFirst: true})
	router.EnableMetascoreRouting(qt, nil, breakers)

	// Verify that cloud-frontier actually has the highest metascore with
	// these parameters. Cost normalization is cost / 0.0001:
	// local-llm:      0.35*0.15 + 0.25*1.0 + 0.20*(1-0.1) + 0.10*1.0 + 0.10*1.0 = 0.0525+0.25+0.18+0.10+0.10 = 0.6825
	// cloud-gpt:      0.35*0.88 + 0.25*1.0 + 0.20*(1-0.6) + 0.10*0.0 + 0.10*1.0 = 0.308+0.25+0.08+0+0.10 = 0.738
	// cloud-frontier: 0.35*0.98 + 0.25*1.0 + 0.20*(1-0.7) + 0.10*0.0 + 0.10*1.0 = 0.343+0.25+0.06+0+0.10 = 0.753
	scenarios := []scenario{
		{name: "simple greeting", req: &Request{Messages: []Message{{Role: "user", Content: "hello"}}}, wantModel: "cloud-frontier"},
		{name: "short question", req: &Request{Messages: []Message{{Role: "user", Content: "what time is it?"}}}, wantModel: "cloud-frontier"},
		{name: "medium task", req: &Request{Messages: []Message{{Role: "user", Content: "explain the difference between TCP and UDP"}}}, wantModel: "cloud-frontier"},
		{name: "complex analysis", req: &Request{
			Messages: []Message{{Role: "user", Content: "analyze the architecture of our distributed system " + strings.Repeat("detail ", 500)}},
			Tools:    makeToolDefs(5),
		}, wantModel: "cloud-frontier"},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			got := router.Select(sc.req)
			if got.Model != sc.wantModel {
				t.Errorf("got %q, want %q", got.Model, sc.wantModel)
			}
		})
	}

	// Now block cloud-frontier: cloud-gpt should take over.
	breakers.Get("cloud-frontier").ForceOpen()
	for _, sc := range scenarios {
		t.Run(sc.name+"/frontier-blocked", func(t *testing.T) {
			got := router.Select(sc.req)
			if got.Model == "cloud-frontier" {
				t.Error("blocked model should never be selected")
			}
			if got.Model != "cloud-gpt" {
				t.Errorf("with frontier blocked, expected cloud-gpt, got %q", got.Model)
			}
		})
	}

	// Block both cloud providers: local should be the last resort.
	breakers.Get("cloud-gpt").ForceOpen()
	for _, sc := range scenarios {
		t.Run(sc.name+"/all-cloud-blocked", func(t *testing.T) {
			got := router.Select(sc.req)
			if got.Model != "local-llm" {
				t.Errorf("with all cloud blocked, expected local-llm, got %q", got.Model)
			}
		})
	}
}

func TestRoutingFitness_NeverSelectsZeroAvailability(t *testing.T) {
	targets := []RouteTarget{
		{Model: "good", Provider: "good-prov", Tier: TierSmall, Cost: 0.001},
		{Model: "blocked", Provider: "blocked-prov", Tier: TierSmall, Cost: 0.0001},
	}
	qt := NewQualityTracker(32)
	seedQuality(qt, "good", 0.5, 16)
	seedQuality(qt, "blocked", 0.99, 16)
	breakers := NewBreakerRegistry(DefaultCircuitBreakerConfig())
	breakers.Get("blocked-prov").ForceOpen()

	router := NewRouter(targets, RouterConfig{CostAware: true})
	router.EnableMetascoreRouting(qt, nil, breakers)

	req := &Request{Messages: []Message{{Role: "user", Content: "hello"}}}
	for i := 0; i < 50; i++ {
		got := router.Select(req)
		if got.Model == "blocked" {
			t.Fatal("should never select a model with 0.0 availability")
		}
	}
}

func TestRoutingFitness_LocalPreferredWhenQualityCompetitive(t *testing.T) {
	targets := []RouteTarget{
		{Model: "local", Provider: "local", Tier: TierSmall, IsLocal: true, Cost: 0.00001},
		{Model: "cloud", Provider: "cloud", Tier: TierSmall, Cost: 0.00008},
	}
	qt := NewQualityTracker(64)
	// Both have similar quality; local should win due to locality + cost.
	seedQuality(qt, "local", 0.80, 20)
	seedQuality(qt, "cloud", 0.82, 20)

	router := NewRouter(targets, RouterConfig{CostAware: true})
	router.EnableMetascoreRouting(qt, nil, nil)

	req := &Request{Messages: []Message{{Role: "user", Content: "hello"}}}
	got := router.Select(req)
	if got.Model != "local" {
		t.Fatalf("when quality is competitive, local should win due to locality+cost advantage, got %q", got.Model)
	}
}

func TestRoutingFitness_HighQualityCloudBeatsLowQualityLocal(t *testing.T) {
	targets := []RouteTarget{
		{Model: "local", Provider: "local", Tier: TierSmall, IsLocal: true, Cost: 0.00001},
		{Model: "cloud", Provider: "cloud", Tier: TierLarge, Cost: 0.00008},
	}
	qt := NewQualityTracker(64)
	seedQuality(qt, "local", 0.15, 20)
	seedQuality(qt, "cloud", 0.95, 20)

	router := NewRouter(targets, RouterConfig{CostAware: true, LocalFirst: true})
	router.EnableMetascoreRouting(qt, nil, nil)

	req := &Request{Messages: []Message{{Role: "user", Content: "explain quantum computing"}}}
	got := router.Select(req)
	if got.Model != "cloud" {
		t.Fatalf("high-quality cloud should beat low-quality local, got %q", got.Model)
	}
}

func TestRoutingFitness_RealisticTrafficMix(t *testing.T) {
	targets := []RouteTarget{
		{Model: "local-fast", Provider: "local-fast", Tier: TierSmall, IsLocal: true, Cost: 0.00001},
		{Model: "cloud-balanced", Provider: "cloud-balanced", Tier: TierMedium, Cost: 0.00004},
		{Model: "cloud-frontier", Provider: "cloud-frontier", Tier: TierFrontier, Cost: 0.00009},
	}

	qt := NewQualityTracker(64)
	seedQuality(qt, "local-fast", 0.20, 20)
	seedQuality(qt, "cloud-balanced", 0.75, 20)
	seedQuality(qt, "cloud-frontier", 0.95, 20)
	breakers := NewBreakerRegistry(DefaultCircuitBreakerConfig())

	router := NewRouter(targets, RouterConfig{CostAware: true})
	router.EnableMetascoreRouting(qt, nil, breakers)

	traffic := []*Request{
		{Messages: []Message{{Role: "user", Content: "hi"}}},
		{Messages: []Message{{Role: "user", Content: "yes"}}},
		{Messages: []Message{{Role: "user", Content: "thanks"}}},
		{Messages: []Message{{Role: "user", Content: "explain closures in Go"}}},
		{Messages: []Message{{Role: "user", Content: "what is 2+2?"}}},
		{Messages: []Message{{Role: "user", Content: "summarize this code " + strings.Repeat("x", 600)}}},
		{Messages: makeMessages(10, "context turn")},
		{Messages: []Message{{Role: "user", Content: "debug this crash" + strings.Repeat(" detail", 200)}}, Tools: makeToolDefs(4)},
		{Messages: []Message{{Role: "user", Content: "compare REST vs GraphQL for our use case"}}},
		{Messages: makeMessages(22, "long conversation"), Tools: makeToolDefs(7)},
		{Messages: []Message{{Role: "user", Content: "refactor the payment module"}}},
		{Messages: []Message{{Role: "user", Content: "ok"}}},
		{Messages: []Message{{Role: "user", Content: "no"}}},
		{Messages: []Message{{Role: "user", Content: "what date is it?"}}},
		{Messages: []Message{{Role: "user", Content: "analyze security implications of " + strings.Repeat("w ", 2000)}}, Tools: makeToolDefs(10)},
		{Messages: []Message{{Role: "user", Content: "hello"}}},
		{Messages: []Message{{Role: "user", Content: "write a unit test for the router"}}},
		{Messages: []Message{{Role: "user", Content: "explain why this test fails"}}},
		{Messages: []Message{{Role: "user", Content: "optimize the database query"}}},
		{Messages: append(makeMessages(25, strings.Repeat("ctx ", 50)),
			Message{Role: "user", Content: "now implement the solution with " + strings.Repeat("d ", 1000)}),
			Tools: makeToolDefs(12)},
	}

	// With metascore routing, cloud-frontier should dominate because of its
	// quality advantage. The key invariant: no blocked model is ever selected.
	for i, req := range traffic {
		got := router.Select(req)
		if got.Model == "" {
			t.Fatalf("request %d: got empty model selection", i)
		}
	}

	// Block cloud-frontier and verify fallback.
	breakers.Get("cloud-frontier").ForceOpen()
	for i, req := range traffic {
		got := router.Select(req)
		if got.Model == "cloud-frontier" {
			t.Fatalf("request %d: selected blocked model cloud-frontier", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeToolDefs(n int) []ToolDef {
	tools := make([]ToolDef, n)
	for i := range tools {
		tools[i] = ToolDef{
			Type:     "function",
			Function: ToolFuncDef{Name: "tool_" + strings.Repeat("x", i+1)},
		}
	}
	return tools
}

func makeMessages(n int, content string) []Message {
	msgs := make([]Message, n)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = Message{Role: role, Content: content}
	}
	return msgs
}
