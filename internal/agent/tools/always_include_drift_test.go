package tools

import (
	"reflect"
	"sort"
	"testing"

	"roboticus/internal/core"
)

// TestToolSearchAlwaysIncludeReconciled prevents the v1.0.8 audit's
// "AlwaysInclude pin drift" regression. Tool admission relies on TWO
// sources of truth for the operational always-include set:
//
//  1. agenttools.DefaultToolSearchConfig() — the in-package fallback used
//     by tests, ad-hoc tooling, and the pipeline's defensive defaults.
//  2. core.DefaultConfig().ToolSearch.AlwaysInclude — the production
//     defaults baked into roboticus.toml startup.
//
// When these drift, an operationally important tool (e.g. obsidian_write)
// can be pinned in one path and silently dropped in the other depending on
// which construction route assembled the config — exactly the failure mode
// that motivated the audit. This test pins the contract: the two lists
// must contain the same set of names.
func TestToolSearchAlwaysIncludeReconciled(t *testing.T) {
	pkg := DefaultToolSearchConfig().AlwaysInclude
	cfg := core.DefaultConfig().ToolSearch.AlwaysInclude

	pkgSorted := append([]string(nil), pkg...)
	cfgSorted := append([]string(nil), cfg...)
	sort.Strings(pkgSorted)
	sort.Strings(cfgSorted)

	if !reflect.DeepEqual(pkgSorted, cfgSorted) {
		t.Fatalf("AlwaysInclude drift detected\n  agenttools.DefaultToolSearchConfig(): %v\n  core.DefaultConfig().ToolSearch:      %v", pkgSorted, cfgSorted)
	}
	for _, want := range []string{"obsidian_write", "get_runtime_context", "introspect", "list-available-skills", "compose-skill", "orchestrate-subagents"} {
		found := false
		for _, n := range pkgSorted {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in AlwaysInclude defaults; lost during reconciliation", want)
		}
	}
}
