package wallet

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
)

// walletFile is the JSON structure for wallet persistence (matches Rust WalletFile).
type walletFile struct {
	Address       string `json:"address"`
	ChainID       int64  `json:"chain_id"`
	PrivateKeyHex string `json:"private_key_hex"`
}

// MigrateResult describes the outcome of a plaintext wallet migration.
type MigrateResult struct {
	Migrated   bool   // true if encryption was performed
	Address    string // wallet address
	Passphrase string // generated passphrase (display once, then discard)
}

// MigratePlaintextWallet checks if the wallet file at path is unencrypted JSON.
// If so, it encrypts it with a newly generated passphrase and returns the
// passphrase so the caller can display it exactly once and store it in the
// keystore.
//
// If the file is already encrypted, missing, or empty, this is a no-op.
func MigratePlaintextWallet(path string) (*MigrateResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return &MigrateResult{}, nil // no file = nothing to migrate
	}
	if len(data) == 0 {
		return &MigrateResult{}, nil
	}

	// Check if it's valid plaintext JSON with the expected wallet fields.
	var wf walletFile
	if err := json.Unmarshal(data, &wf); err != nil {
		return &MigrateResult{}, nil // not JSON = already encrypted or corrupt
	}
	if wf.Address == "" || wf.PrivateKeyHex == "" {
		return &MigrateResult{}, nil // not a wallet file
	}

	// It's a plaintext wallet. Generate a secure passphrase.
	passphrase, err := generatePassphrase()
	if err != nil {
		return nil, fmt.Errorf("wallet migration: failed to generate passphrase: %w", err)
	}

	// Parse the private key to validate it.
	keyBytes, err := hex.DecodeString(wf.PrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("wallet migration: invalid private key hex: %w", err)
	}

	// Create a temporary wallet to do the encryption.
	w := &Wallet{cfg: WalletConfig{Path: path, Passphrase: passphrase, ChainID: wf.ChainID}}
	if err := w.fromBytes(keyBytes); err != nil {
		return nil, fmt.Errorf("wallet migration: failed to parse wallet: %w", err)
	}

	// Re-serialize and encrypt.
	encrypted, err := w.encrypt(data)
	if err != nil {
		return nil, fmt.Errorf("wallet migration: failed to encrypt: %w", err)
	}

	// Write the encrypted file (same path, replacing the plaintext).
	if err := os.WriteFile(path, encrypted, 0o600); err != nil {
		return nil, fmt.Errorf("wallet migration: failed to write encrypted wallet: %w", err)
	}

	log.Info().
		Str("address", w.address).
		Str("path", path).
		Msg("wallet encrypted successfully — plaintext key removed from disk")

	return &MigrateResult{
		Migrated:   true,
		Address:    w.address,
		Passphrase: passphrase,
	}, nil
}

// generatePassphrase creates a cryptographically random 32-byte hex string.
func generatePassphrase() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
