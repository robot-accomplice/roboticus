package wallet

import "testing"

func TestAaveSupplySelector(t *testing.T) {
	want := []byte{0x61, 0x7b, 0xa0, 0x37}
	if string(aaveSupplySelector) != string(want) {
		t.Fatalf("selector = %x, want %x", aaveSupplySelector, want)
	}
}

func TestERC20BalanceOfSelector(t *testing.T) {
	want := []byte{0x70, 0xa0, 0x82, 0x31}
	if string(erc20BalanceOfSelector) != string(want) {
		t.Fatalf("selector = %x, want %x", erc20BalanceOfSelector, want)
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
