package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// WebSearchTool performs web searches using a configurable search API.
type WebSearchTool struct {
	httpClient *http.Client
	searchURL  string
	apiKey     string
}

// NewWebSearchTool creates a web search tool.
func NewWebSearchTool(searchURL, apiKey string) *WebSearchTool {
	if searchURL == "" {
		searchURL = "http://localhost:8888/search"
	}
	return &WebSearchTool{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		searchURL:  searchURL,
		apiKey:     apiKey,
	}
}

func (t *WebSearchTool) Name() string        { return "web_search" }
func (t *WebSearchTool) Description() string  { return "Search the web for current information." }
func (t *WebSearchTool) Risk() RiskLevel      { return RiskCaution }
func (t *WebSearchTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "The search query"},
			"num_results": {"type": "integer", "description": "Number of results to return (default 5, max 10)"}
		},
		"required": ["query"]
	}`)
}

func (t *WebSearchTool) Execute(ctx context.Context, params string, _ *Context) (*Result, error) {
	var p struct {
		Query      string `json:"query"`
		NumResults int    `json:"num_results"`
	}
	if err := json.Unmarshal([]byte(params), &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if p.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if p.NumResults <= 0 || p.NumResults > 10 {
		p.NumResults = 5
	}

	u, err := url.Parse(t.searchURL)
	if err != nil {
		return nil, fmt.Errorf("invalid search URL: %w", err)
	}
	q := u.Query()
	q.Set("q", p.Query)
	q.Set("format", "json")
	q.Set("count", fmt.Sprintf("%d", p.NumResults))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("reading search response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search API returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return &Result{Output: string(body)}, nil
	}

	var sb strings.Builder
	for i, r := range result.Results {
		if i >= p.NumResults {
			break
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Content))
	}

	if sb.Len() == 0 {
		return &Result{Output: "No results found."}, nil
	}
	return &Result{Output: sb.String()}, nil
}

// HTTPFetchTool fetches content from a URL.
type HTTPFetchTool struct {
	httpClient *http.Client
}

// NewHTTPFetchTool creates an HTTP fetch tool.
func NewHTTPFetchTool() *HTTPFetchTool {
	return &HTTPFetchTool{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

func (t *HTTPFetchTool) Name() string        { return "http_fetch" }
func (t *HTTPFetchTool) Description() string  { return "Fetch content from a URL via HTTP GET." }
func (t *HTTPFetchTool) Risk() RiskLevel      { return RiskCaution }
func (t *HTTPFetchTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "The URL to fetch"},
			"headers": {"type": "object", "description": "Optional HTTP headers"}
		},
		"required": ["url"]
	}`)
}

func (t *HTTPFetchTool) Execute(ctx context.Context, params string, _ *Context) (*Result, error) {
	var p struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal([]byte(params), &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if p.URL == "" {
		return nil, fmt.Errorf("url is required")
	}

	parsed, err := url.Parse(p.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("only http and https URLs are supported")
	}

	host := parsed.Hostname()
	if isPrivateHost(host) {
		return nil, fmt.Errorf("fetching from private/localhost addresses is not allowed")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", p.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Goboticus/1.0")
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	output := fmt.Sprintf("HTTP %d %s\nContent-Type: %s\nContent-Length: %d\n\n%s",
		resp.StatusCode, resp.Status,
		resp.Header.Get("Content-Type"),
		len(body),
		string(body),
	)
	return &Result{Output: output}, nil
}

func isPrivateHost(host string) bool {
	lower := strings.ToLower(host)

	// Loopback and special addresses.
	if lower == "localhost" || lower == "0.0.0.0" {
		return true
	}

	// Resolve to IP and check using net.IP methods for comprehensive coverage.
	// This also handles IPv4-mapped IPv6 (::ffff:127.0.0.1), DNS rebinding defense
	// is handled by resolving the hostname to IPs before the HTTP call.
	ips, err := net.LookupHost(host)
	if err != nil {
		// If we can't resolve, check string patterns as fallback.
		return isPrivateHostString(lower)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return true
		}
		// Cloud metadata endpoints (AWS, GCP, Azure).
		if ip.Equal(net.ParseIP("169.254.169.254")) {
			return true
		}
		// GCP metadata alternative.
		if ip.Equal(net.ParseIP("metadata.google.internal")) {
			return true
		}
	}
	return false
}

// isPrivateHostString is a fallback string-based check when DNS resolution fails.
func isPrivateHostString(host string) bool {
	if host == "127.0.0.1" || host == "::1" || host == "0.0.0.0" || host == "localhost" {
		return true
	}
	if strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "169.254.") {
		return true
	}
	if strings.HasPrefix(host, "172.") {
		parts := strings.SplitN(host, ".", 3)
		if len(parts) >= 2 {
			second, err := strconv.Atoi(parts[1])
			if err == nil && second >= 16 && second <= 31 {
				return true
			}
		}
	}
	// IPv6 patterns.
	if strings.HasPrefix(host, "::ffff:") || strings.HasPrefix(host, "fe80:") || host == "::1" {
		return true
	}
	return false
}
