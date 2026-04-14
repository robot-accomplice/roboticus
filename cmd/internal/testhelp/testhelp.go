// Package testhelp provides shared test helpers for CLI command tests.
package testhelp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// SetupMockAPI creates a mock server for API command tests.
// It returns a cleanup function that restores the original port setting.
func SetupMockAPI(t *testing.T, handler http.Handler) func() {
	t.Helper()
	server := httptest.NewServer(handler)
	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	old := viper.GetInt("server.port")
	viper.Set("server.port", port)
	return func() {
		viper.Set("server.port", old)
		server.Close()
	}
}

// JSONHandler returns an http.HandlerFunc that serves JSON-encoded data.
func JSONHandler(data any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}
}
