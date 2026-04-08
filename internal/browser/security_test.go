package browser

import (
	"strings"
	"testing"
)

func TestIsSchemeAllowed(t *testing.T) {
	tests := []struct {
		url     string
		allowed bool
	}{
		{"https://example.com", true},
		{"http://localhost:8080", true},
		{"file:///etc/passwd", false},
		{"FILE:///etc/passwd", false},
		{"javascript:alert(1)", false},
		{"JAVASCRIPT:void(0)", false},
		{"data:text/html,hello", false},
		{"ftp://files.example.com", false},
		{"chrome://settings", false},
		{"chrome-extension://abc/popup.html", false},
		{"about:blank", false},
		{"blob:http://example.com/uuid", false},
		{"", true}, // empty is allowed (will fail elsewhere)
		{"ws://localhost", true},
		{"wss://localhost", true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := isSchemeAllowed(tt.url)
			if got != tt.allowed {
				t.Errorf("isSchemeAllowed(%q) = %v, want %v", tt.url, got, tt.allowed)
			}
		})
	}
}

func TestValidateSelector(t *testing.T) {
	// Valid selector.
	if err := validateSelector("div.class > span#id"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Too long.
	long := strings.Repeat("a", MaxSelectorLength+1)
	if err := validateSelector(long); err == nil {
		t.Fatal("expected error for long selector")
	}

	// Contains curly braces.
	if err := validateSelector("div { color: red }"); err == nil {
		t.Fatal("expected error for curly braces")
	}

	// Exactly at limit.
	exact := strings.Repeat("x", MaxSelectorLength)
	if err := validateSelector(exact); err != nil {
		t.Fatalf("unexpected error at exact limit: %v", err)
	}
}

func TestValidateExpression(t *testing.T) {
	// Valid expression.
	if err := validateExpression("document.title"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Too long.
	long := strings.Repeat("x", MaxExpressionLength+1)
	if err := validateExpression(long); err == nil {
		t.Fatal("expected error for long expression")
	}

	// Exactly at limit.
	exact := strings.Repeat("x", MaxExpressionLength)
	if err := validateExpression(exact); err != nil {
		t.Fatalf("unexpected error at exact limit: %v", err)
	}
}
