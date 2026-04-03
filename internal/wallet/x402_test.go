package wallet

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePaymentRequirements_Valid(t *testing.T) {
	body, _ := json.Marshal(PaymentRequirements{
		Amount:    0.50,
		Recipient: "0x1234567890abcdef1234567890abcdef12345678",
		ChainID:   8453,
	})
	h := NewX402Handler()
	reqs, err := h.ParsePaymentRequirements(body)
	if err != nil {
		t.Fatal(err)
	}
	if reqs.Amount != 0.50 {
		t.Errorf("amount = %f, want 0.50", reqs.Amount)
	}
	if reqs.Recipient == "" {
		t.Error("recipient should not be empty")
	}
}

func TestParsePaymentRequirements_InvalidJSON(t *testing.T) {
	h := NewX402Handler()
	_, err := h.ParsePaymentRequirements([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParsePaymentRequirements_ZeroAmount(t *testing.T) {
	body, _ := json.Marshal(PaymentRequirements{Amount: 0, Recipient: "0x123"})
	h := NewX402Handler()
	_, err := h.ParsePaymentRequirements(body)
	if err == nil {
		t.Error("expected error for zero amount")
	}
}

func TestParsePaymentRequirements_MissingRecipient(t *testing.T) {
	body, _ := json.Marshal(PaymentRequirements{Amount: 1.0})
	h := NewX402Handler()
	_, err := h.ParsePaymentRequirements(body)
	if err == nil {
		t.Error("expected error for missing recipient")
	}
}

func TestBuildPaymentHeader(t *testing.T) {
	h := NewX402Handler()
	header := h.BuildPaymentHeader(1.5, "0xrecipient", "0xsignature")
	if header == "" {
		t.Fatal("header should not be empty")
	}
	// Check format: "x402 amount=... recipient=... auth=..."
	if len(header) < 10 {
		t.Errorf("header too short: %q", header)
	}
}

func TestHandle402_WithWallet(t *testing.T) {
	w, err := NewWallet(WalletConfig{})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(PaymentRequirements{
		Amount:    0.01,
		Recipient: "0x1234567890abcdef1234567890abcdef12345678",
		ChainID:   8453,
	})
	h := NewX402Handler()
	header, err := h.Handle402(body, w)
	if err != nil {
		t.Fatal(err)
	}
	if header == "" {
		t.Error("header should not be empty")
	}
}

func TestHandlePayment_Success(t *testing.T) {
	w, err := NewWallet(WalletConfig{})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(PaymentRequirements{
		Amount:    0.50,
		Recipient: "0x1234567890abcdef1234567890abcdef12345678",
		ChainID:   8453,
	})
	h := NewX402HandlerWithWallet(w)
	header, err := h.HandlePayment(body)
	if err != nil {
		t.Fatalf("HandlePayment: %v", err)
	}
	if header == "" {
		t.Error("expected non-empty payment header")
	}
}

func TestHandlePayment_ExceedsMaxAmount(t *testing.T) {
	w, err := NewWallet(WalletConfig{})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(PaymentRequirements{
		Amount:    5.00, // exceeds $1.00 limit
		Recipient: "0x1234567890abcdef1234567890abcdef12345678",
		ChainID:   8453,
	})
	h := NewX402HandlerWithWallet(w)
	_, err = h.HandlePayment(body)
	if err == nil {
		t.Fatal("expected error for amount exceeding safety limit")
	}
	if !strings.Contains(err.Error(), "safety limit") {
		t.Errorf("error = %q, want safety limit message", err)
	}
}

func TestHandlePayment_NoWallet(t *testing.T) {
	body, _ := json.Marshal(PaymentRequirements{
		Amount:    0.10,
		Recipient: "0x1234567890abcdef1234567890abcdef12345678",
		ChainID:   8453,
	})
	h := NewX402Handler() // no wallet
	_, err := h.HandlePayment(body)
	if err == nil {
		t.Fatal("expected error when no wallet configured")
	}
	if !strings.Contains(err.Error(), "no wallet") {
		t.Errorf("error = %q, want 'no wallet' message", err)
	}
}

func TestHandlePayment_ExactMaxAmount(t *testing.T) {
	w, err := NewWallet(WalletConfig{})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(PaymentRequirements{
		Amount:    1.00, // exactly at limit -- should succeed
		Recipient: "0x1234567890abcdef1234567890abcdef12345678",
		ChainID:   8453,
	})
	h := NewX402HandlerWithWallet(w)
	header, err := h.HandlePayment(body)
	if err != nil {
		t.Fatalf("HandlePayment at exact limit: %v", err)
	}
	if header == "" {
		t.Error("expected non-empty payment header")
	}
}

func TestSignDigest_Structure(t *testing.T) {
	w, err := NewWallet(WalletConfig{})
	if err != nil {
		t.Fatal(err)
	}
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i)
	}
	sig, err := signDigest(w.PrivateKey(), digest)
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) != 65 {
		t.Fatalf("signature length = %d, want 65", len(sig))
	}
	// v should be 27 or 28.
	v := sig[64]
	if v != 27 && v != 28 {
		t.Errorf("v = %d, want 27 or 28", v)
	}
}

func TestSignDigest_Deterministic(t *testing.T) {
	w, err := NewWallet(WalletConfig{})
	if err != nil {
		t.Fatal(err)
	}
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i + 1)
	}
	sig1, _ := signDigest(w.PrivateKey(), digest)
	sig2, _ := signDigest(w.PrivateKey(), digest)
	// RFC 6979 is deterministic — same key + same digest = same signature.
	for i := range sig1 {
		if sig1[i] != sig2[i] {
			t.Fatalf("signature not deterministic at byte %d", i)
		}
	}
}

func TestRfc6979K_Deterministic(t *testing.T) {
	w, err := NewWallet(WalletConfig{})
	if err != nil {
		t.Fatal(err)
	}
	hash := make([]byte, 32)
	hash[0] = 0xAB
	k1 := rfc6979K(w.PrivateKey(), hash)
	k2 := rfc6979K(w.PrivateKey(), hash)
	if k1.Cmp(k2) != 0 {
		t.Fatal("rfc6979K not deterministic")
	}
	// k should be in valid range [1, N-1].
	n := w.PrivateKey().Curve.Params().N
	if k1.Sign() <= 0 || k1.Cmp(n) >= 0 {
		t.Fatalf("k out of range: %s", k1.String())
	}
}

func TestEIP712DomainSeparator_Deterministic(t *testing.T) {
	d1 := eip712DomainSeparator("0xtoken", 8453)
	d2 := eip712DomainSeparator("0xtoken", 8453)
	if len(d1) != 32 {
		t.Fatalf("domain separator length = %d, want 32", len(d1))
	}
	for i := range d1 {
		if d1[i] != d2[i] {
			t.Fatal("domain separator not deterministic")
		}
	}
}
