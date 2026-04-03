package channel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateRemoteURL_BlocksPrivateIPs(t *testing.T) {
	privateHosts := []string{
		"http://10.0.0.1/file",
		"http://10.255.255.255/file",
		"http://172.16.0.1/file",
		"http://172.31.255.255/file",
		"http://192.168.0.1/file",
		"http://192.168.1.100/file",
	}
	for _, u := range privateHosts {
		t.Run(u, func(t *testing.T) {
			err := ValidateRemoteURL(u)
			if err == nil {
				t.Errorf("expected error for private IP URL %q, got nil", u)
			}
		})
	}
}

func TestValidateRemoteURL_BlocksLocalhost(t *testing.T) {
	localhostURLs := []string{
		"http://localhost/file",
		"http://localhost:8080/file",
		"http://127.0.0.1/file",
		"http://127.0.0.1:3000/file",
		"http://0.0.0.0/file",
	}
	for _, u := range localhostURLs {
		t.Run(u, func(t *testing.T) {
			err := ValidateRemoteURL(u)
			if err == nil {
				t.Errorf("expected error for localhost URL %q, got nil", u)
			}
		})
	}
}

func TestValidateRemoteURL_BlocksMetadataEndpoint(t *testing.T) {
	metadataURLs := []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://169.254.169.254/metadata/v1/",
	}
	for _, u := range metadataURLs {
		t.Run(u, func(t *testing.T) {
			err := ValidateRemoteURL(u)
			if err == nil {
				t.Errorf("expected error for metadata endpoint URL %q, got nil", u)
			}
		})
	}
}

func TestValidateRemoteURL_BlocksBadSchemes(t *testing.T) {
	badURLs := []string{
		"ftp://example.com/file",
		"file:///etc/passwd",
		"gopher://evil.com",
	}
	for _, u := range badURLs {
		t.Run(u, func(t *testing.T) {
			err := ValidateRemoteURL(u)
			if err == nil {
				t.Errorf("expected error for bad scheme URL %q, got nil", u)
			}
		})
	}
}

func TestValidateRemoteURL_AllowsPublicURLs(t *testing.T) {
	// Use only domains guaranteed to resolve (IANA reserved).
	publicURLs := []string{
		"https://example.com/image.png",
		"http://example.com/photo.jpg",
	}
	for _, u := range publicURLs {
		t.Run(u, func(t *testing.T) {
			err := ValidateRemoteURL(u)
			if err != nil {
				if strings.Contains(err.Error(), "lookup") || strings.Contains(err.Error(), "no such host") {
					t.Skipf("DNS unavailable: %v", err)
				}
				t.Errorf("unexpected error for public URL %q: %v", u, err)
			}
		})
	}
}

func TestSanitizeFilename_StripsTraversal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"../../../etc/passwd", "passwd"},
		{"..\\..\\windows\\system32\\cmd.exe", "cmd.exe"},
		{"path/to/file.txt", "file.txt"},
		{"normal-file.png", "normal-file.png"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := SanitizeFilename(tc.input)
			if got != tc.want {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSanitizeFilename_HandlesEmpty(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "download"},
		{".", "download"},
		{"..", "download"},
		{"   ", "download"},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q", tc.input), func(t *testing.T) {
			got := SanitizeFilename(tc.input)
			if got != tc.want {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSanitizeFilename_LimitsLength(t *testing.T) {
	long := strings.Repeat("a", 300)
	got := SanitizeFilename(long)
	if len(got) > maxFilenameLen {
		t.Errorf("SanitizeFilename produced %d chars, want <= %d", len(got), maxFilenameLen)
	}
}

func noopValidate(_ string) error { return nil }

func TestDownload_Success(t *testing.T) {
	body := "hello world"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, body)
	}))
	defer ts.Close()

	ms := NewMediaService()
	ms.validateURL = noopValidate // httptest binds to loopback
	result, err := ms.Download(context.Background(), ts.URL+"/test.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Data) != body {
		t.Errorf("got data %q, want %q", result.Data, body)
	}
	if result.ContentType != "text/plain" {
		t.Errorf("got content type %q, want text/plain", result.ContentType)
	}
	if result.Filename != "test.txt" {
		t.Errorf("got filename %q, want test.txt", result.Filename)
	}
	if result.SizeBytes != int64(len(body)) {
		t.Errorf("got size %d, want %d", result.SizeBytes, len(body))
	}
}

func TestDownload_RejectsTooLarge(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "999999999")
		w.Header().Set("Content-Type", "application/octet-stream")
		// Don't write body; HEAD check should reject.
	}))
	defer ts.Close()

	ms := NewMediaServiceWithOptions(1024, defaultTimeout)
	ms.validateURL = noopValidate // httptest binds to loopback
	_, err := ms.Download(context.Background(), ts.URL+"/big.bin")
	if err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("expected 'too large' error, got: %v", err)
	}
}
