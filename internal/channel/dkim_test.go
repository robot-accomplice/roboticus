package channel

import "testing"

func TestExtractDKIMSignature_Present(t *testing.T) {
	raw := "From: test@example.com\r\n" +
		"DKIM-Signature: v=1; a=rsa-sha256; d=example.com; s=selector1; b=abc123base64\r\n" +
		"Subject: Test\r\n"

	sig := extractDKIMSignature(raw)
	if sig == nil {
		t.Fatal("should extract DKIM signature")
	}
	if sig.domain != "example.com" {
		t.Errorf("domain = %s, want example.com", sig.domain)
	}
	if sig.selector != "selector1" {
		t.Errorf("selector = %s, want selector1", sig.selector)
	}
	if sig.signatureB64 != "abc123base64" {
		t.Errorf("signatureB64 = %s", sig.signatureB64)
	}
}

func TestExtractDKIMSignature_Absent(t *testing.T) {
	raw := "From: test@example.com\r\nSubject: Test\r\n"
	sig := extractDKIMSignature(raw)
	if sig != nil {
		t.Error("should return nil when no DKIM-Signature header")
	}
}

func TestParseDKIMTags(t *testing.T) {
	tags := parseDKIMTags("v=1; a=rsa-sha256; d=example.com; s=sel; b=sig")
	if tags["v"] != "1" {
		t.Errorf("v = %s", tags["v"])
	}
	if tags["d"] != "example.com" {
		t.Errorf("d = %s", tags["d"])
	}
	if tags["s"] != "sel" {
		t.Errorf("s = %s", tags["s"])
	}
	if tags["b"] != "sig" {
		t.Errorf("b = %s", tags["b"])
	}
}

func TestDKIMVerifier_Disabled(t *testing.T) {
	v := NewDKIMVerifier(false)
	result := v.Verify("DKIM-Signature: v=1; d=example.com; s=sel; b=sig")
	if result.HasSignature {
		t.Error("disabled verifier should report no signature")
	}
	if result.Status != "none" {
		t.Errorf("status = %s, want none", result.Status)
	}
}

func TestDKIMVerifier_NoSignature(t *testing.T) {
	v := NewDKIMVerifier(true)
	result := v.Verify("From: test@example.com\r\nSubject: Test")
	if result.HasSignature {
		t.Error("should report no signature")
	}
}
