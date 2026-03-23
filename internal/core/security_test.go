package core

import (
	"testing"
)

func TestHMAC(t *testing.T) {
	key := []byte("test-secret-key")
	data := []byte("hello world")

	tag := ComputeHMAC(key, data)
	if tag == "" {
		t.Fatal("HMAC tag should not be empty")
	}

	if !VerifyHMAC(key, data, tag) {
		t.Error("HMAC verification should pass with correct key and data")
	}

	if VerifyHMAC(key, []byte("different data"), tag) {
		t.Error("HMAC verification should fail with different data")
	}

	if VerifyHMAC([]byte("wrong-key"), data, tag) {
		t.Error("HMAC verification should fail with wrong key")
	}
}

func TestIsPathAllowed(t *testing.T) {
	workspace := "/home/agent/workspace"

	tests := []struct {
		path    string
		allowed []string
		want    bool
	}{
		{"/home/agent/workspace/file.txt", nil, true},
		{"/home/agent/workspace/sub/dir/file.txt", nil, true},
		{"/etc/passwd", nil, false},
		{"/home/agent/../etc/passwd", nil, false}, // traversal blocked
		{"/opt/data/file.txt", []string{"/opt/data"}, true},
		{"/opt/other/file.txt", []string{"/opt/data"}, false},
	}

	for _, tt := range tests {
		got := IsPathAllowed(tt.path, workspace, tt.allowed)
		if got != tt.want {
			t.Errorf("IsPathAllowed(%q, %q, %v) = %v, want %v", tt.path, workspace, tt.allowed, got, tt.want)
		}
	}
}

func TestHashSHA256(t *testing.T) {
	hash := HashSHA256([]byte("hello"))
	// SHA-256 of "hello" is well-known
	expected := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if hash != expected {
		t.Errorf("HashSHA256(\"hello\") = %q, want %q", hash, expected)
	}
}
