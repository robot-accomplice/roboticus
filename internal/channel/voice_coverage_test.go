package channel

import "testing"

func TestNewVoiceAdapter(t *testing.T) {
	cfg := VoiceConfig{}
	adapter := NewVoiceAdapter(cfg)
	if adapter == nil {
		t.Fatal("nil")
	}
	if adapter.PlatformName() != "voice" {
		t.Errorf("platform = %s", adapter.PlatformName())
	}
}
