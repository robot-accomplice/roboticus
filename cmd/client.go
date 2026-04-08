package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"

	"roboticus/internal/core"
)

// apiBaseURL returns the base URL for API calls.
// It checks the --url flag / ROBOTICUS_URL env var first,
// then falls back to localhost:{port}.
func apiBaseURL() string {
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
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// apiGet performs a GET request to the local API.
func apiGet(path string) (map[string]any, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiBaseURL() + path)
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

// apiPost performs a POST request with JSON body.
func apiPost(path string, payload map[string]any) (map[string]any, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	b, _ := json.Marshal(payload)
	resp, err := client.Post(apiBaseURL()+path, "application/json", strings.NewReader(string(b)))
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
	}

	return data, nil
}

// apiPut performs a PUT request with JSON body.
func apiPut(path string, payload map[string]any) (map[string]any, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", apiBaseURL()+path, strings.NewReader(string(b)))
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

// apiDelete performs a DELETE request.
func apiDelete(path string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("DELETE", apiBaseURL()+path, nil)
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

// printJSON pretty-prints data as JSON. Respects --quiet (suppresses output)
// and --json (compact single-line output for piping).
func printJSON(data any) {
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

// outputResult is the unified output function that respects --json and --quiet flags.
// All commands should use this instead of raw fmt.Println/printJSON.
//
//   - With --json: outputs json.Marshal(data)
//   - With --quiet: suppresses all non-error output
//   - Otherwise: calls the humanFn to produce formatted human-readable output
//
// The humanFn receives the data and should print it. If humanFn is nil,
// printJSON is used as the default.
func outputResult(data any, humanFn func(any)) {
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
		printJSON(data)
	}
}

// outputTable prints a simple table. Used by commands that list items.
func outputTable(headers []string, rows [][]string) {
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

	// Calculate column widths.
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

	// Print header.
	for i, h := range headers {
		fmt.Printf("%-*s  ", widths[i], strings.ToUpper(h))
	}
	fmt.Println()

	// Print rows.
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				fmt.Printf("%-*s  ", widths[i], cell)
			}
		}
		fmt.Println()
	}
}

// outputMessage prints a simple message, respecting --quiet.
func outputMessage(format string, args ...any) {
	if viper.GetBool("quiet") {
		return
	}
	fmt.Printf(format+"\n", args...)
}
