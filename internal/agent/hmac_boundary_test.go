package agent

import (
	"testing"
)

func TestTagAndVerify(t *testing.T) {
	secret := []byte("test-secret-key-for-hmac")
	content := "Hello, this is trusted content."

	tagged := TagContent(content, secret)

	// Should contain the boundary markers.
	if len(tagged) <= len(content) {
		t.Fatal("tagged content should be longer than original")
	}

	// Verify should succeed.
	inner, ok := VerifyHMACBoundary(tagged, secret)
	if !ok {
		t.Fatal("verification should succeed for properly tagged content")
	}
	if inner != content {
		t.Errorf("inner = %q, want %q", inner, content)
	}
}

func TestVerify_WrongSecret(t *testing.T) {
	secret := []byte("correct-secret")
	wrongSecret := []byte("wrong-secret")
	content := "Sensitive data."

	tagged := TagContent(content, secret)
	_, ok := VerifyHMACBoundary(tagged, wrongSecret)
	if ok {
		t.Fatal("verification should fail with wrong secret")
	}
}

func TestVerify_TamperedContent(t *testing.T) {
	secret := []byte("test-secret")
	tagged := TagContent("original", secret)

	// Tamper with the content.
	tampered := tagged[:50] + "INJECTED" + tagged[50:]
	_, ok := VerifyHMACBoundary(tampered, secret)
	if ok {
		t.Fatal("verification should fail with tampered content")
	}
}

func TestStripHMACBoundaries(t *testing.T) {
	tagged := "<<<TRUST_BOUNDARY:abc123>>>\nContent here\n<<<TRUST_BOUNDARY:abc123>>>"
	stripped := StripHMACBoundaries(tagged)
	if stripped != "Content here" {
		t.Errorf("stripped = %q, want %q", stripped, "Content here")
	}
}

func TestVerify_NoBoundaries(t *testing.T) {
	_, ok := VerifyHMACBoundary("plain text without boundaries", []byte("secret"))
	if ok {
		t.Fatal("should return false for content without boundaries")
	}
}
