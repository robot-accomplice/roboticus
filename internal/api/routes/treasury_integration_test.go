package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"roboticus/internal/db"
	"roboticus/internal/wallet"
	"roboticus/testutil"
)

// TestTreasuryPolicy_PerPaymentCap verifies that the TreasuryConfig
// per-payment cap is enforced by TreasuryPolicy.
func TestTreasuryPolicy_PerPaymentCap(t *testing.T) {
	cfg := wallet.TreasuryConfig{
		PerPaymentCap: 25.0,
	}
	policy := wallet.NewTreasuryPolicy(cfg)

	// Under cap: allowed.
	if err := policy.CheckPerPayment(24.99); err != nil {
		t.Errorf("payment under cap should succeed: %v", err)
	}

	// At cap: allowed.
	if err := policy.CheckPerPayment(25.0); err != nil {
		t.Errorf("payment at cap should succeed: %v", err)
	}

	// Over cap: rejected.
	if err := policy.CheckPerPayment(25.01); err == nil {
		t.Error("payment over cap should be rejected")
	}

	// Verify Config() round-trips.
	if got := policy.Config().PerPaymentCap; got != 25.0 {
		t.Errorf("Config().PerPaymentCap = %f, want 25.0", got)
	}
}

// TestWalletBalance_EmptyDB verifies that GET /api/wallet/balance returns
// zero balances when the wallet_balances table is empty.
func TestWalletBalance_EmptyDB(t *testing.T) {
	store := testutil.TempStore(t)

	handler := GetWalletBalance(store)
	req := httptest.NewRequest(http.MethodGet, "/api/wallet/balance", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Balance should be zero.
	bal, ok := body["balance"].(float64)
	if !ok {
		t.Fatal("balance field missing or not a number")
	}
	if bal != 0.0 {
		t.Errorf("balance = %f, want 0.0", bal)
	}

	// Currency should be USDC.
	if cur, _ := body["currency"].(string); cur != "USDC" {
		t.Errorf("currency = %q, want USDC", cur)
	}

	// Tokens list should be empty.
	tokens, ok := body["tokens"].([]any)
	if !ok {
		t.Fatal("tokens field missing or not an array")
	}
	if len(tokens) != 0 {
		t.Errorf("tokens length = %d, want 0", len(tokens))
	}

	// Chain ID should be Base.
	chainID, _ := body["chain_id"].(float64)
	if int(chainID) != 8453 {
		t.Errorf("chain_id = %v, want 8453", body["chain_id"])
	}
}

// TestWalletBalance_WithCachedData inserts wallet_balances rows and verifies
// the balance endpoint returns the correct aggregated response.
func TestWalletBalance_WithCachedData(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// Insert test balances.
	insertBalance := func(symbol, name string, balance float64, contract string, decimals int, isNative int) {
		t.Helper()
		_, err := store.ExecContext(ctx,
			`INSERT INTO wallet_balances (symbol, name, balance, contract, decimals, is_native)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			symbol, name, balance, contract, decimals, isNative)
		if err != nil {
			t.Fatalf("insert %s balance: %v", symbol, err)
		}
	}

	insertBalance("ETH", "Ether", 0.05, "", 18, 1)
	insertBalance("USDC", "USD Coin", 142.50, "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", 6, 0)

	// Query the endpoint.
	handler := GetWalletBalance(store)
	req := httptest.NewRequest(http.MethodGet, "/api/wallet/balance", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Top-level balance should reflect USDC.
	bal, _ := body["balance"].(float64)
	if bal != 142.50 {
		t.Errorf("balance = %f, want 142.50", bal)
	}

	// Should have 2 tokens.
	tokens, ok := body["tokens"].([]any)
	if !ok {
		t.Fatal("tokens field missing or not an array")
	}
	if len(tokens) != 2 {
		t.Fatalf("tokens length = %d, want 2", len(tokens))
	}

	// Verify token details. Rows are ordered by symbol (ETH, USDC).
	eth, ok := tokens[0].(map[string]any)
	if !ok {
		t.Fatal("first token is not an object")
	}
	if eth["symbol"] != "ETH" {
		t.Errorf("first token symbol = %v, want ETH", eth["symbol"])
	}
	if eth["is_native"] != true {
		t.Errorf("ETH is_native = %v, want true", eth["is_native"])
	}

	usdc, ok := tokens[1].(map[string]any)
	if !ok {
		t.Fatal("second token is not an object")
	}
	if usdc["symbol"] != "USDC" {
		t.Errorf("second token symbol = %v, want USDC", usdc["symbol"])
	}
	usdcBal, _ := usdc["balance"].(float64)
	if usdcBal != 142.50 {
		t.Errorf("USDC token balance = %f, want 142.50", usdcBal)
	}

	// Chain ID should still be Base.
	chainID, _ := body["chain_id"].(float64)
	if int(chainID) != 8453 {
		t.Errorf("chain_id = %v, want 8453", body["chain_id"])
	}
}

// Ensure db.NewRouteQueries is used correctly (compile-time check).
var _ = db.NewRouteQueries
