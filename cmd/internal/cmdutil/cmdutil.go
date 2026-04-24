// Package cmdutil provides shared helper functions for CLI subpackages.
package cmdutil

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"

	"roboticus/internal/core"
)

// APIBaseURL returns the base URL for API calls.
func APIBaseURL() string {
	if u := viper.GetString("gateway.url"); u != "" {
		return strings.TrimRight(u, "/")
	}
	if u := os.Getenv("ROBOTICUS_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	port := viper.GetInt("server.port")
	if port == 0 {
		port = core.DefaultServerPort
	}
	return fmt.Sprintf("http://localhost:%d", port)
}

// APIGet performs a GET request to the local API.
func APIGet(path string) (map[string]any, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(APIBaseURL() + path)
	if err != nil {
		return nil, fmt.Errorf("connection failed (is roboticus running?): %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %s", string(body))
	}

	if resp.StatusCode >= 400 {
		return nil, apiStatusError(resp.StatusCode, data)
	}

	return data, nil
}

// APIPostSlow performs a POST request with a longer timeout for inference calls.
func APIPostSlow(path string, payload map[string]any, timeout time.Duration) (map[string]any, error) {
	client := &http.Client{Timeout: timeout}
	b, _ := json.Marshal(payload)
	resp, err := client.Post(APIBaseURL()+path, "application/json", strings.NewReader(string(b)))
	if err != nil {
		return nil, fmt.Errorf("connection failed (is roboticus running?): %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %s", string(body))
	}

	if resp.StatusCode >= 400 {
		return nil, apiStatusError(resp.StatusCode, data)
	}

	return data, nil
}

// APIPost performs a POST request with JSON body.
func APIPost(path string, payload map[string]any) (map[string]any, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	b, _ := json.Marshal(payload)
	resp, err := client.Post(APIBaseURL()+path, "application/json", strings.NewReader(string(b)))
	if err != nil {
		return nil, fmt.Errorf("connection failed (is roboticus running?): %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %s", string(body))
	}

	if resp.StatusCode >= 400 {
		return nil, apiStatusError(resp.StatusCode, data)
	}

	return data, nil
}

func apiStatusError(status int, data map[string]any) error {
	if msg, ok := data["error"]; ok && strings.TrimSpace(fmt.Sprint(msg)) != "" {
		return fmt.Errorf("API error: %v", msg)
	}
	if detail, ok := data["detail"]; ok && strings.TrimSpace(fmt.Sprint(detail)) != "" {
		return fmt.Errorf("API error: %v", detail)
	}
	if title, ok := data["title"]; ok && strings.TrimSpace(fmt.Sprint(title)) != "" {
		return fmt.Errorf("HTTP %d: %v", status, title)
	}
	return fmt.Errorf("HTTP %d", status)
}

// APIPut performs a PUT request with JSON body.
func APIPut(path string, payload map[string]any) (map[string]any, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", APIBaseURL()+path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed (is roboticus running?): %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %s", string(body))
	}

	if resp.StatusCode >= 400 {
		if msg, ok := data["error"]; ok {
			return nil, fmt.Errorf("API error: %v", msg)
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return data, nil
}

// APIDelete performs a DELETE request.
func APIDelete(path string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("DELETE", APIBaseURL()+path, nil)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// PrintJSON pretty-prints data as JSON. Respects --quiet and --json flags.
func PrintJSON(data any) {
	if viper.GetBool("quiet") {
		return
	}
	if viper.GetBool("json") {
		b, _ := json.Marshal(data)
		fmt.Println(string(b))
		return
	}
	b, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(b))
}

// OutputResult is the unified output function that respects --json and --quiet flags.
func OutputResult(data any, humanFn func(any)) {
	if viper.GetBool("quiet") {
		return
	}
	if viper.GetBool("json") {
		b, err := json.Marshal(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "JSON encode error: %v\n", err)
			return
		}
		fmt.Println(string(b))
		return
	}
	if humanFn != nil {
		humanFn(data)
	} else {
		PrintJSON(data)
	}
}

// OutputTable prints a simple table. Used by commands that list items.
func OutputTable(headers []string, rows [][]string) {
	if viper.GetBool("quiet") {
		return
	}
	if viper.GetBool("json") {
		var items []map[string]string
		for _, row := range rows {
			item := make(map[string]string)
			for i, h := range headers {
				if i < len(row) {
					item[h] = row[i]
				}
			}
			items = append(items, item)
		}
		b, _ := json.Marshal(items)
		fmt.Println(string(b))
		return
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	for i, h := range headers {
		fmt.Printf("%-*s  ", widths[i], strings.ToUpper(h))
	}
	fmt.Println()

	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				fmt.Printf("%-*s  ", widths[i], cell)
			}
		}
		fmt.Println()
	}
}

// OutputMessage prints a simple message, respecting --quiet.
func OutputMessage(format string, args ...any) {
	if viper.GetBool("quiet") {
		return
	}
	fmt.Printf(format+"\n", args...)
}

// LoadConfig unmarshals viper config into a core.Config struct.
func LoadConfig() (core.Config, error) {
	cfg := core.DefaultConfig()
	if err := viper.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("failed to parse config: %w", err)
	}
	cfg.MergeBundledProviders()
	cfg.NormalizePaths()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// EnsureParentDir creates the parent directory for a file path.
func EnsureParentDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o700)
}

// Version is injected at build time via -ldflags "-X roboticus/cmd/internal/cmdutil.Version=YYYY.MM.DD".
// Defaults to "dev" for local builds.
var Version = "dev"

// EffectiveConfigPath returns the config file path from viper or the default location.
//
// Returns the path AS TYPED by the operator — may be relative (if
// `--config configs/prod.toml` was passed) or absolute. For display,
// error messages, and anywhere "what the user typed" matters, use this.
//
// Callers that need a STABLE, filesystem-portable path across different
// working directories (e.g., embedding into a service installation, a
// launchd plist, or a systemd unit where the service manager's CWD
// isn't the shell's CWD) must use EffectiveConfigPathAbs instead.
func EffectiveConfigPath() string {
	if cf := viper.GetString("config"); cf != "" {
		return cf
	}
	if cf := os.Getenv("ROBOTICUS_CONFIG"); cf != "" {
		return cf
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".roboticus", "roboticus.toml")
	}
	return filepath.Join(home, ".roboticus", "roboticus.toml")
}

// EffectiveConfigPathAbs returns the config path absolutized against the
// current working directory. This is the install-time variant that
// MUST be used when the resolved path will outlive the current shell
// process — the canonical example is embedding `--config <path>` into
// a service definition (systemd, launchd, Windows SCM). The service
// manager's working directory when it later invokes roboticus is
// /, /var, or some other system-controlled location — a relative path
// would resolve against that, NOT the operator's install-time CWD.
//
// v1.0.6 P2-H: audit flagged that daemon.Install embedded the raw
// EffectiveConfigPath() into service args, so
// `roboticus daemon install --config configs/prod.toml` would later
// boot the service against whatever `configs/prod.toml` resolved to
// from the service manager's working directory — typically nothing at
// all. Absolutizing at install time freezes the intent.
//
// Filepath.Abs errors (invalid PWD, removed CWD) are surfaced to the
// caller, which should refuse to install a service with an ambiguous
// config reference rather than silently embed a broken path.
func EffectiveConfigPathAbs() (string, error) {
	raw := EffectiveConfigPath()
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolve config path %q to absolute: %w", raw, err)
	}
	return abs, nil
}
