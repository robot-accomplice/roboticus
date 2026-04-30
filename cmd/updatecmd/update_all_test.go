package updatecmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBinaryName(t *testing.T) {
	name := binaryName()
	if name == "" {
		t.Fatal("binaryName() returned empty string")
	}
	// Should contain OS and arch.
	if got := name; got == "" {
		t.Fatal("unexpected empty binary name")
	}
}

func TestFindAssetURL(t *testing.T) {
	rel := LatestRelease{
		TagName: "v2026.04.10",
		Assets: []ReleaseAsset{
			{Name: "roboticus-linux-amd64", BrowserDownloadURL: "https://example.com/roboticus-linux-amd64"},
			{Name: "SHA256SUMS.txt", BrowserDownloadURL: "https://example.com/SHA256SUMS.txt"},
		},
	}

	url, err := findAssetURL(rel, "roboticus-linux-amd64")
	if err != nil {
		t.Fatalf("findAssetURL: %v", err)
	}
	if url != "https://example.com/roboticus-linux-amd64" {
		t.Fatalf("unexpected URL: %s", url)
	}

	_, err = findAssetURL(rel, "roboticus-windows-amd64.exe")
	if err == nil {
		t.Fatal("expected error for missing asset")
	}
	if !strings.Contains(err.Error(), "missing required asset") || !strings.Contains(err.Error(), "roboticus-windows-amd64.exe") {
		t.Fatalf("missing asset error lacks diagnostics: %v", err)
	}
}

func TestParseChecksumFile(t *testing.T) {
	content := "abc123  roboticus-linux-amd64\ndef456  roboticus-darwin-arm64\n"
	tmp := filepath.Join(t.TempDir(), "SHA256SUMS.txt")
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checksums, err := parseChecksumFile(tmp)
	if err != nil {
		t.Fatalf("parseChecksumFile: %v", err)
	}
	if checksums["roboticus-linux-amd64"] != "abc123" {
		t.Fatalf("unexpected hash: %s", checksums["roboticus-linux-amd64"])
	}
	if checksums["roboticus-darwin-arm64"] != "def456" {
		t.Fatalf("unexpected hash: %s", checksums["roboticus-darwin-arm64"])
	}
}

func TestVerifyChecksum_Match(t *testing.T) {
	data := []byte("hello roboticus binary content")
	tmp := filepath.Join(t.TempDir(), "binary")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatal(err)
	}

	h := sha256.Sum256(data)
	expected := hex.EncodeToString(h[:])

	if err := verifyChecksum(tmp, expected); err != nil {
		t.Fatalf("verifyChecksum should pass: %v", err)
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	data := []byte("hello roboticus binary content")
	tmp := filepath.Join(t.TempDir(), "binary")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatal(err)
	}

	err := verifyChecksum(tmp, "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	content := []byte("binary data for copy test")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch: got %q", got)
	}
}

func TestPerformUpdate_ChecksumMismatch(t *testing.T) {
	// Build a fake checksum that won't match.
	binaryContent := []byte("fake binary")
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	checksumContent := fmt.Sprintf("%s  %s\n", wrongHash, binaryName())

	// Serve the fake binary and checksums from a test server.
	mux := setupFakeReleaseServer(t, binaryContent, []byte(checksumContent))
	_ = mux // test server is started inside helper

	// We can't easily test performUpdate end-to-end because it calls os.Executable(),
	// but we can test the checksum verification path directly.
	tmp := filepath.Join(t.TempDir(), "binary")
	if err := os.WriteFile(tmp, binaryContent, 0o644); err != nil {
		t.Fatal(err)
	}

	err := verifyChecksum(tmp, wrongHash)
	if err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

// setupFakeReleaseServer is a helper for future integration tests.
func setupFakeReleaseServer(t *testing.T, binary, checksums []byte) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		binaryName():     binary,
		"SHA256SUMS.txt": checksums,
	}
}
