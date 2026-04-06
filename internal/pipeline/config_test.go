package pipeline

import (
	"testing"

	"roboticus/internal/core"
)

func TestPresetAPI(t *testing.T) {
	cfg := PresetAPI()
	if !cfg.InjectionDefense {
		t.Error("API preset should have injection defense")
	}
	if !cfg.DedupTracking {
		t.Error("API preset should have dedup tracking")
	}
	if cfg.InferenceMode != InferenceStandard {
		t.Error("API preset should use standard inference")
	}
	if !cfg.NicknameRefinement {
		t.Error("API preset should have nickname refinement")
	}
	if cfg.ChannelLabel != "api" {
		t.Errorf("expected channel label 'api', got %q", cfg.ChannelLabel)
	}
}

func TestPresetStreaming(t *testing.T) {
	cfg := PresetStreaming()
	if cfg.InferenceMode != InferenceStreaming {
		t.Error("streaming preset should use streaming inference")
	}
	if cfg.NicknameRefinement {
		t.Error("streaming preset should not have nickname refinement")
	}
	if cfg.GuardSet != GuardSetStream {
		t.Error("streaming preset should use stream guard set")
	}
}

func TestPresetChannel(t *testing.T) {
	cfg := PresetChannel("telegram")
	if !cfg.SpecialistControls {
		t.Error("channel preset should have specialist controls")
	}
	if !cfg.SkillFirstEnabled {
		t.Error("channel preset should have skill-first")
	}
	if cfg.AuthorityMode != AuthorityChannel {
		t.Error("channel preset should use channel authority")
	}
	if cfg.ChannelLabel != "telegram" {
		t.Errorf("expected channel label 'telegram', got %q", cfg.ChannelLabel)
	}
}

func TestPresetCron(t *testing.T) {
	cfg := PresetCron()
	if cfg.DedupTracking {
		t.Error("cron preset should not have dedup tracking")
	}
	if cfg.DelegatedExecution {
		t.Error("cron preset should not have delegated execution")
	}
	if cfg.AuthorityMode != AuthoritySelfGen {
		t.Error("cron preset should use self-generated authority")
	}
}

func TestResolveAuthority_APIKey(t *testing.T) {
	auth := ResolveAuthority(AuthorityAPIKey, nil)
	if auth != core.AuthorityCreator {
		t.Errorf("API key should resolve to Creator, got %v", auth)
	}
}

func TestResolveAuthority_Channel_Allowlisted(t *testing.T) {
	claim := &ChannelClaimContext{
		SenderID:          "user123",
		SenderInAllowlist: true,
	}
	auth := ResolveAuthority(AuthorityChannel, claim)
	if auth != core.AuthorityCreator {
		t.Errorf("allowlisted sender should be Creator, got %v", auth)
	}
}

func TestResolveAuthority_Channel_Trusted(t *testing.T) {
	claim := &ChannelClaimContext{
		SenderID:         "user456",
		TrustedSenderIDs: []string{"user456", "user789"},
	}
	auth := ResolveAuthority(AuthorityChannel, claim)
	if auth != core.AuthorityPeer {
		t.Errorf("trusted sender should be Peer, got %v", auth)
	}
}

func TestResolveAuthority_Channel_Unknown(t *testing.T) {
	claim := &ChannelClaimContext{
		SenderID: "stranger",
	}
	auth := ResolveAuthority(AuthorityChannel, claim)
	if auth != core.AuthorityExternal {
		t.Errorf("unknown sender should be External, got %v", auth)
	}
}
