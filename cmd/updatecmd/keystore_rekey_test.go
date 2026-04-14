package updatecmd

import (
	"os"
	"path/filepath"
	"testing"

	"roboticus/internal/core"
)

func TestKeystoreRekeyCmd_RekeysFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ROBOTICUS_MASTER_KEY", "old-pass")
	t.Setenv("ROBOTICUS_NEW_MASTER_KEY", "new-pass")
	t.Setenv("ROBOTICUS_NEW_MASTER_KEY_CONFIRM", "new-pass")

	path := filepath.Join(home, ".roboticus", "keystore.enc")
	ks, err := core.OpenKeystore(core.KeystoreConfig{Path: path, Passphrase: "old-pass"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ks.Set("provider_key:openai", "sk-test"); err != nil {
		t.Fatal(err)
	}
	if err := ks.Save(); err != nil {
		t.Fatal(err)
	}

	if err := keystoreRekeyCmd.RunE(keystoreRekeyCmd, nil); err != nil {
		t.Fatalf("keystore rekey: %v", err)
	}

	if _, err := core.OpenKeystore(core.KeystoreConfig{Path: path, Passphrase: "old-pass"}); err == nil {
		t.Fatal("expected old passphrase to fail")
	}
	ks2, err := core.OpenKeystore(core.KeystoreConfig{Path: path, Passphrase: "new-pass"})
	if err != nil {
		t.Fatalf("open with new passphrase: %v", err)
	}
	got, err := ks2.Get("provider_key:openai")
	if err != nil {
		t.Fatalf("get after rekey: %v", err)
	}
	if got != "sk-test" {
		t.Fatalf("got %q, want sk-test", got)
	}
}

func TestKeystoreRekeyCmd_RejectsMismatchedConfirmation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ROBOTICUS_MASTER_KEY", "old-pass")
	t.Setenv("ROBOTICUS_NEW_MASTER_KEY", "new-pass")
	t.Setenv("ROBOTICUS_NEW_MASTER_KEY_CONFIRM", "wrong-pass")

	path := filepath.Join(home, ".roboticus", "keystore.enc")
	ks, err := core.OpenKeystore(core.KeystoreConfig{Path: path, Passphrase: "old-pass"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ks.Set("provider_key:openai", "sk-test"); err != nil {
		t.Fatal(err)
	}
	if err := ks.Save(); err != nil {
		t.Fatal(err)
	}

	if err := keystoreRekeyCmd.RunE(keystoreRekeyCmd, nil); err == nil {
		t.Fatal("expected mismatch error")
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("keystore file should still exist: %v", err)
	}
}
