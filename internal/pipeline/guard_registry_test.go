package pipeline

import "testing"

func TestGuardRegistry_Chain(t *testing.T) {
	tests := []struct {
		name      string
		preset    GuardSetPreset
		wantCount int
	}{
		{"full set", GuardSetFull, 28},
		{"stream set", GuardSetStream, 7},
		{"cached set", GuardSetCached, 25}, // Includes Go-only additive guards such as placeholder_content
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

func TestPipeline_GuardsForPreset_UsesRegistryWhenNoCustomGuards(t *testing.T) {
	pipe := New(PipelineDeps{})

	full := pipe.guardsForPreset(GuardSetFull)
	cached := pipe.guardsForPreset(GuardSetCached)
	stream := pipe.guardsForPreset(GuardSetStream)

	if full == nil || cached == nil || stream == nil {
		t.Fatal("expected preset guard chains to resolve from registry")
	}
	if full.Len() != NewDefaultGuardRegistry().Chain(GuardSetFull).Len() {
		t.Fatalf("full preset len = %d, want registry len %d", full.Len(), NewDefaultGuardRegistry().Chain(GuardSetFull).Len())
	}
	if cached.Len() != NewDefaultGuardRegistry().Chain(GuardSetCached).Len() {
		t.Fatalf("cached preset len = %d, want registry len %d", cached.Len(), NewDefaultGuardRegistry().Chain(GuardSetCached).Len())
	}
	if stream.Len() != NewDefaultGuardRegistry().Chain(GuardSetStream).Len() {
		t.Fatalf("stream preset len = %d, want registry len %d", stream.Len(), NewDefaultGuardRegistry().Chain(GuardSetStream).Len())
	}
	if none := pipe.guardsForPreset(GuardSetNone); none != nil {
		t.Fatal("GuardSetNone should resolve to nil guard chain")
	}
}

func TestPipeline_GuardsForPreset_PreservesCustomGuardChains(t *testing.T) {
	custom := NewGuardChain(&EmptyResponseGuard{})
	pipe := New(PipelineDeps{Guards: custom})

	if got := pipe.guardsForPreset(GuardSetFull); got != custom {
		t.Fatal("custom guard chain should be preserved for explicit pipeline deps")
	}
	if got := pipe.guardsForPreset(GuardSetCached); got != custom {
		t.Fatal("custom guard chain should be preserved across presets for explicit pipeline deps")
	}
	if got := pipe.guardsForPreset(GuardSetNone); got != nil {
		t.Fatal("GuardSetNone should still disable custom guard chain")
	}
}
