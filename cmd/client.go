package cmd

// This file provides package-level wrappers around cmdutil functions.
// These are used by root-level tests and the completion command.
// New code in subpackages should import cmdutil directly.
//
// apiPostSlow, apiPut, outputResult, outputMessage were removed in v1.0.6
// after lint flagged them as unused across the whole codebase. Reinstate
// as thin wrappers if tests or new root-level code need them again.

import (
	"roboticus/cmd/internal/cmdutil"
)

func apiBaseURL() string {
	return cmdutil.APIBaseURL()
}

func apiGet(path string) (map[string]any, error) {
	return cmdutil.APIGet(path)
}

func apiPost(path string, payload map[string]any) (map[string]any, error) {
	return cmdutil.APIPost(path, payload)
}

func apiDelete(path string) error {
	return cmdutil.APIDelete(path)
}

func printJSON(data any) {
	cmdutil.PrintJSON(data)
}

func _outputTable(headers []string, rows [][]string) { //nolint:unused // planned for CLI list commands
	cmdutil.OutputTable(headers, rows)
}
