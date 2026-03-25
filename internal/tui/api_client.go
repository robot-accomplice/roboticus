package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var tuiHTTPClient = &http.Client{Timeout: 30 * time.Second}

// tuiAPIPost sends a JSON POST request and returns the parsed response.
func tuiAPIPost(url string, body map[string]any) (map[string]any, error) {
	b, _ := json.Marshal(body)
	resp, err := tuiHTTPClient.Post(url, "application/json", strings.NewReader(string(b)))
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return data, nil
}
