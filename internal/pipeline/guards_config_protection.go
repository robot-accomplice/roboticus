package pipeline

import "strings"

// ConfigProtectionGuard blocks tool calls that attempt to mutate
// security-sensitive configuration keys. It inspects tool results for
// evidence of config mutation and rejects responses that include them.
type ConfigProtectionGuard struct{}

// sensitiveConfigPatterns are prefixes/keys that must not be mutated by tools.
var sensitiveConfigPatterns = []string{
	"api_key",
	"database.path",
	"keystore.",
	"server.bind",
	"server.tls_cert",
	"server.tls_key",
	"server.auth_token",
	"wallet.passphrase",
	"wallet.keyfile",
}

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
		for _, pattern := range sensitiveConfigPatterns {
			if strings.Contains(outputLower, pattern) {
				return GuardResult{
					Passed:  false,
					Content: "I cannot modify security-sensitive configuration settings through tool calls.",
					Reason:  "config_protection: tool attempted to modify " + pattern,
				}
			}
		}
	}

	return GuardResult{Passed: true}
}
