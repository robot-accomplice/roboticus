package db

import (
	"context"
	"testing"
)

func TestRevenueRepository_CRUD(t *testing.T) {
	store := testTempStore(t)
	repo := NewRevenueRepository(store)
	ctx := context.Background()

	opp := RevenueOpportunityRow{
		ID:                  NewID(),
		Source:              "inbound",
		Strategy:            "upsell",
		PayloadJSON:         `{"item":"pro_plan"}`,
		ExpectedRevenueUSDC: 500.0,
		Status:              "pending",
		ConfidenceScore:     0.8,
		EffortScore:         0.3,
		RiskScore:           0.2,
		PriorityScore:       0.9,
	}

	// Create
	if err := repo.CreateOpportunity(ctx, opp); err != nil {
		t.Fatalf("CreateOpportunity: %v", err)
	}

	// Get
	got, err := repo.GetOpportunity(ctx, opp.ID)
	if err != nil {
		t.Fatalf("GetOpportunity: %v", err)
	}
	if got == nil {
		t.Fatal("GetOpportunity returned nil")
	}
	if got.Strategy != "upsell" {
		t.Errorf("Strategy = %q, want %q", got.Strategy, "upsell")
	}
	if got.ExpectedRevenueUSDC != 500.0 {
		t.Errorf("ExpectedRevenueUSDC = %f, want 500.0", got.ExpectedRevenueUSDC)
	}

	// UpdateOpportunityStatus
	if err := repo.UpdateOpportunityStatus(ctx, opp.ID, "approved"); err != nil {
		t.Fatalf("UpdateOpportunityStatus: %v", err)
	}
	updated, err := repo.GetOpportunity(ctx, opp.ID)
	if err != nil {
		t.Fatalf("GetOpportunity after update: %v", err)
	}
	if updated.Status != "approved" {
		t.Errorf("Status = %q, want %q", updated.Status, "approved")
	}
}

func TestRevenueRepository_GetNotFound(t *testing.T) {
	store := testTempStore(t)
	repo := NewRevenueRepository(store)
	ctx := context.Background()

	got, err := repo.GetOpportunity(ctx, "nonexistent-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent opportunity")
	}
}

func TestRevenueRepository_ListWithFilter(t *testing.T) {
	store := testTempStore(t)
	repo := NewRevenueRepository(store)
	ctx := context.Background()

	rows := []RevenueOpportunityRow{
		{ID: NewID(), Source: "s", Strategy: "upsell", PayloadJSON: "{}", ExpectedRevenueUSDC: 100, Status: "pending"},
		{ID: NewID(), Source: "s", Strategy: "upsell", PayloadJSON: "{}", ExpectedRevenueUSDC: 200, Status: "approved"},
		{ID: NewID(), Source: "s", Strategy: "cross_sell", PayloadJSON: "{}", ExpectedRevenueUSDC: 300, Status: "pending"},
	}
	for _, r := range rows {
		if err := repo.CreateOpportunity(ctx, r); err != nil {
			t.Fatalf("CreateOpportunity: %v", err)
		}
	}

	// List all
	all, err := repo.ListOpportunities(ctx, RevenueOpportunityFilter{})
	if err != nil {
		t.Fatalf("ListOpportunities all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("got %d opportunities, want 3", len(all))
	}

	// Filter by status
	pending, err := repo.ListOpportunities(ctx, RevenueOpportunityFilter{Status: "pending"})
	if err != nil {
		t.Fatalf("ListOpportunities by status: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("got %d pending, want 2", len(pending))
	}

	// Filter by strategy
	upsell, err := repo.ListOpportunities(ctx, RevenueOpportunityFilter{Strategy: "upsell"})
	if err != nil {
		t.Fatalf("ListOpportunities by strategy: %v", err)
	}
	if len(upsell) != 2 {
		t.Errorf("got %d upsell, want 2", len(upsell))
	}

	// Filter by both
	both, err := repo.ListOpportunities(ctx, RevenueOpportunityFilter{Status: "pending", Strategy: "upsell"})
	if err != nil {
		t.Fatalf("ListOpportunities by both: %v", err)
	}
	if len(both) != 1 {
		t.Errorf("got %d with both filters, want 1", len(both))
	}

	// Limit
	limited, err := repo.ListOpportunities(ctx, RevenueOpportunityFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListOpportunities with limit: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("got %d with limit 2, want 2", len(limited))
	}
}

func TestRevenueRepository_Feedback(t *testing.T) {
	store := testTempStore(t)
	repo := NewRevenueRepository(store)
	ctx := context.Background()

	opp := RevenueOpportunityRow{
		ID:                  NewID(),
		Source:              "s",
		Strategy:            "upsell",
		PayloadJSON:         "{}",
		ExpectedRevenueUSDC: 100,
		Status:              "pending",
	}
	if err := repo.CreateOpportunity(ctx, opp); err != nil {
		t.Fatalf("CreateOpportunity: %v", err)
	}

	fb1 := RevenueFeedbackRow{
		ID:            NewID(),
		OpportunityID: opp.ID,
		Strategy:      "upsell",
		Grade:         4.5,
		Source:        "operator",
		Comment:       "Good lead",
	}
	fb2 := RevenueFeedbackRow{
		ID:            NewID(),
		OpportunityID: opp.ID,
		Strategy:      "upsell",
		Grade:         3.0,
		Source:        "system",
	}

	if err := repo.CreateFeedback(ctx, fb1); err != nil {
		t.Fatalf("CreateFeedback fb1: %v", err)
	}
	if err := repo.CreateFeedback(ctx, fb2); err != nil {
		t.Fatalf("CreateFeedback fb2: %v", err)
	}

	feedback, err := repo.ListFeedbackByOpportunity(ctx, opp.ID)
	if err != nil {
		t.Fatalf("ListFeedbackByOpportunity: %v", err)
	}
	if len(feedback) != 2 {
		t.Errorf("got %d feedback entries, want 2", len(feedback))
	}

	// Verify comment round-trip
	found := false
	for _, f := range feedback {
		if f.ID == fb1.ID {
			found = true
			if f.Comment != "Good lead" {
				t.Errorf("Comment = %q, want %q", f.Comment, "Good lead")
			}
			if f.Grade != 4.5 {
				t.Errorf("Grade = %f, want 4.5", f.Grade)
			}
		}
	}
	if !found {
		t.Error("fb1 not found in feedback list")
	}

	// List for different opp returns empty
	empty, err := repo.ListFeedbackByOpportunity(ctx, "other-opp")
	if err != nil {
		t.Fatalf("ListFeedbackByOpportunity other: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("got %d feedback for unrelated opp, want 0", len(empty))
	}
}

func TestRevenueRepository_AggregateByStrategy(t *testing.T) {
	store := testTempStore(t)
	repo := NewRevenueRepository(store)
	ctx := context.Background()

	rows := []RevenueOpportunityRow{
		{ID: NewID(), Source: "s", Strategy: "upsell", PayloadJSON: "{}", ExpectedRevenueUSDC: 100, Status: "pending"},
		{ID: NewID(), Source: "s", Strategy: "upsell", PayloadJSON: "{}", ExpectedRevenueUSDC: 200, Status: "pending"},
		{ID: NewID(), Source: "s", Strategy: "cross_sell", PayloadJSON: "{}", ExpectedRevenueUSDC: 500, Status: "pending"},
	}
	for _, r := range rows {
		if err := repo.CreateOpportunity(ctx, r); err != nil {
			t.Fatalf("CreateOpportunity: %v", err)
		}
	}

	aggs, err := repo.AggregateByStrategy(ctx)
	if err != nil {
		t.Fatalf("AggregateByStrategy: %v", err)
	}
	if len(aggs) != 2 {
		t.Fatalf("got %d aggregates, want 2", len(aggs))
	}

	// cross_sell should be first (highest total)
	if aggs[0].Strategy != "cross_sell" {
		t.Errorf("first strategy = %q, want cross_sell", aggs[0].Strategy)
	}
	if aggs[0].TotalExpectedRevenue != 500.0 {
		t.Errorf("cross_sell total = %f, want 500.0", aggs[0].TotalExpectedRevenue)
	}

	// upsell: count=2, total=300
	if aggs[1].Strategy != "upsell" {
		t.Errorf("second strategy = %q, want upsell", aggs[1].Strategy)
	}
	if aggs[1].Count != 2 {
		t.Errorf("upsell count = %d, want 2", aggs[1].Count)
	}
	if aggs[1].TotalExpectedRevenue != 300.0 {
		t.Errorf("upsell total = %f, want 300.0", aggs[1].TotalExpectedRevenue)
	}
}

func TestRevenueRepository_CountByStatus(t *testing.T) {
	store := testTempStore(t)
	repo := NewRevenueRepository(store)
	ctx := context.Background()

	rows := []RevenueOpportunityRow{
		{ID: NewID(), Source: "s", Strategy: "upsell", PayloadJSON: "{}", ExpectedRevenueUSDC: 100, Status: "pending"},
		{ID: NewID(), Source: "s", Strategy: "upsell", PayloadJSON: "{}", ExpectedRevenueUSDC: 100, Status: "pending"},
		{ID: NewID(), Source: "s", Strategy: "upsell", PayloadJSON: "{}", ExpectedRevenueUSDC: 100, Status: "approved"},
	}
	for _, r := range rows {
		if err := repo.CreateOpportunity(ctx, r); err != nil {
			t.Fatalf("CreateOpportunity: %v", err)
		}
	}

	counts, err := repo.CountByStatus(ctx)
	if err != nil {
		t.Fatalf("CountByStatus: %v", err)
	}
	if len(counts) != 2 {
		t.Fatalf("got %d statuses, want 2", len(counts))
	}

	// pending should be first (highest count)
	if counts[0].Status != "pending" {
		t.Errorf("first status = %q, want pending", counts[0].Status)
	}
	if counts[0].Count != 2 {
		t.Errorf("pending count = %d, want 2", counts[0].Count)
	}
	if counts[1].Status != "approved" {
		t.Errorf("second status = %q, want approved", counts[1].Status)
	}
	if counts[1].Count != 1 {
		t.Errorf("approved count = %d, want 1", counts[1].Count)
	}
}
