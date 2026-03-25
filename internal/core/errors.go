package core

import (
	"errors"
	"fmt"
)

// Sentinel errors for common failure modes.
var (
	ErrConfig           = errors.New("configuration error")
	ErrChannel          = errors.New("channel error")
	ErrDatabase         = errors.New("database error")
	ErrLLM              = errors.New("llm error")
	ErrNetwork          = errors.New("network error")
	ErrPolicy           = errors.New("policy violation")
	ErrTool             = errors.New("tool execution error")
	ErrWallet           = errors.New("wallet error")
	ErrInjection        = errors.New("injection detected")
	ErrSchedule         = errors.New("schedule error")
	ErrA2A              = errors.New("a2a protocol error")
	ErrIO               = errors.New("io error")
	ErrSkill            = errors.New("skill error")
	ErrKeystore         = errors.New("keystore error")
	ErrInjectionBlocked = errors.New("injection blocked")
	ErrDuplicate        = errors.New("duplicate request")
	ErrNotFound         = errors.New("not found")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrRateLimited      = errors.New("rate limited")
	ErrCreditExhausted  = errors.New("credit exhausted")
)

// GobError wraps an error with a category and optional context.
type GobError struct {
	Category error
	Message  string
	Cause    error
}

func (e *GobError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Category, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Category, e.Message)
}

func (e *GobError) Unwrap() error {
	return e.Cause
}

func (e *GobError) Is(target error) bool {
	return errors.Is(e.Category, target)
}

// NewError creates a new GobError with the given category and message.
func NewError(category error, msg string) *GobError {
	return &GobError{Category: category, Message: msg}
}

// WrapError creates a new GobError wrapping an existing error.
func WrapError(category error, msg string, cause error) *GobError {
	return &GobError{Category: category, Message: msg, Cause: cause}
}
