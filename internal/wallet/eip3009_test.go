package wallet

import (
	"bytes"
	"math/big"
	"testing"
)

func sampleEIP3009Authorization() EIP3009Authorization {
	return EIP3009Authorization{
		Token:       "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
		From:        "0x1111111111111111111111111111111111111111",
		To:          "0x2222222222222222222222222222222222222222",
		Value:       big.NewInt(125000),
		ValidAfter:  0,
		ValidBefore: 1712345678,
		Nonce:       bytes.Repeat([]byte{0xAB}, 32),
		ChainID:     8453,
	}
}

func TestEIP3009AuthorizationDigest_Deterministic(t *testing.T) {
	auth := sampleEIP3009Authorization()

	d1, err := auth.Digest()
	if err != nil {
		t.Fatal(err)
	}
	d2, err := auth.Digest()
	if err != nil {
		t.Fatal(err)
	}

	if len(d1) != 32 {
		t.Fatalf("digest length = %d, want 32", len(d1))
	}
	if !bytes.Equal(d1, d2) {
		t.Fatal("digest should be deterministic")
	}
}

func TestEIP3009AuthorizationNonceValidation(t *testing.T) {
	auth := sampleEIP3009Authorization()
	auth.Nonce = []byte{0x01, 0x02}

	_, err := auth.Digest()
	if err == nil {
		t.Fatal("expected error for invalid nonce length")
	}
}

func TestWalletSignEIP3009TransferWithAuthorization(t *testing.T) {
	w, err := NewWallet(WalletConfig{})
	if err != nil {
		t.Fatal(err)
	}

	auth := sampleEIP3009Authorization()
	auth.From = ""

	sig, err := w.SignEIP3009TransferWithAuthorization(auth)
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) != 65 {
		t.Fatalf("signature length = %d, want 65", len(sig))
	}
}
