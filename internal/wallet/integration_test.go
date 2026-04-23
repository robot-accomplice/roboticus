package wallet

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWallet_CreateAndLoad creates a wallet with a passphrase, saves it to
// disk, reloads it, and verifies the address survives the round trip.
func TestWallet_CreateAndLoad(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "wallet.key")
	passphrase := "test-passphrase-42"

	// Create and save.
	w1, err := NewWallet(WalletConfig{
		Path:       keyPath,
		Passphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	if w1.Address() == "" {
		t.Fatal("created wallet has empty address")
	}

	// File must exist on disk.
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("wallet file not saved: %v", err)
	}

	// Reload from the same path+passphrase.
	w2, err := NewWallet(WalletConfig{
		Path:       keyPath,
		Passphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("reload wallet: %v", err)
	}

	if w1.Address() != w2.Address() {
		t.Errorf("address mismatch after reload: %s != %s", w1.Address(), w2.Address())
	}
}

// TestWallet_AddressDerivation verifies that the same private key bytes always
// produce the same address (deterministic derivation).
func TestWallet_AddressDerivation(t *testing.T) {
	// Create two wallets from the same key bytes via fromBytes.
	keyBytes := make([]byte, 32)
	// Use a fixed pattern so the test is deterministic.
	for i := range keyBytes {
		keyBytes[i] = byte(i + 1)
	}

	w1 := &Wallet{}
	if err := w1.fromBytes(keyBytes); err != nil {
		t.Fatalf("fromBytes (1): %v", err)
	}

	w2 := &Wallet{}
	if err := w2.fromBytes(keyBytes); err != nil {
		t.Fatalf("fromBytes (2): %v", err)
	}

	if w1.Address() != w2.Address() {
		t.Errorf("same key produced different addresses: %s vs %s", w1.Address(), w2.Address())
	}

	// Address should be a valid 0x-prefixed 40-hex-char string.
	if err := ValidateAddress(w1.Address()); err != nil {
		t.Errorf("derived address is invalid: %v", err)
	}
}

// TestWallet_EncryptDecrypt encrypts a wallet key, then decrypts it with the
// correct passphrase and verifies the key is intact.
func TestWallet_EncryptDecrypt(t *testing.T) {
	passphrase := "encrypt-test-pass"

	// Generate a fresh key.
	w := &Wallet{cfg: WalletConfig{Passphrase: passphrase}}
	if err := w.generate(); err != nil {
		t.Fatalf("generate: %v", err)
	}

	originalAddr := w.Address()
	if originalAddr == "" {
		t.Fatal("generated wallet has empty address")
	}

	// Encrypt the private key.
	// Test-only: production persistence uses PKCS8 via crypto/x509; this test
	// exercises the AEAD envelope directly on raw key material to validate
	// round-trip. The Go 1.26 deprecation on privateKey.D applies to production
	// key manipulation where invariants can be broken — not to a read-only copy
	// for testing the encryption layer.
	keyBytes := w.privateKey.D.Bytes() //nolint:staticcheck // SA1019: test-only raw-byte access

	ciphertext, err := w.encrypt(keyBytes)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if len(ciphertext) < 44 { // 16 salt + 12 nonce + 16 tag minimum
		t.Fatalf("ciphertext too short: %d bytes", len(ciphertext))
	}

	// Decrypt and restore into a new wallet instance.
	w2 := &Wallet{cfg: WalletConfig{Passphrase: passphrase}}
	if err := w2.decrypt(ciphertext); err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if w2.Address() != originalAddr {
		t.Errorf("address after decrypt: got %s, want %s", w2.Address(), originalAddr)
	}

	// Wrong passphrase must fail.
	w3 := &Wallet{cfg: WalletConfig{Passphrase: "wrong-pass"}}
	if err := w3.decrypt(ciphertext); err == nil {
		t.Error("decrypt with wrong passphrase should fail")
	}
}
