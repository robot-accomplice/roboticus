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
	defer resp.Body.Close()

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return data, nil
}

// tuiAPIGet sends a GET request and returns the parsed response.
func tuiAPIGet(url string) (map[string]any, error) {
	resp, err := tuiHTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return data, nil
}
