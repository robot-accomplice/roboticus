package agent

import agenttools "roboticus/internal/agent/tools"

// FilterToolOutput delegates to the canonical tool-layer normalization filter.
func FilterToolOutput(raw string) string {
	return agenttools.FilterToolOutput(raw)
}
