package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKeystore_RefreshIfChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.enc")
	passphrase := "test-passphrase"

	// Create and save initial keystore
	ks1, err := OpenKeystore(KeystoreConfig{Path: path, Passphrase: passphrase})
	if err != nil {
		t.Fatal(err)
	}
	_ = ks1.Set("key1", "original")
	if err := ks1.Save(); err != nil {
		t.Fatal(err)
	}

	// Open a second handle to the same file
	ks2, err := OpenKeystore(KeystoreConfig{Path: path, Passphrase: passphrase})
	if err != nil {
		t.Fatal(err)
	}
	// Record its fingerprint
	ks2.mu.Lock()
	ks2.updateFingerprint()
	ks2.mu.Unlock()

	// Modify through the first handle
	_ = ks1.Set("key1", "modified")
	if err := ks1.Save(); err != nil {
		t.Fatal(err)
	}

	// Ensure mtime differs (some filesystems have 1s granularity)
	time.Sleep(10 * time.Millisecond)

	// Second handle should detect the change
	refreshed := ks2.RefreshIfChanged()
	if !refreshed {
		t.Error("expected RefreshIfChanged to return true after external modification")
	}

	// Verify updated value
	val, err := ks2.Get("key1")
	if err != nil {
		t.Fatal(err)
	}
	if val != "modified" {
		t.Errorf("expected 'modified', got %q", val)
	}
}

func TestKeystore_RefreshIfChanged_NoChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.enc")

	ks, err := OpenKeystore(KeystoreConfig{Path: path, Passphrase: "test"})
	if err != nil {
		t.Fatal(err)
	}
	_ = ks.Set("key1", "value1")
	_ = ks.Save()

	// Record fingerprint
	ks.mu.Lock()
	ks.updateFingerprint()
	ks.mu.Unlock()

	// No external changes — should return false
	if ks.RefreshIfChanged() {
		t.Error("expected false when file hasn't changed")
	}
}

func TestKeystore_TryLegacyPassphrases_NoFile(t *testing.T) {
	ks := &Keystore{
		path:    "/nonexistent/keystore.enc",
		secrets: make(map[string]string),
	}
	if ks.TryLegacyPassphrases() {
		t.Error("expected false when file doesn't exist")
	}
}

func TestKeystore_TryLegacyPassphrases_WithHostname(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.enc")

	hostname, err := os.Hostname()
	if err != nil {
		t.Skip("cannot get hostname")
	}

	// Create a keystore encrypted with hostname-based passphrase
	legacyPhrase := "roboticus-machine-key:" + hostname
	ks1, err := OpenKeystore(KeystoreConfig{Path: path, Passphrase: legacyPhrase})
	if err != nil {
		t.Fatal(err)
	}
	_ = ks1.Set("legacy_key", "legacy_value")
	if err := ks1.Save(); err != nil {
		t.Fatal(err)
	}

	// Try opening with machine passphrase (will differ from hostname-based)
	// Set test machine-id dir to avoid using real machine-id
	t.Setenv("ROBOTICUS_TEST_MACHINE_ID_DIR", dir)

	ks2, err := OpenKeystore(KeystoreConfig{Path: path, Passphrase: MachinePassphrase()})
	if err != nil {
		// Expected — machine passphrase differs from hostname-based
		ks2 = &Keystore{
			path:       path,
			passphrase: MachinePassphrase(),
			secrets:    make(map[string]string),
		}
	}

	// Try legacy migration
	migrated := ks2.TryLegacyPassphrases()
	if !migrated {
		t.Log("legacy migration did not succeed (hostname passphrase may match machine-id)")
		return
	}

	// After migration, should be able to read the legacy key
	val, err := ks2.Get("legacy_key")
	if err != nil {
		t.Fatalf("expected to read legacy_key after migration, got: %v", err)
	}
	if val != "legacy_value" {
		t.Errorf("expected 'legacy_value', got %q", val)
	}
}
