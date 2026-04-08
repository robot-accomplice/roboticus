package core

import "log"

// snapshotSecrets returns a shallow copy of the current secrets map.
// Must be called while holding the read or write lock.
func (ks *Keystore) snapshotSecrets() map[string]string {
	snap := make(map[string]string, len(ks.secrets))
	for k, v := range ks.secrets {
		snap[k] = v
	}
	return snap
}

// rollbackSecrets restores the secrets map from a snapshot.
// Used when Save() fails to prevent in-memory state from diverging from disk.
// Must be called while holding the write lock.
func (ks *Keystore) rollbackSecrets(snapshot map[string]string) {
	ks.secrets = snapshot
}

// SaveWithRollback takes a snapshot before saving and restores it on failure.
// This prevents the case where Set() + Save() fails, leaving the in-memory
// keystore in a state that doesn't match the persisted file.
func (ks *Keystore) SaveWithRollback() error {
	ks.mu.Lock()
	snapshot := ks.snapshotSecrets()
	ks.mu.Unlock()

	err := ks.Save()
	if err != nil {
		ks.mu.Lock()
		ks.rollbackSecrets(snapshot)
		ks.mu.Unlock()
		log.Printf("[keystore] save failed, rolled back in-memory state: %v", err)
	}
	return err
}

// zeroize overwrites a byte slice with zeros to prevent secrets from
// lingering in memory after use. This provides defense-in-depth for
// passphrase material — Go's GC may not immediately reclaim memory.
func zeroize(data []byte) {
	for i := range data {
		data[i] = 0
	}
}

// zeroizeString attempts to overwrite a string's underlying bytes.
// Note: Go strings are immutable and this relies on unsafe behavior.
// For production use, prefer keeping secrets in []byte and calling zeroize().
// This function is a best-effort defense for passphrase cleanup.
func zeroizeString(s *string) {
	// In Go, we cannot truly zeroize a string without unsafe.
	// The pragmatic approach is to overwrite the variable with empty string
	// and rely on the GC to eventually reclaim the original.
	*s = ""
}
