package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/crypto/scrypt"
)

// Keystore provides encrypted storage for API keys, tokens, and other secrets.
// It stores an AES-256-GCM encrypted JSON file on disk, with scrypt key derivation
// from a master passphrase.
//
// Usage:
//
//	ks, err := OpenKeystore("~/.roboticus/keystore.enc", masterKey)
//	apiKey, err := ks.Get("openai_api_key")
//	ks.Set("anthropic_api_key", "sk-ant-...")
//	ks.Save()
type Keystore struct {
	mu        sync.RWMutex
	path      string
	masterKey []byte // derived from passphrase via scrypt
	secrets   map[string]string
	dirty     bool
}

// KeystoreConfig holds keystore initialization options.
type KeystoreConfig struct {
	Path       string // Path to the encrypted keystore file.
	Passphrase string // Master passphrase (or from ROBOTICUS_MASTER_KEY env).
}

// OpenKeystore opens or creates an encrypted keystore.
// If the file doesn't exist, an empty keystore is created in memory.
// Call Save() to persist to disk.
func OpenKeystore(cfg KeystoreConfig) (*Keystore, error) {
	passphrase := cfg.Passphrase
	if passphrase == "" {
		passphrase = os.Getenv("ROBOTICUS_MASTER_KEY")
	}
	if passphrase == "" {
		// No master key — create a read-only keystore that only checks env vars.
		return &Keystore{
			path:    cfg.Path,
			secrets: make(map[string]string),
		}, nil
	}

	ks := &Keystore{
		path:    cfg.Path,
		secrets: make(map[string]string),
	}

	// Derive encryption key from passphrase.
	// Use a fixed salt derived from the path for determinism.
	salt := []byte("roboticus-keystore:" + filepath.Base(cfg.Path))
	key, err := scrypt.Key([]byte(passphrase), salt, 32768, 8, 1, 32)
	if err != nil {
		return nil, fmt.Errorf("keystore: key derivation failed: %w", err)
	}
	ks.masterKey = key

	// Try loading existing file.
	if data, err := os.ReadFile(cfg.Path); err == nil && len(data) > 0 {
		if err := ks.decrypt(data); err != nil {
			return nil, fmt.Errorf("keystore: decrypt failed (wrong passphrase?): %w", err)
		}
	}

	return ks, nil
}

// OpenKeystoreMachine opens the keystore using the machine-derived passphrase,
// matching the Rust roboticus unlock_machine behavior. This reads or creates
// a stable machine-id at ~/.roboticus/machine-id and derives the passphrase
// as "roboticus-machine-key:{id}".
//
// If ROBOTICUS_MASTER_KEY is set, it takes precedence over machine-id.
func OpenKeystoreMachine() (*Keystore, error) {
	path := filepath.Join(ConfigDir(), "keystore.enc")
	passphrase := os.Getenv("ROBOTICUS_MASTER_KEY")
	if passphrase == "" {
		passphrase = MachinePassphrase()
	}
	return OpenKeystore(KeystoreConfig{Path: path, Passphrase: passphrase})
}

// MachinePassphrase reads or creates the machine-id and returns the derived
// passphrase in the format "roboticus-machine-key:{hex-id}".
func MachinePassphrase() string {
	idPath := machineIDPath()
	id, err := os.ReadFile(idPath)
	if err == nil {
		trimmed := strings.TrimSpace(string(id))
		if trimmed != "" {
			return "roboticus-machine-key:" + trimmed
		}
	}
	return "roboticus-machine-key:" + createMachineID(idPath)
}

func machineIDPath() string {
	if testDir := os.Getenv("ROBOTICUS_TEST_MACHINE_ID_DIR"); testDir != "" {
		return filepath.Join(testDir, "machine-id")
	}
	return filepath.Join(ConfigDir(), "machine-id")
}

func createMachineID(path string) string {
	var bytes [32]byte
	if _, err := io.ReadFull(rand.Reader, bytes[:]); err != nil {
		return hex.EncodeToString(bytes[:]) // fallback: zero bytes
	}
	id := hex.EncodeToString(bytes[:])
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
	_ = os.WriteFile(path, []byte(id), 0o600)
	return id
}

// Get retrieves a secret by name. If not found in the keystore,
// falls back to checking the environment variable with the same name.
func (ks *Keystore) Get(name string) (string, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	if val, ok := ks.secrets[name]; ok {
		return val, nil
	}

	// Fallback to environment variable.
	if val := os.Getenv(name); val != "" {
		return val, nil
	}

	return "", fmt.Errorf("keystore: %q not found", name)
}

// GetOrEmpty retrieves a secret, returning empty string if not found.
func (ks *Keystore) GetOrEmpty(name string) string {
	val, _ := ks.Get(name)
	return val
}

// Set stores a secret. Call Save() to persist.
func (ks *Keystore) Set(name, value string) error {
	if name == "" {
		return fmt.Errorf("keystore: name cannot be empty")
	}
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.secrets[name] = value
	ks.dirty = true
	return nil
}

// Delete removes a secret. Call Save() to persist.
func (ks *Keystore) Delete(name string) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	if _, ok := ks.secrets[name]; !ok {
		return fmt.Errorf("keystore: %q not found", name)
	}
	delete(ks.secrets, name)
	ks.dirty = true
	return nil
}

// List returns all secret names (sorted).
func (ks *Keystore) List() []string {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	names := make([]string, 0, len(ks.secrets))
	for name := range ks.secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Save encrypts and writes the keystore to disk.
func (ks *Keystore) Save() error {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	if ks.masterKey == nil {
		return fmt.Errorf("keystore: no master key configured (set ROBOTICUS_MASTER_KEY)")
	}
	if ks.path == "" {
		return fmt.Errorf("keystore: no path configured")
	}

	// Serialize secrets to JSON.
	plaintext, err := json.Marshal(ks.secrets)
	if err != nil {
		return fmt.Errorf("keystore: marshal failed: %w", err)
	}

	// Encrypt with AES-256-GCM.
	ciphertext, err := ks.encrypt(plaintext)
	if err != nil {
		return err
	}

	// Ensure directory exists.
	if dir := filepath.Dir(ks.path); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("keystore: mkdir failed: %w", err)
		}
	}

	if err := os.WriteFile(ks.path, ciphertext, 0600); err != nil {
		return fmt.Errorf("keystore: write failed: %w", err)
	}
	ks.dirty = false
	return nil
}

// HasUnsavedChanges returns true if there are changes not yet persisted.
func (ks *Keystore) HasUnsavedChanges() bool {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return ks.dirty
}

// Count returns the number of stored secrets.
func (ks *Keystore) Count() int {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return len(ks.secrets)
}

func (ks *Keystore) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(ks.masterKey)
	if err != nil {
		return nil, fmt.Errorf("keystore: cipher init: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("keystore: GCM init: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("keystore: nonce generation: %w", err)
	}

	// Format: nonce || ciphertext (with auth tag).
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (ks *Keystore) decrypt(data []byte) error {
	block, err := aes.NewCipher(ks.masterKey)
	if err != nil {
		return err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return err
	}

	return json.Unmarshal(plaintext, &ks.secrets)
}

// ResolveSecret looks up a secret name in the keystore with env var fallback.
// This is the primary API for subsystems that need credentials.
func ResolveSecret(ks *Keystore, name string) string {
	if ks != nil {
		if val, err := ks.Get(name); err == nil {
			return val
		}
	}
	return os.Getenv(name)
}
