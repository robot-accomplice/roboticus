package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCheckForUpdate_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	origURL := updateCheckURL
	origClient := updateHTTPClient
	updateCheckURL = server.URL
	updateHTTPClient = server.Client()
	defer func() {
		updateCheckURL = origURL
		updateHTTPClient = origClient
	}()

	_, _, err := checkForUpdate(context.Background(), "1.0.0")
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

func TestCheckForUpdate_EmptyTagName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"","html_url":"https://example.com"}`))
	}))
	defer server.Close()

	origURL := updateCheckURL
	origClient := updateHTTPClient
	updateCheckURL = server.URL
	updateHTTPClient = server.Client()
	defer func() {
		updateCheckURL = origURL
		updateHTTPClient = origClient
	}()

	_, _, err := checkForUpdate(context.Background(), "1.0.0")
	if err == nil {
		t.Fatal("expected error for empty tag_name")
	}
}

func TestCheckForUpdate_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not valid json`))
	}))
	defer server.Close()

	origURL := updateCheckURL
	origClient := updateHTTPClient
	updateCheckURL = server.URL
	updateHTTPClient = server.Client()
	defer func() {
		updateCheckURL = origURL
		updateHTTPClient = origClient
	}()

	_, _, err := checkForUpdate(context.Background(), "1.0.0")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCheckForUpdate_UpToDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v1.0.0","html_url":"https://example.com"}`))
	}))
	defer server.Close()

	origURL := updateCheckURL
	origClient := updateHTTPClient
	updateCheckURL = server.URL
	updateHTTPClient = server.Client()
	defer func() {
		updateCheckURL = origURL
		updateHTTPClient = origClient
	}()

	_, upToDate, err := checkForUpdate(context.Background(), "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !upToDate {
		t.Fatal("expected up to date when versions match")
	}
}

func TestCheckForUpdate_NoHTMLURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v2.0.0"}`))
	}))
	defer server.Close()

	origURL := updateCheckURL
	origClient := updateHTTPClient
	origReleases := updateReleasesURL
	updateCheckURL = server.URL
	updateHTTPClient = server.Client()
	defer func() {
		updateCheckURL = origURL
		updateHTTPClient = origClient
		updateReleasesURL = origReleases
	}()

	rel, _, err := checkForUpdate(context.Background(), "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When html_url is empty, it should default to updateReleasesURL.
	if rel.HTMLURL != updateReleasesURL {
		t.Errorf("expected HTMLURL=%q, got %q", updateReleasesURL, rel.HTMLURL)
	}
}

func TestCompareVersions_Extended(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		// Equal versions.
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		// Different lengths.
		{"1.0", "1.0.0", 0},
		{"1.0.1", "1.0", 1},
		{"1.0", "1.0.1", -1},
		// Numeric vs string comparison.
		{"1.2.3", "1.10.3", -1},
		// Non-numeric parts.
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-beta", "1.0.0-alpha", 1},
	}
	for _, tt := range tests {
		if got := compareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseChecksumFile_EmptyLines(t *testing.T) {
	dir := t.TempDir()
	content := "\n\nabc123  roboticus-linux-amd64\n\n\ndef456  roboticus-darwin-arm64\n\n"
	path := dir + "/SHA256SUMS.txt"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checksums, err := parseChecksumFile(path)
	if err != nil {
		t.Fatalf("parseChecksumFile: %v", err)
	}
	if len(checksums) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(checksums))
	}
}

func TestParseChecksumFile_NonExistent(t *testing.T) {
	_, err := parseChecksumFile("/nonexistent/path/SHA256SUMS.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestVerifyChecksum_NonExistentFile(t *testing.T) {
	err := verifyChecksum("/nonexistent/path/binary", "abc123")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestDownloadFile_ConnectionRefused(t *testing.T) {
	origClient := updateHTTPClient
	updateHTTPClient = &http.Client{}
	defer func() { updateHTTPClient = origClient }()

	_, err := downloadFile(context.Background(), "http://127.0.0.1:1/binary")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestCopyFile_SourceNotFound(t *testing.T) {
	err := copyFile("/nonexistent/src", t.TempDir()+"/dst")
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestCopyFile_BadDest(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/src"
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := copyFile(src, "/nonexistent/dir/dst")
	if err == nil {
		t.Fatal("expected error for bad destination directory")
	}
}
