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
func NewID() string {
	b := make([]byte, 16)
	readRandom(b)
	return encodeHex(b)
}
