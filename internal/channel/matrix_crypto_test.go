package channel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewMatrixCrypto_KeyGeneration(t *testing.T) {
	mc := NewMatrixCrypto()

	if len(mc.Curve25519Private) != 32 {
		t.Fatalf("curve25519 private key length = %d, want 32", len(mc.Curve25519Private))
	}
	if len(mc.Curve25519Public) != 32 {
		t.Fatalf("curve25519 public key length = %d, want 32", len(mc.Curve25519Public))
	}
	if len(mc.Ed25519Private) != 64 {
		t.Fatalf("ed25519 private key length = %d, want 64", len(mc.Ed25519Private))
	}
	if len(mc.Ed25519Public) != 32 {
		t.Fatalf("ed25519 public key length = %d, want 32", len(mc.Ed25519Public))
	}

	// Keys should not be all zeros.
	allZero := true
	for _, b := range mc.Curve25519Public {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("curve25519 public key is all zeros")
	}
}

func TestDeviceKeysJSON(t *testing.T) {
	mc := NewMatrixCrypto()
	dkj := mc.DeviceKeysJSON()

	algos, ok := dkj["algorithms"].([]string)
	if !ok || len(algos) != 2 {
		t.Fatalf("algorithms = %v", dkj["algorithms"])
	}
	if algos[0] != "m.olm.v1.curve25519-aes-sha2" {
		t.Errorf("algo[0] = %s", algos[0])
	}

	keys, ok := dkj["keys"].(map[string]string)
	if !ok {
		t.Fatal("keys is not map[string]string")
	}
	if keys["curve25519:DEVICE"] == "" {
		t.Error("missing curve25519 device key")
	}
	if keys["ed25519:DEVICE"] == "" {
		t.Error("missing ed25519 device key")
	}
}

func TestGenerateOneTimeKeys(t *testing.T) {
	mc := NewMatrixCrypto()
	mc.GenerateOneTimeKeys(5)

	if len(mc.OneTimeKeys) != 5 {
		t.Errorf("generated %d one-time keys, want 5", len(mc.OneTimeKeys))
	}

	// Each key should be 32 bytes.
	for keyID, pub := range mc.OneTimeKeys {
		if len(pub) != 32 {
			t.Errorf("one-time key %s has length %d, want 32", keyID, len(pub))
		}
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	mc := NewMatrixCrypto()
	roomID := "!test:example.com"

	if err := mc.CreateOutboundSession(roomID); err != nil {
		t.Fatalf("create outbound session: %v", err)
	}

	plaintext := "Hello, encrypted Matrix world!"
	encrypted, err := mc.EncryptMessage(roomID, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Verify the encrypted output has the expected structure.
	if encrypted["algorithm"] != "m.megolm.v1.aes-sha2" {
		t.Errorf("algorithm = %v", encrypted["algorithm"])
	}
	if encrypted["session_id"] == "" {
		t.Error("missing session_id")
	}
	if encrypted["ciphertext"] == "" {
		t.Error("missing ciphertext")
	}

	// Create a fresh MatrixCrypto with the same inbound session for decryption.
	// In real usage, the session key would be shared via Olm.
	// Here we test by using the same MatrixCrypto instance which has both sessions.
	mc2 := NewMatrixCrypto()
	mc2.InboundSessions[roomID] = &megolmSession{
		SessionID:    mc.OutboundSessions[roomID].SessionID,
		RatchetKey:   mc.InboundSessions[roomID].RatchetKey,
		MessageIndex: 0,
	}

	// But we need the inbound session before the encrypt ratcheted it.
	// Since we already encrypted, the inbound session advanced too.
	// Let's just use the original mc which has synced inbound state.
	// Re-create a fresh round trip.
	mc3 := NewMatrixCrypto()
	if err := mc3.CreateOutboundSession(roomID); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Capture inbound session state before encryption.
	inboundCopy := &megolmSession{
		SessionID:    mc3.InboundSessions[roomID].SessionID,
		RatchetKey:   append([]byte(nil), mc3.InboundSessions[roomID].RatchetKey...),
		MessageIndex: mc3.InboundSessions[roomID].MessageIndex,
	}

	enc, err := mc3.EncryptMessage(roomID, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Use captured inbound session for decryption on a different instance.
	decryptor := NewMatrixCrypto()
	decryptor.InboundSessions[roomID] = inboundCopy

	decrypted, err := decryptor.DecryptEvent(roomID, enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDecryptMultipleMessages(t *testing.T) {
	mc := NewMatrixCrypto()
	roomID := "!multi:example.com"

	if err := mc.CreateOutboundSession(roomID); err != nil {
		t.Fatalf("create session: %v", err)
	}

	messages := []string{"first", "second", "third"}

	// Capture initial inbound state.
	inbound := &megolmSession{
		SessionID:    mc.InboundSessions[roomID].SessionID,
		RatchetKey:   append([]byte(nil), mc.InboundSessions[roomID].RatchetKey...),
		MessageIndex: 0,
	}

	var encrypted []map[string]any
	for _, msg := range messages {
		enc, err := mc.EncryptMessage(roomID, msg)
		if err != nil {
			t.Fatalf("encrypt %q: %v", msg, err)
		}
		encrypted = append(encrypted, enc)
	}

	// Decrypt in order using the captured inbound session.
	decryptor := NewMatrixCrypto()
	decryptor.InboundSessions[roomID] = inbound

	for i, enc := range encrypted {
		decrypted, err := decryptor.DecryptEvent(roomID, enc)
		if err != nil {
			t.Fatalf("decrypt message %d: %v", i, err)
		}
		if decrypted != messages[i] {
			t.Errorf("message %d: got %q, want %q", i, decrypted, messages[i])
		}
	}
}

func TestSessionRotationAtThreshold(t *testing.T) {
	mc := NewMatrixCrypto()
	mc.RotationThreshold = 3
	roomID := "!rotate:example.com"

	if err := mc.CreateOutboundSession(roomID); err != nil {
		t.Fatalf("create session: %v", err)
	}

	originalSessionID := mc.OutboundSessions[roomID].SessionID

	// Send 3 messages (threshold), session should remain the same.
	for i := 0; i < 3; i++ {
		_, err := mc.EncryptMessage(roomID, "msg")
		if err != nil {
			t.Fatalf("encrypt %d: %v", i, err)
		}
	}

	// The 4th encrypt should trigger rotation (because message_index == 3 >= threshold 3).
	_, err := mc.EncryptMessage(roomID, "after rotation")
	if err != nil {
		t.Fatalf("encrypt after rotation: %v", err)
	}

	newSessionID := mc.OutboundSessions[roomID].SessionID
	if newSessionID == originalSessionID {
		t.Error("session was not rotated after threshold")
	}

	// Message index on the new session should be 1 (just sent one message on it).
	if mc.OutboundSessions[roomID].MessageIndex != 1 {
		t.Errorf("message index after rotation = %d, want 1", mc.OutboundSessions[roomID].MessageIndex)
	}
}

func TestPersistAndLoad(t *testing.T) {
	mc := NewMatrixCrypto()
	mc.GenerateOneTimeKeys(3)

	roomID := "!persist:example.com"
	if err := mc.CreateOutboundSession(roomID); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Encrypt a message to advance state.
	_, err := mc.EncryptMessage(roomID, "test persist")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Persist to temp file.
	dir := t.TempDir()
	path := filepath.Join(dir, "crypto_state.json")
	if err := mc.Persist(path); err != nil {
		t.Fatalf("persist: %v", err)
	}

	// Verify file permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file permissions = %o, want 600", info.Mode().Perm())
	}

	// Load it back.
	loaded, err := LoadCrypto(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Verify keys match.
	if string(loaded.Curve25519Public) != string(mc.Curve25519Public) {
		t.Error("curve25519 public key mismatch after load")
	}
	if string(loaded.Ed25519Public) != string(mc.Ed25519Public) {
		t.Error("ed25519 public key mismatch after load")
	}
	if len(loaded.OneTimeKeys) != 3 {
		t.Errorf("loaded %d one-time keys, want 3", len(loaded.OneTimeKeys))
	}

	// Verify outbound session state.
	outbound, ok := loaded.OutboundSessions[roomID]
	if !ok {
		t.Fatal("outbound session not loaded")
	}
	if outbound.MessageIndex != mc.OutboundSessions[roomID].MessageIndex {
		t.Errorf("message index = %d, want %d", outbound.MessageIndex, mc.OutboundSessions[roomID].MessageIndex)
	}
}

func TestDecryptEvent_NoSession(t *testing.T) {
	mc := NewMatrixCrypto()
	_, err := mc.DecryptEvent("!noroom:example.com", map[string]any{
		"session_id": "abc",
		"ciphertext": "data",
	})
	if err == nil {
		t.Error("expected error for missing inbound session")
	}
}

func TestDecryptEvent_SessionIDMismatch(t *testing.T) {
	mc := NewMatrixCrypto()
	roomID := "!mismatch:example.com"
	if err := mc.CreateOutboundSession(roomID); err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err := mc.DecryptEvent(roomID, map[string]any{
		"session_id":    "wrong-session",
		"ciphertext":    "data",
		"message_index": float64(0),
	})
	if err == nil {
		t.Error("expected error for session ID mismatch")
	}
}

func TestAESGCMRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	plaintext := []byte("hello world encryption test")
	ct, err := aesGCMEncrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	pt, err := aesGCMDecrypt(key, ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(pt) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", pt, plaintext)
	}
}

func TestLoadCrypto_FileNotFound(t *testing.T) {
	_, err := LoadCrypto("/nonexistent/path/crypto.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}
