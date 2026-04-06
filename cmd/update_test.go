package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"2026.04.05", "2026.04.05", 0},
		{"2026.04.06", "2026.04.05", 1},
		{"2026.04.04", "2026.04.05", -1},
		{"v1.2.0", "1.10.0", -1},
	}

	for _, tt := range tests {
		if got := compareVersions(tt.a, tt.b); got != tt.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestCheckForUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v2026.04.10","html_url":"https://example.com/release"}`))
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

	rel, upToDate, err := checkForUpdate(context.Background(), "2026.04.05")
	if err != nil {
		t.Fatalf("checkForUpdate returned error: %v", err)
	}
	if rel.TagName != "v2026.04.10" {
		t.Fatalf("tag_name = %q", rel.TagName)
	}
	if upToDate {
		t.Fatal("expected update to be available")
	}
}

func TestCheckForUpdate_DevBuildNeverClaimsUpToDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v2026.04.10","html_url":"https://example.com/release"}`))
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

	_, upToDate, err := checkForUpdate(context.Background(), "dev")
	if err != nil {
		t.Fatalf("checkForUpdate returned error: %v", err)
	}
	if upToDate {
		t.Fatal("dev build should not report up to date")
	}
}
