package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeystore_SetGetDelete(t *testing.T) {
	ks, err := OpenKeystore(KeystoreConfig{Passphrase: "test-pass"})
	if err != nil {
		t.Fatal(err)
	}

	// Set and get.
	_ = ks.Set("api_key", "sk-test-123")
	val, err := ks.Get("api_key")
	if err != nil {
		t.Fatal(err)
	}
	if val != "sk-test-123" {
		t.Errorf("got %q, want %q", val, "sk-test-123")
	}

	// List.
	names := ks.List()
	if len(names) != 1 || names[0] != "api_key" {
		t.Errorf("list = %v, want [api_key]", names)
	}

	// Delete.
	if err := ks.Delete("api_key"); err != nil {
		t.Fatal(err)
	}
	_, err = ks.Get("api_key")
	if err == nil {
		t.Error("expected error after delete")
	}

	// Delete nonexistent.
	if err := ks.Delete("nope"); err == nil {
		t.Error("expected error for nonexistent key")
	}
}

func TestKeystore_EnvFallback(t *testing.T) {
	ks, _ := OpenKeystore(KeystoreConfig{})

	_ = os.Setenv("TEST_ROBOTICUS_KEY", "from-env")
	defer func() { _ = os.Unsetenv("TEST_ROBOTICUS_KEY") }()

	val, err := ks.Get("TEST_ROBOTICUS_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if val != "from-env" {
		t.Errorf("got %q, want %q", val, "from-env")
	}
}

func TestKeystore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.enc")

	// Create and save.
	ks1, err := OpenKeystore(KeystoreConfig{Path: path, Passphrase: "master-key"})
	if err != nil {
		t.Fatal(err)
	}
	_ = ks1.Set("secret1", "value1")
	_ = ks1.Set("secret2", "value2")
	if err := ks1.Save(); err != nil {
		t.Fatal(err)
	}

	// File should exist.
	if _, err := os.Stat(path); err != nil {
		t.Fatal("keystore file not created")
	}

	// Reload with same passphrase.
	ks2, err := OpenKeystore(KeystoreConfig{Path: path, Passphrase: "master-key"})
	if err != nil {
		t.Fatal(err)
	}
	val, err := ks2.Get("secret1")
	if err != nil || val != "value1" {
		t.Errorf("reload: got %q, want %q", val, "value1")
	}
	if ks2.Count() != 2 {
		t.Errorf("reload: count = %d, want 2", ks2.Count())
	}

	// Wrong passphrase should fail.
	_, err = OpenKeystore(KeystoreConfig{Path: path, Passphrase: "wrong"})
	if err == nil {
		t.Error("expected error with wrong passphrase")
	}
}

func TestKeystore_EmptyName(t *testing.T) {
	ks, _ := OpenKeystore(KeystoreConfig{Passphrase: "test"})
	if err := ks.Set("", "value"); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestKeystore_SaveWithoutMasterKey(t *testing.T) {
	ks, _ := OpenKeystore(KeystoreConfig{})
	_ = ks.Set("key", "val")
	if err := ks.Save(); err == nil {
		t.Error("expected error when saving without master key")
	}
}

func TestKeystore_HasUnsavedChanges(t *testing.T) {
	ks, _ := OpenKeystore(KeystoreConfig{Passphrase: "test"})
	if ks.HasUnsavedChanges() {
		t.Error("fresh keystore should not have unsaved changes")
	}
	_ = ks.Set("key", "val")
	if !ks.HasUnsavedChanges() {
		t.Error("should have unsaved changes after Set")
	}
}

func TestResolveSecret(t *testing.T) {
	ks, _ := OpenKeystore(KeystoreConfig{Passphrase: "test"})
	_ = ks.Set("MY_KEY", "from-keystore")

	_ = os.Setenv("MY_OTHER_KEY", "from-env")
	defer func() { _ = os.Unsetenv("MY_OTHER_KEY") }()

	// Keystore takes precedence.
	if got := ResolveSecret(ks, "MY_KEY"); got != "from-keystore" {
		t.Errorf("got %q, want from-keystore", got)
	}

	// Falls back to env.
	if got := ResolveSecret(ks, "MY_OTHER_KEY"); got != "from-env" {
		t.Errorf("got %q, want from-env", got)
	}

	// Nil keystore falls back to env.
	if got := ResolveSecret(nil, "MY_OTHER_KEY"); got != "from-env" {
		t.Errorf("got %q, want from-env", got)
	}
}

func TestKeystore_GetOrEmpty(t *testing.T) {
	ks, _ := OpenKeystore(KeystoreConfig{Passphrase: "test"})
	if got := ks.GetOrEmpty("nonexistent"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	_ = ks.Set("key", "val")
	if got := ks.GetOrEmpty("key"); got != "val" {
		t.Errorf("got %q, want val", got)
	}
}
