package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeystore_AuditLog_SetGetDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.enc")

	ks, err := OpenKeystore(KeystoreConfig{
		Path:       path,
		Passphrase: "test-passphrase",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Initially empty audit log
	if len(ks.AuditLog()) != 0 {
		t.Error("expected empty audit log initially")
	}

	// Set triggers audit event
	if err := ks.Set("key1", "value1"); err != nil {
		t.Fatal(err)
	}
	events := ks.AuditLog()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event after Set, got %d", len(events))
	}
	if events[0].Op != "set" {
		t.Errorf("expected op='set', got %q", events[0].Op)
	}
	if events[0].Key != "key1" {
		t.Errorf("expected key='key1', got %q", events[0].Key)
	}

	// Overwrite triggers second audit event
	if err := ks.Set("key1", "value2"); err != nil {
		t.Fatal(err)
	}
	events = ks.AuditLog()
	if len(events) != 2 {
		t.Fatalf("expected 2 audit events after overwrite, got %d", len(events))
	}

	// Delete triggers audit event
	if err := ks.Delete("key1"); err != nil {
		t.Fatal(err)
	}
	events = ks.AuditLog()
	if len(events) != 3 {
		t.Fatalf("expected 3 audit events after Delete, got %d", len(events))
	}
	if events[2].Op != "delete" {
		t.Errorf("expected op='delete', got %q", events[2].Op)
	}
}

func TestKeystore_AuditLog_IsACopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.enc")

	ks, err := OpenKeystore(KeystoreConfig{
		Path:       path,
		Passphrase: "test-passphrase",
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = ks.Set("key1", "value1")
	events1 := ks.AuditLog()
	_ = ks.Set("key2", "value2")
	events2 := ks.AuditLog()

	// The first slice should not have been modified
	if len(events1) != 1 {
		t.Errorf("first snapshot should still have 1 event, got %d", len(events1))
	}
	if len(events2) != 2 {
		t.Errorf("second snapshot should have 2 events, got %d", len(events2))
	}
}

func TestKeystore_AuditLog_NilOnFresh(t *testing.T) {
	ks := &Keystore{secrets: make(map[string]string)}
	events := ks.AuditLog()
	if events != nil {
		t.Errorf("expected nil audit log on fresh keystore, got %v", events)
	}
}

func TestKeystore_Zeroize(t *testing.T) {
	data := []byte("sensitive-data-here")
	zeroize(data)
	for i, b := range data {
		if b != 0 {
			t.Errorf("byte %d not zeroed: got %d", i, b)
		}
	}
}

func TestKeystore_SaveWithRollback_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.enc")

	ks, err := OpenKeystore(KeystoreConfig{
		Path:       path,
		Passphrase: "test-passphrase",
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = ks.Set("key1", "value1")
	if err := ks.SaveWithRollback(); err != nil {
		t.Fatalf("SaveWithRollback failed: %v", err)
	}

	// Verify file was written
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected keystore file to exist after save")
	}
}

func TestKeystore_SaveWithRollback_FailureRollsBack(t *testing.T) {
	// Use a path that will fail to write (directory doesn't allow creation)
	ks := &Keystore{
		path:       "/nonexistent-root-dir/impossible/keystore.enc",
		passphrase: "test-passphrase",
		secrets:    map[string]string{"key1": "value1"},
		dirty:      true,
	}

	originalCount := len(ks.secrets)
	err := ks.SaveWithRollback()
	if err == nil {
		t.Fatal("expected SaveWithRollback to fail on bad path")
	}

	// Secrets should still be intact after rollback
	if len(ks.secrets) != originalCount {
		t.Errorf("expected %d secrets after rollback, got %d", originalCount, len(ks.secrets))
	}
	if ks.secrets["key1"] != "value1" {
		t.Error("expected key1=value1 after rollback")
	}
}
