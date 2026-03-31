package pipeline

import "testing"

func TestResolveAuthority_APIKey_Coverage(t *testing.T) {
	got := ResolveAuthority(AuthorityAPIKey, nil)
	if got.String() != "creator" {
		t.Errorf("APIKey = %s, want creator", got.String())
	}
}

func TestResolveAuthority_Channel_Coverage(t *testing.T) {
	got := ResolveAuthority(AuthorityChannel, nil)
	if got.String() != "external" {
		t.Errorf("Channel = %s, want external", got.String())
	}
}

func TestPresetAPI_Coverage(t *testing.T) {
	cfg := PresetAPI()
	if !cfg.InjectionDefense {
		t.Error("should enable injection defense")
	}
	if !cfg.PostTurnIngest {
		t.Error("should enable post-turn ingest")
	}
	if cfg.InferenceMode != InferenceStandard {
		t.Errorf("mode = %v", cfg.InferenceMode)
	}
}

func TestPresetCron_Coverage(t *testing.T) {
	cfg := PresetCron()
	if cfg.SessionResolution != SessionDedicated {
		t.Errorf("session = %v", cfg.SessionResolution)
	}
}

func TestPresetChannel_Coverage(t *testing.T) {
	cfg := PresetChannel("discord")
	if cfg.ChannelLabel != "discord" {
		t.Errorf("label = %s", cfg.ChannelLabel)
	}
}

func TestPresetStreaming_Coverage(t *testing.T) {
	cfg := PresetStreaming()
	if cfg.InferenceMode != InferenceStreaming {
		t.Errorf("mode = %v", cfg.InferenceMode)
	}
}

func TestInferenceMode_Values(t *testing.T) {
	if InferenceStandard == InferenceStreaming {
		t.Error("standard and streaming should differ")
	}
}

func TestSessionResolution_Values(t *testing.T) {
	if SessionFromBody == SessionFromChannel {
		t.Error("should differ")
	}
}
