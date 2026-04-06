package wallet

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// wallet.go — loadOrGenerate, generateAndSave, encrypt/decrypt, RPC methods
// ---------------------------------------------------------------------------

func TestNewWallet_DefaultChainID(t *testing.T) {
	w, err := NewWallet(WalletConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if w.ChainID() != DefaultChainID {
		t.Errorf("ChainID = %d, want %d", w.ChainID(), DefaultChainID)
	}
}

func TestNewWallet_CustomChainID(t *testing.T) {
	w, err := NewWallet(WalletConfig{ChainID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if w.ChainID() != 1 {
		t.Errorf("ChainID = %d, want 1", w.ChainID())
	}
}

func TestNewWallet_PassphraseFromEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wallet.enc")
	t.Setenv("ROBOTICUS_WALLET_PASSPHRASE", "test-secret-123")

	w, err := NewWallet(WalletConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if w.Address() == "" {
		t.Fatal("expected non-empty address")
	}

	// File should have been written.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("wallet file was not created")
	}
}

func TestWallet_GenerateAndSave_ThenLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wallet.enc")
	passphrase := "my-secure-pass"

	// Generate and save.
	w1, err := NewWallet(WalletConfig{
		Path:       path,
		ChainID:    1,
		Passphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	addr1 := w1.Address()

	// Reload from file.
	w2, err := NewWallet(WalletConfig{
		Path:       path,
		ChainID:    1,
		Passphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	if w2.Address() != addr1 {
		t.Errorf("reloaded address %q != original %q", w2.Address(), addr1)
	}
}

func TestWallet_LoadOrGenerate_WrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wallet.enc")

	// Generate with passphrase.
	_, err := NewWallet(WalletConfig{
		Path:       path,
		Passphrase: "correct-pass",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Try loading with wrong passphrase.
	_, err = NewWallet(WalletConfig{
		Path:       path,
		Passphrase: "wrong-pass",
	})
	if err == nil {
		t.Fatal("expected error for wrong passphrase")
	}
	if !strings.Contains(err.Error(), "cannot decrypt") {
		t.Errorf("error = %q, want 'cannot decrypt'", err)
	}
}

func TestWallet_LoadOrGenerate_NoPassphrase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wallet.enc")

	// Create an encrypted wallet first.
	_, err := NewWallet(WalletConfig{
		Path:       path,
		Passphrase: "some-pass",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Try loading without passphrase.
	_, err = NewWallet(WalletConfig{Path: path})
	if err == nil {
		t.Fatal("expected error without passphrase")
	}
	if !strings.Contains(err.Error(), "passphrase required") {
		t.Errorf("error = %q, want 'passphrase required'", err)
	}
}

func TestWallet_GenerateAndSave_NoPassphrase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wallet.enc")

	_, err := NewWallet(WalletConfig{Path: path})
	if err == nil {
		t.Fatal("expected error when saving without passphrase")
	}
	if !strings.Contains(err.Error(), "passphrase required") {
		t.Errorf("error = %q, want 'passphrase required'", err)
	}
}

func TestWallet_LoadOrGenerate_UnreadableFile(t *testing.T) {
	dir := t.TempDir()
	// Create a directory where we expect a file -- ReadFile will fail.
	path := filepath.Join(dir, "subdir")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}

	_, err := NewWallet(WalletConfig{Path: path, Passphrase: "x"})
	if err == nil {
		t.Fatal("expected error for unreadable file")
	}
	if !strings.Contains(err.Error(), "wallet load") {
		t.Errorf("error = %q, want 'wallet load'", err)
	}
}

func TestWallet_Decrypt_TooShort(t *testing.T) {
	w := &Wallet{cfg: WalletConfig{Passphrase: "x"}}
	err := w.decrypt(make([]byte, 10)) // shorter than 28 minimum
	if err == nil {
		t.Fatal("expected error for short data")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Errorf("error = %q, want 'too short'", err)
	}
}

func TestWallet_EncryptDecrypt_Roundtrip(t *testing.T) {
	w := &Wallet{cfg: WalletConfig{Passphrase: "roundtrip-test"}}
	plaintext := []byte("secret-key-bytes-32-chars-long!!")

	encrypted, err := w.encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}

	// Create fresh wallet to decrypt.
	w2 := &Wallet{cfg: WalletConfig{Passphrase: "roundtrip-test"}}
	err = w2.decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}

	// The decrypted wallet should have loaded a key from the bytes.
	if w2.privateKey == nil {
		t.Fatal("private key not loaded after decrypt")
	}
}

// ---------------------------------------------------------------------------
// RPC mock helpers
// ---------------------------------------------------------------------------

func mockRPCServer(t *testing.T, handler func(method string, params []any) (any, error)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		result, err := handler(req.Method, req.Params)
		if err != nil {
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"error":   map[string]any{"code": -32000, "message": err.Error()},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  result,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestWallet_GetBalance_Success(t *testing.T) {
	srv := mockRPCServer(t, func(method string, _ []any) (any, error) {
		if method == "eth_getBalance" {
			return "0xde0b6b3a7640000", nil // 1 ETH in wei
		}
		return nil, fmt.Errorf("unexpected method %s", method)
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	bal, err := w.GetBalance()
	if err != nil {
		t.Fatal(err)
	}
	expected := new(big.Int)
	expected.SetString("de0b6b3a7640000", 16)
	if bal.Cmp(expected) != 0 {
		t.Errorf("balance = %s, want %s", bal, expected)
	}
}

func TestWallet_GetBalance_NoRPC(t *testing.T) {
	w, _ := NewWallet(WalletConfig{})
	_, err := w.GetBalance()
	if err == nil {
		t.Fatal("expected error with no RPC URL")
	}
	if !strings.Contains(err.Error(), "no RPC URL") {
		t.Errorf("error = %q", err)
	}
}

func TestWallet_GetBalance_RPCError(t *testing.T) {
	srv := mockRPCServer(t, func(_ string, _ []any) (any, error) {
		return nil, fmt.Errorf("rate limited")
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	_, err := w.GetBalance()
	if err == nil {
		t.Fatal("expected RPC error")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %q", err)
	}
}

func TestWallet_GetBalance_BadResultType(t *testing.T) {
	srv := mockRPCServer(t, func(_ string, _ []any) (any, error) {
		return 12345, nil // not a string
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	_, err := w.GetBalance()
	if err == nil {
		t.Fatal("expected error for non-string result")
	}
	if !strings.Contains(err.Error(), "unexpected balance type") {
		t.Errorf("error = %q", err)
	}
}

func TestWallet_GetERC20Balance_Success(t *testing.T) {
	srv := mockRPCServer(t, func(method string, _ []any) (any, error) {
		if method == "eth_call" {
			return "0x000000000000000000000000000000000000000000000000000000003b9aca00", nil
		}
		return nil, fmt.Errorf("unexpected method")
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	bal, err := w.GetERC20Balance("0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48")
	if err != nil {
		t.Fatal(err)
	}
	expected := big.NewInt(1_000_000_000) // 0x3b9aca00
	if bal.Cmp(expected) != 0 {
		t.Errorf("erc20 balance = %s, want %s", bal, expected)
	}
}

func TestWallet_GetERC20Balance_NoRPC(t *testing.T) {
	w, _ := NewWallet(WalletConfig{})
	_, err := w.GetERC20Balance("0xtoken")
	if err == nil {
		t.Fatal("expected error with no RPC URL")
	}
}

func TestWallet_GetERC20Balance_BadResultType(t *testing.T) {
	srv := mockRPCServer(t, func(_ string, _ []any) (any, error) {
		return 999, nil
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	_, err := w.GetERC20Balance("0xtoken")
	if err == nil {
		t.Fatal("expected error for non-string result")
	}
}

func TestWallet_GetChainID_Success(t *testing.T) {
	srv := mockRPCServer(t, func(method string, _ []any) (any, error) {
		if method == "eth_chainId" {
			return "0x2105", nil // 8453
		}
		return nil, fmt.Errorf("unexpected")
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	chainID, err := w.GetChainID()
	if err != nil {
		t.Fatal(err)
	}
	if chainID != 8453 {
		t.Errorf("chainID = %d, want 8453", chainID)
	}
}

func TestWallet_GetChainID_BadType(t *testing.T) {
	srv := mockRPCServer(t, func(_ string, _ []any) (any, error) {
		return true, nil
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	_, err := w.GetChainID()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWallet_GetTransactionCount_Success(t *testing.T) {
	srv := mockRPCServer(t, func(method string, _ []any) (any, error) {
		if method == "eth_getTransactionCount" {
			return "0x5", nil
		}
		return nil, fmt.Errorf("unexpected")
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	nonce, err := w.GetTransactionCount()
	if err != nil {
		t.Fatal(err)
	}
	if nonce != 5 {
		t.Errorf("nonce = %d, want 5", nonce)
	}
}

func TestWallet_GetTransactionCount_BadType(t *testing.T) {
	srv := mockRPCServer(t, func(_ string, _ []any) (any, error) {
		return []string{"bad"}, nil
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	_, err := w.GetTransactionCount()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWallet_EthCall_Success(t *testing.T) {
	srv := mockRPCServer(t, func(method string, _ []any) (any, error) {
		if method == "eth_call" {
			return "0xdeadbeef", nil
		}
		return nil, fmt.Errorf("unexpected")
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	result, err := w.EthCall("0xcontract", "0xdata")
	if err != nil {
		t.Fatal(err)
	}
	if result != "0xdeadbeef" {
		t.Errorf("result = %q", result)
	}
}

func TestWallet_EthCall_BadType(t *testing.T) {
	srv := mockRPCServer(t, func(_ string, _ []any) (any, error) {
		return 42, nil
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	_, err := w.EthCall("0xto", "0xdata")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWallet_SendRawTransaction_Success(t *testing.T) {
	srv := mockRPCServer(t, func(method string, _ []any) (any, error) {
		if method == "eth_sendRawTransaction" {
			return "0xabcdef1234567890", nil
		}
		return nil, fmt.Errorf("unexpected")
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	hash, err := w.SendRawTransaction("0x02deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if hash != "0xabcdef1234567890" {
		t.Errorf("hash = %q", hash)
	}
}

func TestWallet_SendRawTransaction_BadType(t *testing.T) {
	srv := mockRPCServer(t, func(_ string, _ []any) (any, error) {
		return 123, nil
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	_, err := w.SendRawTransaction("0x02dead")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWallet_SendTransaction_NoRPC(t *testing.T) {
	w, _ := NewWallet(WalletConfig{})
	_, err := w.SendTransaction("0xto", big.NewInt(1), nil)
	if err == nil {
		t.Fatal("expected error with no RPC")
	}
}

func TestWallet_SendTransactionWithParams_Success(t *testing.T) {
	srv := mockRPCServer(t, func(method string, _ []any) (any, error) {
		switch method {
		case "eth_sendRawTransaction":
			return "0xtxhash123", nil
		default:
			return nil, fmt.Errorf("unexpected method: %s", method)
		}
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL, ChainID: 1})
	hash, err := w.SendTransactionWithParams(
		"0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		big.NewInt(1000),
		nil,
		TxParams{Nonce: NonceVal(1), GasLimit: 21000},
	)
	if err != nil {
		t.Fatal(err)
	}
	if hash != "0xtxhash123" {
		t.Errorf("hash = %q", hash)
	}
}

func TestWallet_RpcCall_InvalidURL(t *testing.T) {
	w, _ := NewWallet(WalletConfig{RPCURL: "http://127.0.0.1:1"}) // unlikely to be listening
	_, err := w.rpcCall("eth_blockNumber", []any{})
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}

func TestWallet_RpcCall_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	_, err := w.rpcCall("eth_test", []any{})
	if err == nil {
		t.Fatal("expected error for bad JSON response")
	}
	if !strings.Contains(err.Error(), "decoding RPC response") {
		t.Errorf("error = %q", err)
	}
}

// ---------------------------------------------------------------------------
// secp256k1.go — Double, Money overflow, negative Money
// ---------------------------------------------------------------------------

func TestSecp256k1_Double(t *testing.T) {
	curve := secp256k1Curve()
	params := curve.Params()
	x, y := curve.Double(params.Gx, params.Gy)
	// 2*G should be on curve.
	if !curve.IsOnCurve(x, y) {
		t.Fatal("Double(G) not on curve")
	}
	// Should match ScalarBaseMult(2).
	x2, y2 := curve.ScalarBaseMult(big.NewInt(2).Bytes())
	if x.Cmp(x2) != 0 || y.Cmp(y2) != 0 {
		t.Fatal("Double(G) != 2*G")
	}
}

func TestSecp256k1_ScalarMult_Zero(t *testing.T) {
	curve := secp256k1Curve()
	params := curve.Params()
	x, y := curve.ScalarBaseMult(big.NewInt(0).Bytes())
	_ = params
	// 0*G should be the point at infinity (0, 0).
	if x.Sign() != 0 || y.Sign() != 0 {
		t.Errorf("0*G = (%s, %s), want (0, 0)", x, y)
	}
}

func TestSecp256k1_AddPointAtInfinity(t *testing.T) {
	curve := secp256k1Curve()
	params := curve.Params()
	// O + G = G
	x, y := curve.Add(big.NewInt(0), big.NewInt(0), params.Gx, params.Gy)
	if x.Cmp(params.Gx) != 0 || y.Cmp(params.Gy) != 0 {
		t.Fatal("O + G != G")
	}
	// G + O = G
	x2, y2 := curve.Add(params.Gx, params.Gy, big.NewInt(0), big.NewInt(0))
	if x2.Cmp(params.Gx) != 0 || y2.Cmp(params.Gy) != 0 {
		t.Fatal("G + O != G")
	}
}

func TestMoney_NegativeAmount(t *testing.T) {
	m := FromDollars(-5.50)
	if m.Cents() != -550 {
		t.Errorf("cents = %d, want -550", m.Cents())
	}
	s := m.String()
	if s != "-$5.50" {
		t.Errorf("String() = %q, want '-$5.50'", s)
	}
}

func TestMoney_SmallCents(t *testing.T) {
	// Test padTwo with single-digit cents.
	m := FromDollars(1.05)
	if m.String() != "$1.05" {
		t.Errorf("String() = %q, want '$1.05'", m.String())
	}
	m2 := FromDollars(0.09)
	if m2.String() != "$0.09" {
		t.Errorf("String() = %q, want '$0.09'", m2.String())
	}
}

func TestMoney_AddOverflow(t *testing.T) {
	m := Money{cents: maxInt64 - 10}
	result := m.Add(Money{cents: 100}) // would overflow
	if result.Cents() != maxInt64 {
		t.Errorf("overflow add: cents = %d, want %d", result.Cents(), maxInt64)
	}
}

func TestMoney_AddNegativeOverflow(t *testing.T) {
	m := Money{cents: minInt64 + 10}
	result := m.Add(Money{cents: -100}) // would underflow
	if result.Cents() != minInt64 {
		t.Errorf("underflow add: cents = %d, want %d", result.Cents(), minInt64)
	}
}

func TestMoney_Zero(t *testing.T) {
	m := FromDollars(0)
	if m.Cents() != 0 {
		t.Errorf("cents = %d, want 0", m.Cents())
	}
	if m.String() != "$0.00" {
		t.Errorf("String() = %q, want '$0.00'", m.String())
	}
}

// ---------------------------------------------------------------------------
// eip3009.go — NonceHex, Digest edge cases
// ---------------------------------------------------------------------------

func TestEIP3009_NonceHex(t *testing.T) {
	auth := sampleEIP3009Authorization()
	hexStr, err := auth.NonceHex()
	if err != nil {
		t.Fatal(err)
	}
	if len(hexStr) != 64 { // 32 bytes = 64 hex chars
		t.Errorf("nonce hex length = %d, want 64", len(hexStr))
	}
}

func TestEIP3009_NonceHex_Empty(t *testing.T) {
	auth := EIP3009Authorization{
		Token:       "0xtoken",
		From:        "0xfrom",
		To:          "0xto",
		Value:       big.NewInt(100),
		ValidBefore: 9999999999,
		ChainID:     1,
	}
	hexStr, err := auth.NonceHex()
	if err != nil {
		t.Fatal(err)
	}
	// Should be 32 zero bytes.
	if hexStr != strings.Repeat("00", 32) {
		t.Errorf("empty nonce hex = %q, want 64 zeros", hexStr)
	}
}

func TestEIP3009_NonceHex_InvalidLength(t *testing.T) {
	auth := EIP3009Authorization{Nonce: []byte{1, 2, 3}}
	_, err := auth.NonceHex()
	if err == nil {
		t.Fatal("expected error for bad nonce length")
	}
}

func TestEIP3009_Digest_NilValue(t *testing.T) {
	auth := EIP3009Authorization{
		Token:       "0xtoken",
		From:        "0xfrom",
		To:          "0xto",
		Value:       nil,
		ValidBefore: 9999999999,
		ChainID:     1,
	}
	_, err := auth.Digest()
	if err == nil {
		t.Fatal("expected error for nil value")
	}
}

func TestEIP3009_Digest_ZeroChainID(t *testing.T) {
	auth := EIP3009Authorization{
		Token:       "0xtoken",
		From:        "0xfrom",
		To:          "0xto",
		Value:       big.NewInt(100),
		ValidBefore: 9999999999,
		ChainID:     0,
	}
	_, err := auth.Digest()
	if err == nil {
		t.Fatal("expected error for zero chain ID")
	}
}

func TestSignEIP3009_NilPrivateKey(t *testing.T) {
	w := &Wallet{}
	auth := sampleEIP3009Authorization()
	_, err := w.SignEIP3009TransferWithAuthorization(auth)
	if err == nil {
		t.Fatal("expected error with nil private key")
	}
}

func TestSignEIP3009_DefaultsFromWallet(t *testing.T) {
	w, _ := NewWallet(WalletConfig{ChainID: 42})
	auth := EIP3009Authorization{
		Token:       "0xtoken",
		To:          "0xrecipient",
		Value:       big.NewInt(100),
		ValidBefore: 9999999999,
		Nonce:       make([]byte, 32),
		// From and ChainID left empty — should be filled from wallet.
	}
	sig, err := w.SignEIP3009TransferWithAuthorization(auth)
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) != 65 {
		t.Errorf("sig length = %d, want 65", len(sig))
	}
}

// ---------------------------------------------------------------------------
// yield.go — helper functions and error paths
// ---------------------------------------------------------------------------

func TestUsdcToWei(t *testing.T) {
	tests := []struct {
		amount float64
		want   int64
	}{
		{1.0, 1_000_000},
		{0.5, 500_000},
		{100.0, 100_000_000},
		{0.001, 1000},
	}
	for _, tt := range tests {
		got := usdcToWei(tt.amount)
		if got.Int64() != tt.want {
			t.Errorf("usdcToWei(%f) = %d, want %d", tt.amount, got.Int64(), tt.want)
		}
	}
}

func TestWeiToUSDC(t *testing.T) {
	tests := []struct {
		wei  *big.Int
		want float64
	}{
		{big.NewInt(1_000_000), 1.0},
		{big.NewInt(500_000), 0.5},
		{nil, 0},
		{big.NewInt(0), 0},
	}
	for _, tt := range tests {
		got := weiToUSDC(tt.wei)
		if math.Abs(got-tt.want) > 0.001 {
			t.Errorf("weiToUSDC(%v) = %f, want %f", tt.wei, got, tt.want)
		}
	}
}

func TestPadAddress(t *testing.T) {
	result := padAddress("0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045")
	if len(result) != 32 {
		t.Errorf("padAddress length = %d, want 32", len(result))
	}
	// First 12 bytes should be zero.
	for i := 0; i < 12; i++ {
		if result[i] != 0 {
			t.Errorf("byte %d = %x, want 0", i, result[i])
		}
	}
}

func TestPadUint256(t *testing.T) {
	v := big.NewInt(1_000_000)
	result := padUint256(v)
	if len(result) != 32 {
		t.Errorf("padUint256 length = %d, want 32", len(result))
	}
	// Decode back.
	n := new(big.Int).SetBytes(result)
	if n.Cmp(v) != 0 {
		t.Errorf("roundtrip: got %s, want %s", n, v)
	}
}

func TestBuildCalldata(t *testing.T) {
	selector := []byte{0x12, 0x34, 0x56, 0x78}
	arg := make([]byte, 32)
	arg[31] = 0x01

	data := buildCalldata(selector, arg)
	if len(data) != 36 {
		t.Errorf("calldata length = %d, want 36", len(data))
	}
	if hex.EncodeToString(data[:4]) != "12345678" {
		t.Errorf("selector = %x, want 12345678", data[:4])
	}
}

func TestParseUint256(t *testing.T) {
	tests := []struct {
		hex  string
		want int64
	}{
		{"0x3b9aca00", 1_000_000_000},
		{"3b9aca00", 1_000_000_000},
		{"0x0", 0},
		{"0x1", 1},
	}
	for _, tt := range tests {
		n := parseUint256(tt.hex)
		if n.Int64() != tt.want {
			t.Errorf("parseUint256(%q) = %d, want %d", tt.hex, n.Int64(), tt.want)
		}
	}
}

func TestCalculateExcess_NegativeResult(t *testing.T) {
	y := NewYieldEngine(YieldConfig{Enabled: true, MinDeposit: 10.0}, nil)
	excess := y.CalculateExcess(50.0, 100.0) // 50 - 100 - 10 = -60 → clamp to 0
	if excess != 0 {
		t.Errorf("excess = %f, want 0", excess)
	}
}

func TestYieldEngine_Deposit_NotEnabled(t *testing.T) {
	y := NewYieldEngine(YieldConfig{Enabled: false}, nil)
	_, err := y.Deposit(100.0, "0xagent")
	if err == nil {
		t.Fatal("expected error when not enabled")
	}
}

func TestYieldEngine_Deposit_NoRPCURL(t *testing.T) {
	y := NewYieldEngine(YieldConfig{Enabled: true}, nil)
	_, err := y.Deposit(100.0, "0xagent")
	if err == nil {
		t.Fatal("expected error for missing RPC URL")
	}
}

func TestYieldEngine_Deposit_NoPoolAddress(t *testing.T) {
	y := NewYieldEngine(YieldConfig{
		Enabled:     true,
		ChainRPCURL: "http://localhost:8545",
	}, nil)
	_, err := y.Deposit(100.0, "0xagent")
	if err == nil {
		t.Fatal("expected error for missing pool address")
	}
}

func TestYieldEngine_Deposit_DryRun(t *testing.T) {
	y := NewYieldEngine(YieldConfig{
		Enabled:     true,
		ChainRPCURL: "http://localhost:8545",
		PoolAddress: "0xpool",
		USDCAddress: "0xusdc",
		Protocol:    "aave",
	}, nil) // no wallet = dry run
	hash, err := y.Deposit(100.0, "0xagent")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "0x_dryrun_supply_") {
		t.Errorf("expected dry run hash, got %q", hash)
	}
}

func TestYieldEngine_Withdraw_NotEnabled(t *testing.T) {
	y := NewYieldEngine(YieldConfig{Enabled: false}, nil)
	_, err := y.Withdraw(50.0, "0xagent")
	if err == nil {
		t.Fatal("expected error when not enabled")
	}
}

func TestYieldEngine_Withdraw_NoRPCURL(t *testing.T) {
	y := NewYieldEngine(YieldConfig{Enabled: true}, nil)
	_, err := y.Withdraw(50.0, "0xagent")
	if err == nil {
		t.Fatal("expected error for missing RPC URL")
	}
}

func TestYieldEngine_Withdraw_DryRun(t *testing.T) {
	y := NewYieldEngine(YieldConfig{
		Enabled:     true,
		ChainRPCURL: "http://localhost:8545",
		PoolAddress: "0xpool",
		USDCAddress: "0xusdc",
		Protocol:    "aave",
	}, nil)
	hash, err := y.Withdraw(50.0, "0xagent")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "0x_dryrun_withdraw_") {
		t.Errorf("expected dry run hash, got %q", hash)
	}
}

func TestYieldEngine_GetATokenBalance_NoRPC(t *testing.T) {
	y := NewYieldEngine(YieldConfig{Enabled: true}, nil)
	_, err := y.GetATokenBalance("0xagent")
	if err == nil {
		t.Fatal("expected error for missing RPC URL")
	}
}

func TestYieldEngine_GetATokenBalance_NoATokenAddress(t *testing.T) {
	y := NewYieldEngine(YieldConfig{
		Enabled:     true,
		ChainRPCURL: "http://localhost:8545",
	}, nil)
	_, err := y.GetATokenBalance("0xagent")
	if err == nil {
		t.Fatal("expected error for missing aToken address")
	}
}

func TestYieldEngine_GetATokenBalance_NoWallet(t *testing.T) {
	y := NewYieldEngine(YieldConfig{
		Enabled:       true,
		ChainRPCURL:   "http://localhost:8545",
		ATokenAddress: "0xatoken",
	}, nil)
	bal, err := y.GetATokenBalance("0xagent")
	if err != nil {
		t.Fatal(err)
	}
	if bal != 0 {
		t.Errorf("balance = %f, want 0", bal)
	}
}

func TestYieldEngine_GetATokenBalance_WithWallet(t *testing.T) {
	srv := mockRPCServer(t, func(method string, _ []any) (any, error) {
		if method == "eth_call" {
			// 1 USDC = 1000000 in 32-byte hex
			return "0x00000000000000000000000000000000000000000000000000000000000f4240", nil
		}
		return nil, fmt.Errorf("unexpected")
	})
	defer srv.Close()

	w, _ := NewWallet(WalletConfig{RPCURL: srv.URL})
	y := NewYieldEngine(YieldConfig{
		Enabled:       true,
		ChainRPCURL:   srv.URL,
		ATokenAddress: "0xatoken",
	}, w)
	bal, err := y.GetATokenBalance("0xagent")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(bal-1.0) > 0.001 {
		t.Errorf("balance = %f, want ~1.0", bal)
	}
}

func TestYieldEngine_ShouldDeposit_Disabled(t *testing.T) {
	y := NewYieldEngine(YieldConfig{Enabled: false, MinDeposit: 10.0}, nil)
	if y.ShouldDeposit(50.0) {
		t.Error("should not deposit when disabled")
	}
}

func TestYieldEngine_ShouldWithdraw_Disabled(t *testing.T) {
	y := NewYieldEngine(YieldConfig{Enabled: false, WithdrawalThreshold: 20.0}, nil)
	if y.ShouldWithdraw(10.0) {
		t.Error("should not withdraw when disabled")
	}
}

// ---------------------------------------------------------------------------
// x402.go — Handle402 edge cases
// ---------------------------------------------------------------------------

func TestHandle402_NilPrivateKey(t *testing.T) {
	w := &Wallet{address: "0xtest"}
	body, _ := json.Marshal(PaymentRequirements{
		Amount:    0.01,
		Recipient: "0xrecipient",
		ChainID:   1,
	})
	h := NewX402Handler()
	_, err := h.Handle402(body, w)
	if err == nil {
		t.Fatal("expected error with nil private key")
	}
}

func TestHandlePayment_InvalidJSON(t *testing.T) {
	h := NewX402HandlerWithWallet(nil)
	_, err := h.HandlePayment([]byte("bad json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestHandle402_WithNonce(t *testing.T) {
	w, _ := NewWallet(WalletConfig{})
	nonce := hex.EncodeToString(make([]byte, 32))
	body, _ := json.Marshal(PaymentRequirements{
		Amount:    0.01,
		Recipient: "0x1234567890abcdef1234567890abcdef12345678",
		ChainID:   8453,
		Nonce:     nonce,
	})
	h := NewX402Handler()
	header, err := h.Handle402(body, w)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(header, "x402 ") {
		t.Errorf("header = %q, want x402 prefix", header)
	}
}

// ---------------------------------------------------------------------------
// RLP — edge cases for unknown types
// ---------------------------------------------------------------------------

func TestRLPEncodeItem_UnknownType(t *testing.T) {
	// Unknown types should encode as empty bytes (0x80).
	got := rlpEncodeItem("a string type")
	if len(got) != 1 || got[0] != 0x80 {
		t.Errorf("unknown type: got %x, want 80", got)
	}
}

func TestRLPEncodeItem_NilBigInt(t *testing.T) {
	var n *big.Int
	got := rlpEncodeItem(n)
	if len(got) != 1 || got[0] != 0x80 {
		t.Errorf("nil big.Int: got %x, want 80", got)
	}
}

// ---------------------------------------------------------------------------
// treasury.go — zero-config and edge cases
// ---------------------------------------------------------------------------

func TestTreasuryPolicy_ZeroConfig_AllowsEverything(t *testing.T) {
	p := NewTreasuryPolicy(TreasuryConfig{})
	if err := p.CheckPerPayment(999999); err != nil {
		t.Errorf("zero cap should allow any payment: %v", err)
	}
	if err := p.CheckHourlyLimit(999999, 999999); err != nil {
		t.Errorf("zero hourly limit should allow: %v", err)
	}
	if err := p.CheckDailyLimit(999999, 999999); err != nil {
		t.Errorf("zero daily limit should allow: %v", err)
	}
	if err := p.CheckMinimumReserve(0); err != nil {
		t.Errorf("zero reserve should allow: %v", err)
	}
}

func TestTreasuryPolicy_DailyInferenceBudget(t *testing.T) {
	cfg := TreasuryConfig{DailyInferenceBudget: 50.0}
	p := NewTreasuryPolicy(cfg)
	if p.Config().DailyInferenceBudget != 50.0 {
		t.Errorf("inference budget = %f", p.Config().DailyInferenceBudget)
	}
}

// ---------------------------------------------------------------------------
// wallet.go — GenerateAndSave with unwritable path
// ---------------------------------------------------------------------------

func TestWallet_GenerateAndSave_UnwritablePath(t *testing.T) {
	// Path to a non-existent deeply nested directory.
	path := filepath.Join(t.TempDir(), "no", "such", "dir", "wallet.enc")
	_, err := NewWallet(WalletConfig{
		Path:       path,
		Passphrase: "test",
	})
	if err == nil {
		t.Fatal("expected error for unwritable path")
	}
	if !strings.Contains(err.Error(), "wallet save") {
		t.Errorf("error = %q, want 'wallet save'", err)
	}
}
