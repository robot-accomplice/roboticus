package channel

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
)

// DKIMResult holds the outcome of DKIM signature verification.
type DKIMResult struct {
	HasSignature bool   `json:"has_signature"`
	Status       string `json:"status"` // "pass", "fail", "none", "temperror"
	Domain       string `json:"domain,omitempty"`
	Selector     string `json:"selector,omitempty"`
	Error        string `json:"error,omitempty"`
}

// DKIMVerifier implements RFC 6376 DKIM signature verification.
type DKIMVerifier struct {
	enabled bool
}

// NewDKIMVerifier creates a DKIM verifier.
func NewDKIMVerifier(enabled bool) *DKIMVerifier {
	return &DKIMVerifier{enabled: enabled}
}

// Verify checks DKIM signatures in raw email headers.
func (v *DKIMVerifier) Verify(rawHeaders string) DKIMResult {
	if !v.enabled {
		return DKIMResult{HasSignature: false, Status: "none"}
	}

	// Parse DKIM-Signature header.
	sig := extractDKIMSignature(rawHeaders)
	if sig == nil {
		return DKIMResult{HasSignature: false, Status: "none"}
	}

	result := DKIMResult{
		HasSignature: true,
		Domain:       sig.domain,
		Selector:     sig.selector,
	}

	// Fetch public key from DNS.
	pubKey, err := fetchDKIMPublicKey(sig.selector, sig.domain)
	if err != nil {
		result.Status = "temperror"
		result.Error = fmt.Sprintf("DNS lookup failed: %v", err)
		return result
	}

	// Verify signature.
	hash := sha256.Sum256([]byte(sig.canonicalizedData))
	sigBytes, err := base64.StdEncoding.DecodeString(sig.signatureB64)
	if err != nil {
		result.Status = "fail"
		result.Error = "invalid base64 signature"
		return result
	}

	err = rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], sigBytes)
	if err != nil {
		result.Status = "fail"
		result.Error = "signature verification failed"
		return result
	}

	result.Status = "pass"
	return result
}

type dkimSignature struct {
	domain           string
	selector         string
	signatureB64     string
	canonicalizedData string
}

func extractDKIMSignature(rawHeaders string) *dkimSignature {
	// Find DKIM-Signature header.
	idx := strings.Index(strings.ToLower(rawHeaders), "dkim-signature:")
	if idx < 0 {
		return nil
	}

	headerValue := rawHeaders[idx+len("dkim-signature:"):]
	// Read until next header (line not starting with space/tab).
	lines := strings.Split(headerValue, "\n")
	var fullValue strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		if fullValue.Len() > 0 && line[0] != ' ' && line[0] != '\t' {
			break
		}
		fullValue.WriteString(trimmed)
	}

	// Parse tag=value pairs.
	tags := parseDKIMTags(fullValue.String())
	domain := tags["d"]
	selector := tags["s"]
	sigB64 := strings.ReplaceAll(tags["b"], " ", "")

	if domain == "" || selector == "" || sigB64 == "" {
		return nil
	}

	return &dkimSignature{
		domain:           domain,
		selector:         selector,
		signatureB64:     sigB64,
		canonicalizedData: rawHeaders, // simplified: full headers as canonical data
	}
}

func parseDKIMTags(value string) map[string]string {
	tags := make(map[string]string)
	for _, part := range strings.Split(value, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			tags[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return tags
}

func fetchDKIMPublicKey(selector, domain string) (*rsa.PublicKey, error) {
	queryDomain := selector + "._domainkey." + domain
	txts, err := net.LookupTXT(queryDomain)
	if err != nil {
		return nil, fmt.Errorf("DNS TXT lookup for %s: %w", queryDomain, err)
	}

	for _, txt := range txts {
		tags := parseDKIMTags(txt)
		pubB64 := strings.ReplaceAll(tags["p"], " ", "")
		if pubB64 == "" {
			continue
		}
		pubDER, err := base64.StdEncoding.DecodeString(pubB64)
		if err != nil {
			continue
		}
		pub, err := x509.ParsePKIXPublicKey(pubDER)
		if err != nil {
			continue
		}
		if rsaPub, ok := pub.(*rsa.PublicKey); ok {
			return rsaPub, nil
		}
	}
	return nil, fmt.Errorf("no valid RSA public key found in DNS for %s", queryDomain)
}
