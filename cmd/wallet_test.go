package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestWalletBalanceCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": "wallet unavailable"})
	}))
	defer cleanup()

	err := walletBalanceCmd.RunE(walletBalanceCmd, nil)
	if err == nil {
		t.Fatal("expected error for wallet failure")
	}
}

func TestWalletAddressCmd_ServerError(t *testing.T) {
	cleanup := setupMockAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": "no wallet configured"})
	}))
	defer cleanup()

	err := walletAddressCmd.RunE(walletAddressCmd, nil)
	if err == nil {
		t.Fatal("expected error for wallet failure")
	}
}
