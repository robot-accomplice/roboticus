package agent

import (
	"strings"
)

// protocolMarkers are internal markers that should never appear in user-facing output.
var protocolMarkers = []string{
	"[DELEGATION_STARTED]",
	"[DELEGATION_COMPLETED]",
	"[SUBTASK_RESULT]",
	"[ORCHESTRATION_PLAN]",
	"[INTERNAL_NOTE]",
	"[TASK_STATE:",
	"[EXECUTION_TRACE]",
	"<<<TRUST_BOUNDARY:",
}

// HasProtocolLeak checks if output contains internal protocol markers
// that should not be exposed to users.
func HasProtocolLeak(content string) bool {
	for _, marker := range protocolMarkers {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}

// StripProtocolLeaks removes all internal protocol markers from output.
// Used as a last-resort rescue when guards don't catch a leak.
func StripProtocolLeaks(content string) string {
	lines := strings.Split(content, "\n")
	var clean []string
	for _, line := range lines {
		hasMarker := false
		for _, marker := range protocolMarkers {
			if strings.Contains(line, marker) {
				hasMarker = true
				break
			}
		}
		if !hasMarker {
			clean = append(clean, line)
		}
	}
	return strings.TrimSpace(strings.Join(clean, "\n"))
}
