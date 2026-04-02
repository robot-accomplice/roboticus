package wallet

import "testing"

func TestMustDecodeHex(t *testing.T) {
	// Valid hex.
	result := mustDecodeHex("48656c6c6f")
	if string(result) != "Hello" {
		t.Errorf("decoded = %q", string(result))
	}
}

func TestMustDecodeHex_Empty(t *testing.T) {
	result := mustDecodeHex("")
	if len(result) != 0 {
		t.Errorf("empty hex should decode to empty, got %d bytes", len(result))
	}
}

func TestWallet_NewWallet_Defaults(t *testing.T) {
	w, err := NewWallet(WalletConfig{})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if w == nil {
		t.Fatal("nil")
	}
}

func TestWallet_Address_Generated(t *testing.T) {
	w, _ := NewWallet(WalletConfig{})
	addr := w.Address()
	if addr == "" {
		t.Error("should generate address")
	}
	if len(addr) < 10 {
		t.Errorf("address too short: %s", addr)
	}
}
