package wallet

import (
	"encoding/hex"
	"math/big"
	"testing"
)

func TestBuildSignedTx_Structure(t *testing.T) {
	w, err := NewWallet(WalletConfig{ChainID: 1})
	if err != nil {
		t.Fatal(err)
	}

	to := "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	value := big.NewInt(1_000_000_000_000_000) // 0.001 ETH
	params := TxParams{
		Nonce:                NonceVal(1),
		MaxPriorityFeePerGas: big.NewInt(1_000_000_000),
		MaxFeePerGas:         big.NewInt(30_000_000_000),
		GasLimit:             21000,
	}

	raw, err := w.BuildSignedTx(to, value, nil, params)
	if err != nil {
		t.Fatalf("BuildSignedTx failed: %v", err)
	}

	// Must start with 0x02 (EIP-1559 type prefix).
	if len(raw) == 0 {
		t.Fatal("empty raw transaction")
	}
	if raw[0] != 0x02 {
		t.Errorf("type prefix: got %x, want 02", raw[0])
	}

	// Must be at least type(1) + RLP overhead + signature.
	// Minimum: type(1) + list prefix + fields ~ > 80 bytes
	if len(raw) < 80 {
		t.Errorf("raw tx too short: %d bytes", len(raw))
	}

	t.Logf("signed tx (%d bytes): %s", len(raw), hex.EncodeToString(raw))
}

func TestBuildSignedTx_Deterministic(t *testing.T) {
	w, err := NewWallet(WalletConfig{ChainID: 1})
	if err != nil {
		t.Fatal(err)
	}

	to := "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	value := big.NewInt(1_000_000_000_000_000)
	params := TxParams{
		Nonce:                NonceVal(5),
		MaxPriorityFeePerGas: big.NewInt(2_000_000_000),
		MaxFeePerGas:         big.NewInt(50_000_000_000),
		GasLimit:             21000,
	}

	raw1, err := w.BuildSignedTx(to, value, nil, params)
	if err != nil {
		t.Fatal(err)
	}
	raw2, err := w.BuildSignedTx(to, value, nil, params)
	if err != nil {
		t.Fatal(err)
	}

	h1 := hex.EncodeToString(raw1)
	h2 := hex.EncodeToString(raw2)
	if h1 != h2 {
		t.Errorf("signed tx not deterministic:\n  %s\n  %s", h1, h2)
	}
}

func TestBuildSignedTx_ContractCall(t *testing.T) {
	w, err := NewWallet(WalletConfig{ChainID: 8453})
	if err != nil {
		t.Fatal(err)
	}

	to := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	// ERC-20 transfer(address,uint256) selector
	data, _ := hex.DecodeString("a9059cbb000000000000000000000000d8dA6BF26964aF9D7eEd9e03E53415D37aA960450000000000000000000000000000000000000000000000000000000005f5e100")
	params := TxParams{
		Nonce:                NonceVal(1), // must be non-zero to avoid RPC fetch
		MaxPriorityFeePerGas: big.NewInt(1_000_000_000),
		MaxFeePerGas:         big.NewInt(30_000_000_000),
		GasLimit:             100000,
	}

	raw, err := w.BuildSignedTx(to, big.NewInt(0), data, params)
	if err != nil {
		t.Fatalf("BuildSignedTx with data failed: %v", err)
	}

	if raw[0] != 0x02 {
		t.Errorf("type prefix: got %x, want 02", raw[0])
	}

	// Contract call tx will be larger due to calldata.
	if len(raw) < 120 {
		t.Errorf("contract call tx too short: %d bytes", len(raw))
	}

	t.Logf("contract call tx (%d bytes): %s", len(raw), hex.EncodeToString(raw))
}

func TestBuildSignedTx_DefaultGasLimit(t *testing.T) {
	w, err := NewWallet(WalletConfig{ChainID: 1})
	if err != nil {
		t.Fatal(err)
	}

	to := "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"

	// ETH transfer (no data) -> default 21000
	rawETH, err := w.BuildSignedTx(to, big.NewInt(1), nil, TxParams{Nonce: NonceVal(1)})
	if err != nil {
		t.Fatal(err)
	}

	// Contract call (with data) -> default 100000
	data := []byte{0xa9, 0x05, 0x9c, 0xbb}
	rawCall, err := w.BuildSignedTx(to, big.NewInt(0), data, TxParams{Nonce: NonceVal(1)})
	if err != nil {
		t.Fatal(err)
	}

	// Both must be valid EIP-1559 transactions.
	if rawETH[0] != 0x02 || rawCall[0] != 0x02 {
		t.Error("both should be EIP-1559 type")
	}

	// Contract call will be different (different gas limit + calldata).
	if hex.EncodeToString(rawETH) == hex.EncodeToString(rawCall) {
		t.Error("ETH transfer and contract call should produce different tx bytes")
	}
}

func TestBuildSignedTx_NilValue(t *testing.T) {
	w, err := NewWallet(WalletConfig{ChainID: 1})
	if err != nil {
		t.Fatal(err)
	}

	// nil value should not panic.
	raw, err := w.BuildSignedTx(
		"0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		nil,
		nil,
		TxParams{Nonce: NonceVal(1), GasLimit: 21000},
	)
	if err != nil {
		t.Fatal(err)
	}
	if raw[0] != 0x02 {
		t.Errorf("type prefix: got %x, want 02", raw[0])
	}
}

func TestBuildSignedTx_InvalidAddress(t *testing.T) {
	w, err := NewWallet(WalletConfig{ChainID: 1})
	if err != nil {
		t.Fatal(err)
	}

	_, err = w.BuildSignedTx("not-hex", big.NewInt(0), nil, TxParams{Nonce: NonceVal(1)})
	if err == nil {
		t.Error("expected error for invalid address")
	}
}

func TestHexToBytes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0xdeadbeef", "deadbeef"},
		{"deadbeef", "deadbeef"},
		{"0x00", "00"},
		{"0x", ""},
	}
	for _, tt := range tests {
		got, err := hexToBytes(tt.input)
		if err != nil {
			t.Errorf("hexToBytes(%q): %v", tt.input, err)
			continue
		}
		if hex.EncodeToString(got) != tt.want {
			t.Errorf("hexToBytes(%q): got %x, want %s", tt.input, got, tt.want)
		}
	}
}

// TestBuildSignedTx_KnownKey verifies that a transaction built with a known
// private key produces consistent, verifiable output.
func TestBuildSignedTx_KnownKey(t *testing.T) {
	// Use a deterministic key for reproducibility.
	keyHex := "4c0883a69102937d6231471b5dbb6204fe512961708279f4d1b5c2a69b5c8d1f"
	keyBytes, _ := hex.DecodeString(keyHex)

	w, err := NewWallet(WalletConfig{ChainID: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Override with known key.
	if err := w.fromBytes(keyBytes); err != nil {
		t.Fatal(err)
	}

	to := "0x3535353535353535353535353535353535353535"

	params := TxParams{
		Nonce:                NonceVal(9),
		MaxPriorityFeePerGas: big.NewInt(1_000_000_000),
		MaxFeePerGas:         big.NewInt(30_000_000_000),
		GasLimit:             21000,
	}
	value := big.NewInt(1_000_000_000_000_000_000) // 1 ETH

	raw, err := w.BuildSignedTx(to, value, nil, params)
	if err != nil {
		t.Fatal(err)
	}

	// Build again to confirm determinism.
	raw2, err := w.BuildSignedTx(to, value, nil, params)
	if err != nil {
		t.Fatal(err)
	}

	if hex.EncodeToString(raw) != hex.EncodeToString(raw2) {
		t.Error("known key tx not deterministic")
	}

	// Verify structure: type byte + RLP list.
	if raw[0] != 0x02 {
		t.Errorf("type byte: got %x, want 02", raw[0])
	}

	// The second byte should be a list prefix (0xc0+ or 0xf7+).
	if raw[1] < 0xc0 {
		t.Errorf("expected RLP list prefix, got %x", raw[1])
	}

	t.Logf("known key address: %s", w.Address())
	t.Logf("signed tx: 0x%s", hex.EncodeToString(raw))
}
