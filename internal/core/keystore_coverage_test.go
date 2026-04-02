package core

import (
	"path/filepath"
	"testing"
)

func TestKeystore_SetAndGet(t *testing.T) {
	dir := t.TempDir()
	ks, err := OpenKeystore(KeystoreConfig{
		Path:       filepath.Join(dir, "test.keys"),
		Passphrase: "testpass",
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	err = ks.Set("api_key", "sk-123456")
	if err != nil {
		t.Fatalf("set: %v", err)
	}

	val, err := ks.Get("api_key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != "sk-123456" {
		t.Errorf("val = %q", val)
	}
}

func TestKeystore_GetMissing(t *testing.T) {
	dir := t.TempDir()
	ks, _ := OpenKeystore(KeystoreConfig{
		Path:       filepath.Join(dir, "test.keys"),
		Passphrase: "testpass",
	})

	_, err := ks.Get("nonexistent")
	if err == nil {
		t.Error("should error for missing key")
	}
}

func TestKeystore_GetOrEmpty_Coverage(t *testing.T) {
	dir := t.TempDir()
	ks, _ := OpenKeystore(KeystoreConfig{
		Path:       filepath.Join(dir, "test.keys"),
		Passphrase: "pass",
	})

	val := ks.GetOrEmpty("missing")
	if val != "" {
		t.Errorf("should be empty for missing key, got %q", val)
	}

	_ = ks.Set("exists", "hello")
	val = ks.GetOrEmpty("exists")
	if val != "hello" {
		t.Errorf("val = %q", val)
	}
}

func TestKeystore_MultipleKeys(t *testing.T) {
	dir := t.TempDir()
	ks, _ := OpenKeystore(KeystoreConfig{
		Path:       filepath.Join(dir, "multi.keys"),
		Passphrase: "pass",
	})
	_ = ks.Set("key1", "val1")
	_ = ks.Set("key2", "val2")

	v1, _ := ks.Get("key1")
	v2, _ := ks.Get("key2")
	if v1 != "val1" || v2 != "val2" {
		t.Errorf("v1=%s v2=%s", v1, v2)
	}
}
