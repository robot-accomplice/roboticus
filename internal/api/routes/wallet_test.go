package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"goboticus/testutil"
)

func TestGetWalletAddress(t *testing.T) {
	store := testutil.TempStore(t)
	_, _ = store.ExecContext(bgCtx, `INSERT INTO identity (key, value) VALUES ('wallet_address', '0xABC123')`)

	handler := GetWalletAddress(store)
	req := httptest.NewRequest("GET", "/api/wallet/address", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	if body["address"] != "0xABC123" {
		t.Errorf("address = %v", body["address"])
	}
	if body["chain_id"].(float64) != 8453 {
		t.Errorf("chain_id = %v", body["chain_id"])
	}
}

func TestGetWalletAddress_Empty(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GetWalletAddress(store)
	req := httptest.NewRequest("GET", "/api/wallet/address", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	if body["address"] != "" {
		t.Errorf("empty db should return empty address, got %v", body["address"])
	}
}

func TestGetSwaps_Empty(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GetSwaps(store)
	req := httptest.NewRequest("GET", "/api/services/swaps", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	tasks := body["swap_tasks"].([]any)
	if len(tasks) != 0 {
		t.Errorf("empty db should return 0 swaps, got %d", len(tasks))
	}
}

func TestGetTaxPayouts_Empty(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GetTaxPayouts(store)
	req := httptest.NewRequest("GET", "/api/services/tax-payouts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	tasks := body["tax_tasks"].([]any)
	if len(tasks) != 0 {
		t.Errorf("empty db should return 0 tax tasks, got %d", len(tasks))
	}
}

func TestGetRoster(t *testing.T) {
	store := testutil.TempStore(t)
	handler := GetRoster(store)
	req := httptest.NewRequest("GET", "/api/roster", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	agents := body["agents"].([]any)
	if len(agents) < 1 {
		t.Error("should have at least the default agent")
	}
}

func TestGetSwaps_QueryFailureReturnsServerError(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE service_requests`); err != nil {
		t.Fatalf("drop service_requests: %v", err)
	}

	handler := GetSwaps(store)
	req := httptest.NewRequest("GET", "/api/services/swaps", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestGetRoster_QueryFailureReturnsServerError(t *testing.T) {
	store := testutil.TempStore(t)
	if _, err := store.ExecContext(bgCtx, `DROP TABLE sub_agents`); err != nil {
		t.Fatalf("drop sub_agents: %v", err)
	}

	handler := GetRoster(store)
	req := httptest.NewRequest("GET", "/api/roster", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestListPlugins_Nil(t *testing.T) {
	handler := ListPlugins(nil)
	req := httptest.NewRequest("GET", "/api/plugins", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	plugins := body["plugins"].([]any)
	if len(plugins) != 0 {
		t.Errorf("nil registry should return empty, got %d", len(plugins))
	}
}

func TestListMCPConnections_Nil(t *testing.T) {
	handler := ListMCPConnections(nil)
	req := httptest.NewRequest("GET", "/api/mcp/connections", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := jsonBody(t, rec)
	conns := body["connections"].([]any)
	if len(conns) != 0 {
		t.Errorf("nil manager should return empty, got %d", len(conns))
	}
}
