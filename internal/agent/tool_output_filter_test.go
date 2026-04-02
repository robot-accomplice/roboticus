package agent

import (
	"strings"
	"testing"
)

func TestOutputFilter_Passthrough(t *testing.T) {
	f := NewOutputFilter(2000)
	short := "This is a short output"
	result := f.Filter("echo", short)
	if result != short {
		t.Errorf("short output was modified: got %q", result)
	}
}

func TestOutputFilter_Truncation(t *testing.T) {
	f := NewOutputFilter(100)
	long := strings.Repeat("x", 500)
	result := f.Filter("web_search", long)

	if len(result) > 200 { // slack for truncation notice with tool name + char counts
		t.Errorf("result too long: %d chars", len(result))
	}
	if !strings.Contains(result, "truncated") {
		t.Error("truncated output should contain truncation notice")
	}
}

func TestOutputFilter_EmptyOutput(t *testing.T) {
	f := NewOutputFilter(2000)
	result := f.Filter("echo", "")
	if result != "" {
		t.Errorf("empty output should stay empty, got %q", result)
	}
}

func TestOutputFilter_ExactBoundary(t *testing.T) {
	f := NewOutputFilter(10)
	exact := "1234567890" // exactly 10 chars
	result := f.Filter("test", exact)
	if result != exact {
		t.Error("output at exact boundary should not be truncated")
	}
}
