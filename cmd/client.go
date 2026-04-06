package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/viper"

	"roboticus/internal/core"
)

// apiBaseURL returns the base URL for API calls.
func apiBaseURL() string {
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

// printJSON pretty-prints a map as JSON.
func printJSON(data any) {
	b, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(b))
}
