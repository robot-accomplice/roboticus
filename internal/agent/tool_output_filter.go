package agent

import "fmt"

// OutputFilter truncates verbose tool outputs to preserve token budget.
type OutputFilter struct {
	maxChars int
}

// NewOutputFilter creates a filter with the given character limit.
func NewOutputFilter(maxChars int) *OutputFilter {
	return &OutputFilter{maxChars: maxChars}
}

// Filter truncates output if it exceeds the limit.
func (f *OutputFilter) Filter(toolName string, output string) string {
	if output == "" || len(output) <= f.maxChars {
		return output
	}
	return output[:f.maxChars] + fmt.Sprintf("\n... [truncated: %s output was %d chars, limit %d]", toolName, len(output), f.maxChars)
}
