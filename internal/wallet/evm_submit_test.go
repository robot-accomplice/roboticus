package wallet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewEVMSubmitter(t *testing.T) {
	s := NewEVMSubmitter("http://localhost:8545", 8453)
	if s.rpcURL != "http://localhost:8545" {
		t.Errorf("rpcURL = %q, want http://localhost:8545", s.rpcURL)
	}
	if s.ChainID() != 8453 {
		t.Errorf("chainID = %d, want 8453", s.ChainID())
	}
}

func TestEVMSubmitter_SubmitTx_EmptyRejectsError(t *testing.T) {
	s := NewEVMSubmitter("http://localhost:8545", 1)
	_, err := s.SubmitTx(context.Background(), nil)
	if err == nil {
		t.Error("expected error for empty transaction")
	}
}

func TestEVMSubmitter_SubmitTx_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  "0xabcdef1234567890",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewEVMSubmitter(srv.URL, 1)
	hash, err := s.SubmitTx(context.Background(), []byte{0x02, 0xf8})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash != "0xabcdef1234567890" {
		t.Errorf("hash = %q, want 0xabcdef1234567890", hash)
	}
}

func TestEVMSubmitter_SubmitTx_RPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error":   map[string]any{"code": -32000, "message": "nonce too low"},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewEVMSubmitter(srv.URL, 1)
	_, err := s.SubmitTx(context.Background(), []byte{0x02})
	if err == nil {
		t.Error("expected RPC error")
	}
}

func TestEVMSubmitter_WaitForReceipt_EmptyHash(t *testing.T) {
	s := NewEVMSubmitter("http://localhost:8545", 1)
	_, err := s.WaitForReceipt(context.Background(), "", 5*time.Second)
	if err == nil {
		t.Error("expected error for empty tx hash")
	}
}

func TestEVMSubmitter_WaitForReceipt_Success(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Return nil first time, receipt second time.
		if callCount == 1 {
			resp := map[string]any{"jsonrpc": "2.0", "id": 1, "result": nil}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"blockNumber": "0xa",
				"status":      "0x1",
				"gasUsed":     "0x5208",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewEVMSubmitter(srv.URL, 1)
	receipt, err := s.WaitForReceipt(context.Background(), "0xabc", 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt.BlockNumber != 10 {
		t.Errorf("blockNumber = %d, want 10", receipt.BlockNumber)
	}
	if receipt.Status != 1 {
		t.Errorf("status = %d, want 1", receipt.Status)
	}
	if receipt.GasUsed != 21000 {
		t.Errorf("gasUsed = %d, want 21000", receipt.GasUsed)
	}
}

func TestEVMSubmitter_WaitForReceipt_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"jsonrpc": "2.0", "id": 1, "result": nil}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := NewEVMSubmitter(srv.URL, 1)
	_, err := s.WaitForReceipt(context.Background(), "0xabc", 3*time.Second)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestTxReceipt_Fields(t *testing.T) {
	r := TxReceipt{
		TxHash:      "0x123",
		BlockNumber: 42,
		Status:      1,
		GasUsed:     21000,
	}
	if r.TxHash != "0x123" {
		t.Errorf("TxHash = %q", r.TxHash)
	}
	if r.BlockNumber != 42 {
		t.Errorf("BlockNumber = %d", r.BlockNumber)
	}
}
