package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const gholaMaxStdoutBytes = 512 * 1024

// GholaTool runs the external `ghola` HTTP client (operator-installed; curl-like)
// to fetch remote pages. Arguments are built explicitly — no shell — and the
// target URL is validated like http_fetch (https only, no private/metadata hosts).
type GholaTool struct {
	// Executable is the ghola binary name or absolute path. Empty means "ghola"
	// resolved via PATH.
	Executable string
}

// NewGholaTool constructs a GholaTool. If exe is empty, "ghola" is used.
func NewGholaTool(exe string) *GholaTool {
	return &GholaTool{Executable: strings.TrimSpace(exe)}
}

func (t *GholaTool) Name() string { return "ghola" }

func (t *GholaTool) Description() string {
	return "Fetch a URL with the external ghola CLI (-X, -H, -d). " +
		"Redirect following, timeouts, and browser-like profiles are chosen from the request shape (plain GET vs POST/API), not separate fields. Public http(s) only."
}

func (t *GholaTool) Risk() RiskLevel { return RiskCaution }

func (t *GholaTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "Target URL (https recommended)"},
			"method": {"type": "string", "description": "HTTP method (default GET); passed as ghola -X"},
			"headers": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Optional headers; each passed as ghola -H \"Name: value\""},
			"body": {"type": "string", "description": "Optional body; ghola -d"},
			"timeout_seconds": {"type": "integer", "description": "Hard cap for the subprocess and ghola --timeout in ms (default 30, max 120)"}
		},
		"required": ["url"]
	}`)
}

func (t *GholaTool) resolveExecutable() (string, error) {
	exe := strings.TrimSpace(t.Executable)
	if exe == "" {
		exe = "ghola"
	}
	if filepath.Base(exe) == exe {
		return exec.LookPath(exe)
	}
	return exe, nil
}

func gholaValidateTargetURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("only http and https URLs are supported")
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("URL has no host")
	}
	if isPrivateHost(host) {
		return nil, fmt.Errorf("fetching from private or reserved addresses is not allowed")
	}
	return parsed, nil
}

// gholaDeriveClientOpts maps the logical HTTP request to ghola client flags.
// Plain document GETs (no body) get browser-like behavior for sites that reject generic clients;
// POST / non-GET / bodies stay minimal for API-style calls.
func gholaDeriveClientOpts(method, body string) (impersonate string, includeRespHeaders bool) {
	m := strings.TrimSpace(strings.ToUpper(method))
	if m == "" {
		m = "GET"
	}
	if (m == "GET" || m == "HEAD") && strings.TrimSpace(body) == "" {
		return "chrome", true
	}
	return "", false
}

// gholaArgv builds argv for ghola: options first, URL last (see `ghola --help`).
func gholaArgv(method string, headers map[string]string, body string, pageURL string, timeoutMs int, impersonate string, includeRespHeaders bool) []string {
	var argv []string
	argv = append(argv, "-L")
	if timeoutMs > 0 {
		argv = append(argv, "--timeout", strconv.Itoa(timeoutMs))
	}
	if imp := strings.TrimSpace(impersonate); imp != "" {
		argv = append(argv, "--impersonate", imp)
	}
	if includeRespHeaders {
		argv = append(argv, "-i")
	}
	m := strings.TrimSpace(strings.ToUpper(method))
	if m != "" && m != "GET" {
		argv = append(argv, "-X", m)
	}
	for k, v := range headers {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		argv = append(argv, "-H", fmt.Sprintf("%s: %s", k, strings.TrimSpace(v)))
	}
	if strings.TrimSpace(body) != "" {
		argv = append(argv, "-d", body)
	}
	argv = append(argv, pageURL)
	return argv
}

func (t *GholaTool) Execute(ctx context.Context, params string, _ *Context) (*Result, error) {
	var p struct {
		URL            string            `json:"url"`
		Method         string            `json:"method"`
		Headers        map[string]string `json:"headers"`
		Body           string            `json:"body"`
		TimeoutSeconds int               `json:"timeout_seconds"`
	}
	if err := json.Unmarshal([]byte(params), &p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(p.URL) == "" {
		return nil, fmt.Errorf("url is required")
	}
	pageURL, err := gholaValidateTargetURL(p.URL)
	if err != nil {
		return nil, err
	}

	timeout := 30
	if p.TimeoutSeconds > 0 {
		timeout = p.TimeoutSeconds
	}
	if timeout < 1 {
		timeout = 1
	}
	if timeout > 120 {
		timeout = 120
	}
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	bin, err := t.resolveExecutable()
	if err != nil {
		return nil, fmt.Errorf("ghola executable: %w", err)
	}

	timeoutMs := timeout * 1000
	impersonate, includeHdr := gholaDeriveClientOpts(p.Method, p.Body)

	argv := gholaArgv(p.Method, p.Headers, p.Body, pageURL.String(), timeoutMs, impersonate, includeHdr)

	cmd := exec.CommandContext(execCtx, bin, argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	out := stdout.String()
	if stderr.Len() > 0 {
		if out != "" {
			out += "\n"
		}
		out += stderr.String()
	}
	if runErr != nil {
		if out == "" {
			out = runErr.Error()
		} else {
			out = runErr.Error() + "\n" + out
		}
	}
	if len(out) > gholaMaxStdoutBytes {
		out = out[:gholaMaxStdoutBytes] + "\n...[truncated]"
	}
	return &Result{Output: out, Source: "builtin"}, nil
}
