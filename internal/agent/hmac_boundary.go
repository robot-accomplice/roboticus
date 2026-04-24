package agent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	boundaryPrefix = "<<<TRUST_BOUNDARY:"
	boundarySuffix = ">>>"
)

// TagContent wraps content with HMAC-verified trust boundaries.
// The boundaries allow downstream processing to verify that the content
// was produced by a trusted source (the agent) and not injected.
func TagContent(content string, secret []byte) string {
	tag := computeHMAC(content, secret)
	return boundaryPrefix + tag + boundarySuffix + "\n" +
		content + "\n" +
		boundaryPrefix + tag + boundarySuffix
}

// VerifyHMACBoundary checks if content has valid HMAC trust boundaries.
// Returns the inner content and true if valid, or the original content and false if not.
func VerifyHMACBoundary(tagged string, secret []byte) (string, bool) {
	lines := strings.Split(tagged, "\n")
	if len(lines) < 3 {
		return tagged, false
	}

	// Extract tags from first and last lines.
	firstTag := extractTag(lines[0])
	lastTag := extractTag(lines[len(lines)-1])
	if firstTag == "" || lastTag == "" || firstTag != lastTag {
		return tagged, false
	}

	// Reconstruct inner content.
	inner := strings.Join(lines[1:len(lines)-1], "\n")

	// Verify HMAC.
	expected := computeHMAC(inner, secret)
	if !hmac.Equal([]byte(firstTag), []byte(expected)) {
		return tagged, false
	}

	return inner, true
}

// StripHMACBoundaries removes trust boundary lines from content.
// Used when verification fails (attacker-forged boundaries).
func StripHMACBoundaries(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), boundaryPrefix) {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

// SanitizeModelOutput applies the Rust-parity output sanitization pipeline:
// 1. Verify HMAC boundaries (if present)
// 2. Strip forged boundaries
// 3. L4 injection scan
func SanitizeModelOutput(content string, secret []byte, injection *InjectionDetector) string {
	// Step 1: HMAC boundary verification.
	if strings.Contains(content, boundaryPrefix) {
		inner, valid := VerifyHMACBoundary(content, secret)
		if valid {
			content = inner
		} else {
			// Forged boundaries — strip them.
			content = StripHMACBoundaries(content)
		}
	}

	// Step 2: L4 injection scan.
	if injection != nil {
		score := injection.ScanOutput(content)
		if score.IsBlocked() {
			return ""
		}
	}

	return content
}

func computeHMAC(data string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func extractTag(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, boundaryPrefix) || !strings.HasSuffix(line, boundarySuffix) {
		return ""
	}
	// Extract hex tag between prefix and suffix.
	tag := line[len(boundaryPrefix) : len(line)-len(boundarySuffix)]
	// Validate it's hex.
	if _, err := hex.DecodeString(tag); err != nil {
		return ""
	}
	return tag
}
