package agent

import "testing"

func TestNewToolRegistry_Creates(t *testing.T) {
	reg := NewToolRegistry()
	if reg == nil {
		t.Fatal("nil")
	}
	// Exercise ToolDefs and Names methods.
	_ = reg.ToolDefs()
	_ = reg.Names()
}

func TestNewLoop_Creates(t *testing.T) {
	cfg := LoopConfig{MaxTurns: 5}
	deps := LoopDeps{}
	loop := NewLoop(cfg, deps)
	if loop == nil {
		t.Fatal("nil")
	}
}

func TestDefaultLoopConfig_Values(t *testing.T) {
	cfg := DefaultLoopConfig()
	if cfg.MaxTurns <= 0 {
		t.Error("max turns should be positive")
	}
}
