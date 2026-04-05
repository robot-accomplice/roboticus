package llm

import (
	"context"
	"testing"
)

func seedQuality(qt *QualityTracker, model string, score float64, n int) {
	for i := 0; i < n; i++ {
		qt.Record(model, score)
	}
}

func TestRouter_MetascoreOverridesHeuristicRouting(t *testing.T) {
	targets := []RouteTarget{
		{Model: "small-fast", Provider: "small-fast", Tier: TierSmall, Cost: 0.001},
		{Model: "frontier-smart", Provider: "frontier-smart", Tier: TierFrontier, Cost: 0.02},
	}
	router := NewRouter(targets, RouterConfig{CostAware: true})

	qt := NewQualityTracker(32)
	seedQuality(qt, "small-fast", 0.20, 16)
	seedQuality(qt, "frontier-smart", 0.95, 16)
	router.EnableMetascoreRouting(qt, nil, nil)

	req := &Request{Messages: []Message{{Role: "user", Content: "hi"}}}
	selected := router.Select(req)
	if selected.Model != "frontier-smart" {
		t.Fatalf("metascore routing should override heuristic small-tier choice, got %q", selected.Model)
	}
}

func TestRouter_MetascoreSkipsBreakerBlockedWinner(t *testing.T) {
	targets := []RouteTarget{
		{Model: "small-fast", Provider: "small-fast", Tier: TierSmall, Cost: 0.001},
		{Model: "frontier-smart", Provider: "frontier-smart", Tier: TierFrontier, Cost: 0.02},
	}
	router := NewRouter(targets, RouterConfig{CostAware: true})

	qt := NewQualityTracker(32)
	seedQuality(qt, "small-fast", 0.20, 16)
	seedQuality(qt, "frontier-smart", 0.95, 16)
	breakers := NewBreakerRegistry(DefaultCircuitBreakerConfig())
	breakers.Get("frontier-smart").ForceOpen()
	router.EnableMetascoreRouting(qt, nil, breakers)

	req := &Request{Messages: []Message{{Role: "user", Content: "hi"}}}
	selected := router.Select(req)
	if selected.Model != "small-fast" {
		t.Fatalf("metascore routing should skip breaker-blocked model, got %q", selected.Model)
	}
}

func TestRouter_MetascoreFitnessOnRepresentativeTraffic(t *testing.T) {
	targets := []RouteTarget{
		{Model: "local-cheap", Provider: "local-cheap", Tier: TierSmall, IsLocal: true, Cost: 0.0001},
		{Model: "cloud-strong", Provider: "cloud-strong", Tier: TierFrontier, Cost: 0.02},
	}
	router := NewRouter(targets, RouterConfig{CostAware: true, LocalFirst: true})

	qt := NewQualityTracker(64)
	seedQuality(qt, "local-cheap", 0.15, 24)
	seedQuality(qt, "cloud-strong", 0.96, 24)
	router.EnableMetascoreRouting(qt, nil, nil)

	cases := []*Request{
		{Messages: []Message{{Role: "user", Content: "hello"}}},
		{Messages: []Message{{Role: "user", Content: "summarize this note"}}},
		{Messages: []Message{{Role: "user", Content: "analyze architecture trade-offs"}}},
	}

	correct := 0
	for _, req := range cases {
		if got := router.Select(req); got.Model == "cloud-strong" {
			correct++
		}
	}

	accuracy := float64(correct) / float64(len(cases))
	if accuracy < 1.0 {
		t.Fatalf("metascore routing representative accuracy = %.2f, want 1.00", accuracy)
	}
}

func TestService_Complete_UsesMetascoreSelectedProvider(t *testing.T) {
	localClient, _ := NewClientWithHTTP(&Provider{
		Name: "local-model", URL: "http://local", Format: FormatOpenAI, IsLocal: true,
	}, &mockHTTP{
		statusCode: 200,
		body:       `{"id":"local","model":"local-model","choices":[{"message":{"content":"local response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`,
	})
	cloudClient, _ := NewClientWithHTTP(&Provider{
		Name: "cloud-model", URL: "http://cloud", Format: FormatOpenAI,
	}, &mockHTTP{
		statusCode: 200,
		body:       `{"id":"cloud","model":"cloud-model","choices":[{"message":{"content":"cloud response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":30}}`,
	})

	svc, err := NewService(ServiceConfig{
		Primary:   "local-model/local-model",
		Fallbacks: []string{"cloud-model/cloud-model"},
		Providers: []Provider{
			{Name: "local-model", URL: "http://local", Format: FormatOpenAI, IsLocal: true, CostPerOutputTok: 0.0001},
			{Name: "cloud-model", URL: "http://cloud", Format: FormatOpenAI, CostPerOutputTok: 0.00001},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	svc.providers["local-model"] = localClient
	svc.providers["cloud-model"] = cloudClient

	seedQuality(svc.quality, "local-model", 0.10, 16)
	seedQuality(svc.quality, "cloud-model", 0.95, 16)
	svc.router.EnableMetascoreRouting(svc.quality, nil, svc.breakers)

	resp, err := svc.Complete(context.Background(), &Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "cloud response" {
		t.Fatalf("metascore-selected provider response = %q, want cloud response", resp.Content)
	}
}
