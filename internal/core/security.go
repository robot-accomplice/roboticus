package core

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// ComputeHMAC generates an HMAC-SHA256 tag for trust boundary marking (L2 injection defense).
func ComputeHMAC(key []byte, data []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyHMAC checks an HMAC-SHA256 tag.
func VerifyHMAC(key []byte, data []byte, tag string) bool {
	expected := ComputeHMAC(key, data)
	return hmac.Equal([]byte(expected), []byte(tag))
}

// IsPathAllowed checks if a path is within the allowed workspace boundaries.
func IsPathAllowed(path string, workspace string, allowedPaths []string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	// Block path traversal attempts.
	if strings.Contains(path, "..") {
		return false
	}

	// Check workspace containment.
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return false
	}
	if absPath == absWorkspace || strings.HasPrefix(absPath, absWorkspace+string(filepath.Separator)) {
		return true
	}

	// Check additional allowed paths.
	for _, allowed := range allowedPaths {
		absAllowed, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if absPath == absAllowed || strings.HasPrefix(absPath, absAllowed+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

// HashSHA256 returns the hex-encoded SHA-256 hash of data.
func HashSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
