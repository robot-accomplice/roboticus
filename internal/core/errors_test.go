package core

import (
	"errors"
	"fmt"
	"testing"
)

func TestGobErrorIs(t *testing.T) {
	err := NewError(ErrDatabase, "connection failed")
	if !errors.Is(err, ErrDatabase) {
		t.Error("GobError should match ErrDatabase")
	}
	if errors.Is(err, ErrConfig) {
		t.Error("GobError should not match ErrConfig")
	}
}

func TestGobErrorWraps(t *testing.T) {
	cause := fmt.Errorf("socket timeout")
	err := WrapError(ErrNetwork, "request failed", cause)

	if !errors.Is(err, ErrNetwork) {
		t.Error("wrapped error should match ErrNetwork category")
	}

	unwrapped := errors.Unwrap(err)
	if unwrapped == nil {
		t.Error("Unwrap should return the cause")
	}

	expected := "network error: request failed: socket timeout"
	if err.Error() != expected {
		t.Errorf("got %q, want %q", err.Error(), expected)
	}
}

func TestGobErrorNoWrap(t *testing.T) {
	err := NewError(ErrConfig, "bad port")
	expected := "configuration error: bad port"
	if err.Error() != expected {
		t.Errorf("got %q, want %q", err.Error(), expected)
	}
}
