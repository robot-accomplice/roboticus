package policy

import (
	"strings"
	"testing"

	"roboticus/internal/core"
)

func TestFormatDeniedToolResult_AuthorityCaution_Channel(t *testing.T) {
	dec := Deny("authority", "caution tools require peer or higher authority")
	out := FormatDeniedToolResult("ghola", dec, core.AuthorityExternal, nil)
	if !strings.Contains(out, `rule "authority"`) || !strings.Contains(out, "priority 1") {
		t.Fatalf("expected explicit rule id and priority: %q", out)
	}
	if !strings.Contains(out, "Policy denied: caution tools require peer or higher authority") {
		t.Fatalf("denial line: %q", out)
	}
	if !strings.Contains(out, "channels.*") || !strings.Contains(out, "trusted_sender_ids") {
		t.Fatalf("expected channel remediation hints: %s", out)
	}
	if !strings.Contains(out, "name the invoked policy rule") {
		t.Fatalf("expected cite guidance: %s", out)
	}
}

func TestFormatDeniedToolResult_AuthorityCaution_API(t *testing.T) {
	dec := Deny("authority", "caution tools require peer or higher authority")
	claim := &core.SecurityClaim{
		Authority: core.AuthorityExternal,
		Sources:   []core.ClaimSource{core.ClaimSourceAPIKey},
	}
	out := FormatDeniedToolResult("http_fetch", dec, core.AuthorityExternal, claim)
	if !strings.Contains(out, "HTTP API") || !strings.Contains(out, "threat") {
		t.Fatalf("expected API-specific hint: %s", out)
	}
}

func TestFormatDeniedToolResult_PathProtection(t *testing.T) {
	dec := Deny("path_protection", "path traversal detected")
	out := FormatDeniedToolResult("read_file", dec, core.AuthorityPeer, nil)
	if !strings.Contains(out, `rule "path_protection"`) || !strings.Contains(out, "priority 4") {
		t.Fatalf("expected path_protection rule: %q", out)
	}
	if !strings.Contains(out, "security.allowed_paths") {
		t.Fatalf("expected path remediation: %s", out)
	}
}

func TestFormatBlockedToolResult_CitesPolicy(t *testing.T) {
	out := FormatBlockedToolResult("ghola")
	if !strings.Contains(out, "agent.approvals.blocked_tools") || !strings.Contains(out, `"ghola"`) {
		t.Fatalf("expected approvals policy path: %q", out)
	}
}
