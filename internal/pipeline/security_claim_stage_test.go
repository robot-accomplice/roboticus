package pipeline

import (
	"context"
	"strings"
	"testing"

	"roboticus/internal/core"
)

func TestStageAuthority_AttachesResolvedSecurityClaimToSessionAndTrace(t *testing.T) {
	pipe := New(PipelineDeps{})
	pc := &pipelineContext{
		cfg: PresetChannel("telegram"),
		input: Input{
			Claim: &ChannelClaimContext{
				SenderID:            "sender-1",
				ChatID:              "chat-1",
				Platform:            "telegram",
				SenderInAllowlist:   true,
				AllowlistConfigured: true,
				TrustedSenderIDs:    []string{"chat-1"},
			},
		},
		session: NewSession("s1", "agent-1", "TestBot"),
		tr:      NewTraceRecorder(),
	}

	pipe.stageAuthority(context.Background(), pc)

	if pc.session.SecurityClaim == nil {
		t.Fatal("expected security claim on session")
	}
	if got := pc.session.SecurityClaim.Authority; got != core.AuthorityCreator {
		t.Fatalf("authority = %s, want %s", got, core.AuthorityCreator)
	}
	if got := pc.session.SecurityClaim.Channel; got != "telegram" {
		t.Fatalf("channel = %q, want telegram", got)
	}
	if got := pc.session.SecurityClaim.SenderID; got != "sender-1" {
		t.Fatalf("sender id = %q, want sender-1", got)
	}
	if pc.session.Authority != core.AuthorityCreator {
		t.Fatalf("session authority = %s, want %s", pc.session.Authority, core.AuthorityCreator)
	}

	trace := pc.tr.Finish("turn-1", "channel")
	authSpanFound := false
	for _, span := range trace.Stages {
		if span.Name != "authority_resolution" {
			continue
		}
		authSpanFound = true
		if got := span.Metadata["authority"]; got != core.AuthorityCreator.String() {
			t.Fatalf("trace authority annotation = %v, want %s", got, core.AuthorityCreator)
		}
		rawSources, _ := span.Metadata["claim_sources"].(string)
		if !strings.Contains(rawSources, core.ClaimSourceChannelAllowList.String()) {
			t.Fatalf("trace claim_sources missing allow-list source: %q", rawSources)
		}
		if !strings.Contains(rawSources, core.ClaimSourceTrustedSenderID.String()) {
			t.Fatalf("trace claim_sources missing trusted-sender source: %q", rawSources)
		}
	}
	if !authSpanFound {
		t.Fatal("expected authority_resolution span")
	}
}

func TestStageAuthority_ThreatCautionDowngradesCreatorClaim(t *testing.T) {
	pipe := New(PipelineDeps{})
	pc := &pipelineContext{
		cfg:           PresetAPI(),
		input:         Input{},
		session:       NewSession("s1", "agent-1", "TestBot"),
		tr:            NewTraceRecorder(),
		threatCaution: true,
	}

	pipe.stageAuthority(context.Background(), pc)

	if pc.session.SecurityClaim == nil {
		t.Fatal("expected security claim on session")
	}
	if got := pc.session.SecurityClaim.Authority; got != core.AuthorityPeer {
		t.Fatalf("authority = %s, want %s", got, core.AuthorityPeer)
	}
	if !pc.session.SecurityClaim.ThreatDowngraded {
		t.Fatal("expected threat downgrade marker on security claim")
	}
	if pc.session.Authority != core.AuthorityPeer {
		t.Fatalf("session authority = %s, want %s", pc.session.Authority, core.AuthorityPeer)
	}
}
