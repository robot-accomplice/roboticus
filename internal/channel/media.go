package channel

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

const (
	defaultMaxFileSize = 10 * 1024 * 1024 // 10 MB
	defaultTimeout     = 30 * time.Second
	maxFilenameLen     = 255
)

// MediaService downloads remote media with SSRF protection.
type MediaService struct {
	maxFileSize   int64
	httpClient    *http.Client
	validateURL   func(string) error // override for testing; nil means use ValidateRemoteURL
}

// DownloadedMedia holds a fetched file's bytes and metadata.
type DownloadedMedia struct {
	Data        []byte
	ContentType string
	Filename    string
	SizeBytes   int64
}

// NewMediaService creates a MediaService with sensible defaults.
func NewMediaService() *MediaService {
	return &MediaService{
		maxFileSize: defaultMaxFileSize,
		httpClient:  &http.Client{Timeout: defaultTimeout},
	}
}

// NewMediaServiceWithOptions creates a MediaService with custom limits.
func NewMediaServiceWithOptions(maxFileSize int64, timeout time.Duration) *MediaService {
	return &MediaService{
		maxFileSize: maxFileSize,
		httpClient:  &http.Client{Timeout: timeout},
	}
}

// ValidateRemoteURL checks that rawURL is safe to fetch (not an internal/private address).
func ValidateRemoteURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported scheme %q: only http and https are allowed", parsed.Scheme)
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}

	lower := strings.ToLower(host)
	if lower == "localhost" || lower == "0.0.0.0" {
		return fmt.Errorf("host %q is not allowed", host)
	}

	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("failed to resolve host %q: %w", host, err)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if err := checkIP(ip); err != nil {
			return fmt.Errorf("host %q resolves to blocked address %s: %w", host, ipStr, err)
		}
	}

	return nil
}

// checkIP returns an error if the IP falls into a forbidden range.
func checkIP(ip net.IP) error {
	if ip.IsLoopback() {
		return fmt.Errorf("loopback address")
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("link-local address")
	}
	if ip.IsMulticast() {
		return fmt.Errorf("multicast address")
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified address")
	}

	// Check RFC1918 private ranges.
	if ip4 := ip.To4(); ip4 != nil {
		if isPrivateIPv4(ip4) {
			return fmt.Errorf("private address")
		}
		// Cloud metadata endpoint 169.254.169.254.
		if ip4[0] == 169 && ip4[1] == 254 && ip4[2] == 169 && ip4[3] == 254 {
			return fmt.Errorf("cloud metadata endpoint")
		}
	}

	return nil
}

// isPrivateIPv4 checks whether ip4 is in 10.0.0.0/8, 172.16.0.0/12, or 192.168.0.0/16.
func isPrivateIPv4(ip4 net.IP) bool {
	// 10.0.0.0/8
	if ip4[0] == 10 {
		return true
	}
	// 172.16.0.0/12
	if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
		return true
	}
	// 192.168.0.0/16
	if ip4[0] == 192 && ip4[1] == 168 {
		return true
	}
	return false
}

// Download fetches media from rawURL with SSRF protection and size limits.
func (m *MediaService) Download(ctx context.Context, rawURL string) (*DownloadedMedia, error) {
	validate := ValidateRemoteURL
	if m.validateURL != nil {
		validate = m.validateURL
	}
	if err := validate(rawURL); err != nil {
		return nil, fmt.Errorf("URL validation failed: %w", err)
	}

	// HEAD to check Content-Length before downloading.
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating HEAD request: %w", err)
	}
	headResp, err := m.httpClient.Do(headReq)
	if err != nil {
		return nil, fmt.Errorf("HEAD request failed: %w", err)
	}
	_ = headResp.Body.Close()

	if headResp.ContentLength > m.maxFileSize {
		return nil, fmt.Errorf("file too large: Content-Length %d exceeds limit %d", headResp.ContentLength, m.maxFileSize)
	}

	// GET to download the body.
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating GET request: %w", err)
	}
	getResp, err := m.httpClient.Do(getReq)
	if err != nil {
		return nil, fmt.Errorf("GET request failed: %w", err)
	}
	defer func() { _ = getResp.Body.Close() }()

	// Read up to maxFileSize+1 to detect truncation.
	data, err := io.ReadAll(io.LimitReader(getResp.Body, m.maxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	if int64(len(data)) > m.maxFileSize {
		return nil, fmt.Errorf("file too large: body exceeds limit %d", m.maxFileSize)
	}

	contentType := getResp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	parsed, _ := url.Parse(rawURL)
	filename := SanitizeFilename(path.Base(parsed.Path))

	return &DownloadedMedia{
		Data:        data,
		ContentType: contentType,
		Filename:    filename,
		SizeBytes:   int64(len(data)),
	}, nil
}

var safeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// SanitizeFilename strips directory components, replaces unsafe characters, and limits length.
func SanitizeFilename(name string) string {
	// Strip directory traversal and path components.
	name = path.Base(name)
	// Replace backslash-separated components too.
	if idx := strings.LastIndex(name, `\`); idx >= 0 {
		name = name[idx+1:]
	}

	// Replace unsafe characters.
	name = safeFilenameRe.ReplaceAllString(name, "_")

	// Trim leading/trailing underscores and dots for cleanliness.
	name = strings.Trim(name, "_.")

	if name == "" {
		return "download"
	}

	if len(name) > maxFilenameLen {
		name = name[:maxFilenameLen]
	}

	return name
}
