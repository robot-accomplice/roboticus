package cmd

// This file provides package-level wrappers around cmdutil functions.
// These are used by root-level tests and the completion command.
// New code in subpackages should import cmdutil directly.

import (
	"time"

	"roboticus/cmd/internal/cmdutil"
)

func apiBaseURL() string {
	return cmdutil.APIBaseURL()
}

func apiGet(path string) (map[string]any, error) {
	return cmdutil.APIGet(path)
}

func apiPostSlow(path string, payload map[string]any, timeout time.Duration) (map[string]any, error) {
	return cmdutil.APIPostSlow(path, payload, timeout)
}

func apiPost(path string, payload map[string]any) (map[string]any, error) {
	return cmdutil.APIPost(path, payload)
}

func apiPut(path string, payload map[string]any) (map[string]any, error) {
	return cmdutil.APIPut(path, payload)
}

func apiDelete(path string) error {
	return cmdutil.APIDelete(path)
}

func printJSON(data any) {
	cmdutil.PrintJSON(data)
}

func outputResult(data any, humanFn func(any)) {
	cmdutil.OutputResult(data, humanFn)
}

func _outputTable(headers []string, rows [][]string) { //nolint:unused // planned for CLI list commands
	cmdutil.OutputTable(headers, rows)
}

func outputMessage(format string, args ...any) {
	cmdutil.OutputMessage(format, args...)
}
