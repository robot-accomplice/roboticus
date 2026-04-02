package pipeline

import "testing"

func TestGuardRegistry_Chain(t *testing.T) {
	tests := []struct {
		name      string
		preset    GuardSetPreset
		wantCount int
	}{
		{"full set", GuardSetFull, 17},
		{"stream set", GuardSetStream, 5},
		{"cached set", GuardSetCached, 17},
		{"none set", GuardSetNone, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewDefaultGuardRegistry()
			chain := reg.Chain(tt.preset)
			if chain.Len() != tt.wantCount {
				t.Errorf("Chain(%v).Len() = %d, want %d", tt.preset, chain.Len(), tt.wantCount)
			}
		})
	}
}

func TestGuardRegistry_Get(t *testing.T) {
	reg := NewDefaultGuardRegistry()

	g, ok := reg.Get("empty_response")
	if !ok {
		t.Fatal("expected to find empty_response guard")
	}
	if g.Name() != "empty_response" {
		t.Errorf("Name() = %q, want %q", g.Name(), "empty_response")
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("expected nonexistent guard to not be found")
	}
}

func TestGuardRegistry_Register(t *testing.T) {
	reg := NewGuardRegistry()
	reg.Register(&EmptyResponseGuard{})

	if _, ok := reg.Get("empty_response"); !ok {
		t.Error("registered guard not found")
	}
}
