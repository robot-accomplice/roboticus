package cmdutil

import (
	"strings"
	"testing"
)

func TestAPIStatusError_UsesProblemDetailsDetail(t *testing.T) {
	err := apiStatusError(500, map[string]any{
		"type":   "about:blank",
		"title":  "Internal Server Error",
		"status": 500,
		"detail": "inference failed",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "inference failed") {
		t.Fatalf("error = %q, want problem detail", err.Error())
	}
}
