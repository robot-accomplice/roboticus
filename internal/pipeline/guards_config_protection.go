package pipeline

import (
	"strings"

	"roboticus/internal/security"
)

// ConfigProtectionGuard blocks tool calls that attempt to mutate
// security-sensitive configuration keys. It inspects tool results for
// evidence of config mutation and rejects responses that include them.
type ConfigProtectionGuard struct{}

func (g *ConfigProtectionGuard) Name() string { return "config_protection" }

func (g *ConfigProtectionGuard) Check(content string) GuardResult {
	return GuardResult{Passed: true}
}

func (g *ConfigProtectionGuard) CheckWithContext(content string, ctx *GuardContext) GuardResult {
	if ctx == nil || len(ctx.ToolResults) == 0 {
		return GuardResult{Passed: true}
	}

	for _, tr := range ctx.ToolResults {
		lower := strings.ToLower(tr.ToolName)
		if !strings.Contains(lower, "config") && !strings.Contains(lower, "setting") {
			continue
		}
		outputLower := strings.ToLower(tr.Output)
		if pattern, matched := security.MatchProtectedConfigPattern(outputLower); matched {
			return GuardResult{
				Passed:  false,
				Content: "I cannot modify security-sensitive configuration settings through tool calls.",
				Reason:  "config_protection: tool attempted to modify " + pattern,
			}
		}
	}

	return GuardResult{Passed: true}
}
