package agent

import (
	"encoding/json"
	"testing"
)

func testServiceDef() ServiceDefinition {
	return ServiceDefinition{
		ID:                    "code-review",
		Name:                  "Code Review",
		Description:           "Automated code review",
		PriceUSDC:             5.0,
		CapabilitiesRequired:  []string{"coding"},
		MaxConcurrent:         3,
		EstimatedDurationSecs: 300,
	}
}

func TestServiceManager_RegisterAndList(t *testing.T) {
	mgr := NewServiceManager()
	if err := mgr.RegisterService(testServiceDef()); err != nil {
		t.Fatal(err)
	}
	if mgr.CatalogSize() != 1 {
		t.Errorf("CatalogSize = %d, want 1", mgr.CatalogSize())
	}
	if _, ok := mgr.GetService("code-review"); !ok {
		t.Error("expected to find code-review service")
	}
}

func TestServiceManager_RejectEmptyID(t *testing.T) {
	mgr := NewServiceManager()
	svc := testServiceDef()
	svc.ID = ""
	if err := mgr.RegisterService(svc); err == nil {
		t.Error("expected error for empty ID")
	}
}

func TestServiceManager_RejectNegativePrice(t *testing.T) {
	mgr := NewServiceManager()
	svc := testServiceDef()
	svc.PriceUSDC = -1.0
	if err := mgr.RegisterService(svc); err == nil {
		t.Error("expected error for negative price")
	}
}

func TestServiceManager_FullLifecycle(t *testing.T) {
	mgr := NewServiceManager()
	mgr.RegisterService(testServiceDef())

	// Create quote.
	quote, err := mgr.CreateQuote("code-review", "client-1", json.RawMessage("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if quote.Status != ServiceStatusQuoted {
		t.Errorf("status = %s, want quoted", quote.Status)
	}

	// Record payment.
	if err := mgr.RecordPayment(quote.ID, "0xabc123"); err != nil {
		t.Fatal(err)
	}
	req, _ := mgr.GetRequest(quote.ID)
	if req.Status != ServiceStatusPaymentVerified {
		t.Errorf("status = %s, want payment_verified", req.Status)
	}

	// Start fulfillment.
	if err := mgr.StartFulfillment(quote.ID); err != nil {
		t.Fatal(err)
	}
	req, _ = mgr.GetRequest(quote.ID)
	if req.Status != ServiceStatusInProgress {
		t.Errorf("status = %s, want in_progress", req.Status)
	}

	// Complete.
	if err := mgr.CompleteFulfillment(quote.ID); err != nil {
		t.Fatal(err)
	}
	req, _ = mgr.GetRequest(quote.ID)
	if req.Status != ServiceStatusCompleted {
		t.Errorf("status = %s, want completed", req.Status)
	}
	if req.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}

	// Revenue.
	if revenue := mgr.TotalRevenue(); revenue != 5.0 {
		t.Errorf("TotalRevenue = %f, want 5.0", revenue)
	}
}

func TestServiceManager_InvalidTransitions(t *testing.T) {
	mgr := NewServiceManager()
	mgr.RegisterService(testServiceDef())

	quote, _ := mgr.CreateQuote("code-review", "client-1", json.RawMessage("{}"))

	// Can't start fulfillment before payment.
	if err := mgr.StartFulfillment(quote.ID); err == nil {
		t.Error("expected error: payment not verified")
	}

	// Can't complete before starting.
	if err := mgr.CompleteFulfillment(quote.ID); err == nil {
		t.Error("expected error: not in progress")
	}

	// Can't pay twice.
	mgr.RecordPayment(quote.ID, "0xabc")
	if err := mgr.RecordPayment(quote.ID, "0xdef"); err == nil {
		t.Error("expected error: not in quoted state")
	}
}

func TestServiceManager_FailFulfillment(t *testing.T) {
	mgr := NewServiceManager()
	mgr.RegisterService(testServiceDef())
	quote, _ := mgr.CreateQuote("code-review", "client-1", json.RawMessage("{}"))
	mgr.RecordPayment(quote.ID, "0xabc")
	mgr.StartFulfillment(quote.ID)

	if err := mgr.FailFulfillment(quote.ID); err != nil {
		t.Fatal(err)
	}
	req, _ := mgr.GetRequest(quote.ID)
	if req.Status != ServiceStatusFailed {
		t.Errorf("status = %s, want failed", req.Status)
	}
}

func TestServiceManager_RequestsByStatus(t *testing.T) {
	mgr := NewServiceManager()
	mgr.RegisterService(testServiceDef())
	mgr.CreateQuote("code-review", "client-1", json.RawMessage("{}"))
	mgr.CreateQuote("code-review", "client-2", json.RawMessage("{}"))

	quoted := mgr.RequestsByStatus(ServiceStatusQuoted)
	if len(quoted) != 2 {
		t.Errorf("expected 2 quoted requests, got %d", len(quoted))
	}
}

func TestServiceManager_NonexistentService(t *testing.T) {
	mgr := NewServiceManager()
	if _, err := mgr.CreateQuote("nonexistent", "client", json.RawMessage("{}")); err == nil {
		t.Error("expected error for nonexistent service")
	}
}
