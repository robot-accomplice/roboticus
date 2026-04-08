package core

import (
	"log"
	"os"
	"time"
)

// keystoreFingerprint tracks file state for change detection.
type keystoreFingerprint struct {
	modTime time.Time
	size    int64
}

// RefreshIfChanged checks if the keystore file has been modified since last
// load (by comparing mtime + size) and reloads if so. Returns true if the
// keystore was refreshed.
//
// This matches the Rust reference's refresh_locked() behavior.
func (ks *Keystore) RefreshIfChanged() bool {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if ks.path == "" || ks.passphrase == "" {
		return false
	}

	info, err := os.Stat(ks.path)
	if err != nil {
		return false
	}

	fp := keystoreFingerprint{
		modTime: info.ModTime(),
		size:    info.Size(),
	}

	// No change since last load
	if fp == ks.fingerprint {
		return false
	}

	data, err := os.ReadFile(ks.path)
	if err != nil || len(data) == 0 {
		return false
	}

	// Attempt decrypt with current passphrase
	oldSecrets := ks.snapshotSecrets()
	if err := ks.decrypt(data); err != nil {
		// Restore on failure
		ks.secrets = oldSecrets
		log.Printf("[keystore] refresh failed (passphrase may have changed): %v", err)
		return false
	}

	ks.fingerprint = fp
	ks.dirty = false
	ks.appendAuditEvent("refresh", "", map[string]interface{}{
		"mtime": fp.modTime.Format(time.RFC3339),
		"size":  fp.size,
	})
	return true
}

// TryLegacyPassphrases attempts to open the keystore using hostname-based
// passphrase variants from before the rebrand. If successful, re-keys the
// keystore to the current machine passphrase.
//
// Legacy formats tried:
//  1. "roboticus-machine-key:{hostname}"
//  2. "dawn-machine-key:{hostname}" (pre-rebrand)
//
// Returns true if a legacy passphrase worked and re-keying succeeded.
func (ks *Keystore) TryLegacyPassphrases() bool {
	if ks.path == "" {
		return false
	}
	data, err := os.ReadFile(ks.path)
	if err != nil || len(data) == 0 {
		return false
	}

	hostname, err := os.Hostname()
	if err != nil {
		return false
	}

	legacyPhrases := []string{
		"roboticus-machine-key:" + hostname,
		"dawn-machine-key:" + hostname,
	}

	for _, phrase := range legacyPhrases {
		testKS := &Keystore{
			passphrase: phrase,
			secrets:    make(map[string]string),
		}
		if err := testKS.decrypt(data); err == nil {
			// Legacy passphrase works — re-key to current machine passphrase
			ks.mu.Lock()
			ks.secrets = testKS.secrets
			currentPhrase := MachinePassphrase()
			ks.passphrase = currentPhrase
			ks.dirty = true
			ks.appendAuditEvent("legacy_migration", "", map[string]interface{}{
				"legacy_format": phrase[:20] + "...",
			})
			ks.mu.Unlock()

			if err := ks.Save(); err != nil {
				log.Printf("[keystore] legacy migration: re-key failed: %v", err)
				return false
			}
			log.Printf("[keystore] migrated from legacy passphrase format")
			return true
		}
	}

	return false
}

// updateFingerprint records the current file state after a successful load or save.
// Must be called while holding the write lock.
func (ks *Keystore) updateFingerprint() {
	if ks.path == "" {
		return
	}
	info, err := os.Stat(ks.path)
	if err != nil {
		return
	}
	ks.fingerprint = keystoreFingerprint{
		modTime: info.ModTime(),
		size:    info.Size(),
	}
}
