package db

import (
	"crypto/rand"
	"encoding/hex"
)

// readRandom fills b with cryptographically secure random bytes.
func readRandom(b []byte) (int, error) {
	return rand.Read(b)
}

// encodeHex encodes bytes as a lowercase hex string.
func encodeHex(b []byte) string {
	return hex.EncodeToString(b)
}

// NewID generates a cryptographically random 16-byte hex ID.
// Panics if the system CSPRNG is unavailable — this should never happen
// on a healthy system and indicates a critical failure.
func NewID() string {
	b := make([]byte, 16)
	if _, err := readRandom(b); err != nil {
		panic("db.NewID: crypto/rand failed: " + err.Error())
	}
	return encodeHex(b)
}
