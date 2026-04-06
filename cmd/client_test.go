package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestAPIBaseURL_Default(t *testing.T) {
	// Clear any override so default kicks in.
	old := viper.GetInt("server.port")
	viper.Set("server.port", 0)
	defer viper.Set("server.port", old)

	url := apiBaseURL()
	// Should use the default port from core.DefaultServerPort.
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Errorf("unexpected URL: %s", url)
	}
}

func TestAPIBaseURL_CustomPort(t *testing.T) {
	old := viper.GetInt("server.port")
	viper.Set("server.port", 9999)
	defer viper.Set("server.port", old)

	url := apiBaseURL()
	if url != "http://127.0.0.1:9999" {
		t.Errorf("expected port 9999 in URL, got %s", url)
	}
}

func TestPrintJSON(t *testing.T) {
	// Just verify it doesn't panic.
	printJSON(map[string]any{"key": "value"})
	printJSON(nil)
	printJSON([]string{"a", "b"})
}

func TestAPIGet_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/test" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer server.Close()

	// Override the port to point at our test server.
	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	old := viper.GetInt("server.port")
	viper.Set("server.port", port)
	defer viper.Set("server.port", old)

	data, err := apiGet("/api/test")
	if err != nil {
		t.Fatalf("apiGet: %v", err)
	}
	if data["status"] != "ok" {
		t.Errorf("unexpected response: %v", data)
	}
}

func TestAPIGet_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "bad request"})
	}))
	defer server.Close()

	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	old := viper.GetInt("server.port")
	viper.Set("server.port", port)
	defer viper.Set("server.port", old)

	_, err := apiGet("/api/test")
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Errorf("expected 'bad request' in error, got: %v", err)
	}
}

func TestAPIGet_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	old := viper.GetInt("server.port")
	viper.Set("server.port", port)
	defer viper.Set("server.port", old)

	_, err := apiGet("/api/test")
	if err == nil {
		t.Fatal("expected error for non-JSON response")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("expected 'invalid JSON' in error, got: %v", err)
	}
}

func TestAPIPost_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected application/json content-type, got %s", ct)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "123", "received": body})
	}))
	defer server.Close()

	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	old := viper.GetInt("server.port")
	viper.Set("server.port", port)
	defer viper.Set("server.port", old)

	data, err := apiPost("/api/test", map[string]any{"name": "test"})
	if err != nil {
		t.Fatalf("apiPost: %v", err)
	}
	if data["id"] != "123" {
		t.Errorf("unexpected response: %v", data)
	}
}

func TestAPIPost_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "server error"})
	}))
	defer server.Close()

	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	old := viper.GetInt("server.port")
	viper.Set("server.port", port)
	defer viper.Set("server.port", old)

	_, err := apiPost("/api/test", nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "server error") {
		t.Errorf("expected 'server error' in error, got: %v", err)
	}
}

func TestAPIDelete_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	old := viper.GetInt("server.port")
	viper.Set("server.port", port)
	defer viper.Set("server.port", old)

	err := apiDelete("/api/test/123")
	if err != nil {
		t.Fatalf("apiDelete: %v", err)
	}
}

func TestAPIDelete_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer server.Close()

	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	old := viper.GetInt("server.port")
	viper.Set("server.port", port)
	defer viper.Set("server.port", old)

	err := apiDelete("/api/test/123")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected '404' in error, got: %v", err)
	}
}

func TestAPIGet_ConnectionRefused(t *testing.T) {
	// Use a port that's definitely not listening.
	old := viper.GetInt("server.port")
	viper.Set("server.port", 1)
	defer viper.Set("server.port", old)

	_, err := apiGet("/api/test")
	if err == nil {
		t.Fatal("expected connection error")
	}
	if !strings.Contains(err.Error(), "connection failed") {
		t.Errorf("expected 'connection failed' in error, got: %v", err)
	}
}
