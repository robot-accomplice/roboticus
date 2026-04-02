package core

import (
	"errors"
	"testing"
)

func TestGobError_Error(t *testing.T) {
	err := NewError(ErrConfig, "missing field")
	if err.Error() == "" {
		t.Error("should have message")
	}
}

func TestGobError_Unwrap(t *testing.T) {
	inner := errors.New("inner error")
	wrapped := WrapError(ErrDatabase, "wrapper", inner)
	if !errors.Is(wrapped, inner) {
		t.Error("should unwrap to inner error")
	}
}

func TestGobError_Code(t *testing.T) {
	err := NewError(ErrLLM, "llm failed")
	var ge *GobError
	if errors.As(err, &ge) {
		if !errors.Is(ge.Category, ErrLLM) {
			t.Errorf("category = %v, want ErrLLM", ge.Category)
		}
	} else {
		t.Error("should be GobError")
	}
}

func TestSentinelErrors_Exist(t *testing.T) {
	errs := []error{
		ErrConfig, ErrDatabase, ErrLLM, ErrNetwork,
		ErrRateLimited, ErrUnauthorized, ErrCreditExhausted,
		ErrInjectionBlocked,
	}
	for _, e := range errs {
		if e == nil {
			t.Error("sentinel error should not be nil")
		}
		if e.Error() == "" {
			t.Error("sentinel error should have message")
		}
	}
}

func TestOrDone_ContextCancelled(t *testing.T) {
	// OrDone should close output when context done closes.
	// This is a utility in core/ordone.go.
}
