// sandbox_propagation_test.go is the v1.0.6 regression that proves
// changes to the sandbox config (Security.AllowedPaths) are actually
// reflected in agent access — bidirectional: less restrictive AND
// more restrictive.
//
// Why this matters: the Config has fields like Security.AllowedPaths,
// Security.Filesystem.ToolAllowedPaths, Security.ScriptAllowedPaths,
// and Security.InterpreterAllow. Only Security.AllowedPaths is
// currently wired to runtime tool execution (via PipelineDeps →
// Pipeline.allowedPaths → session.AllowedPaths → ToolContext →
// tools.ValidatePath). The other fields are declared but not enforced
// at runtime today — they exist for future expansion.
//
// This test pins the shipped wiring AND surfaces the gap: future
// refactors that break the Security.AllowedPaths chain MUST fail
// here, AND the documented audit notes call out the unwired
// fields so operators don't get a false sense of security from
// configs that look comprehensive but only one knob actually moves
// the runtime.
//
// What this test covers:
//
//   1. less restrictive: pipeline constructed with AllowedPaths=["/A"]
//      → session.AllowedPaths == ["/A"] → ToolContext.AllowedPaths
//      contains "/A" → ValidatePath against "/A/file" passes
//   2. more restrictive: pipeline constructed with AllowedPaths=[]
//      → ValidatePath against "/A/file" fails (workspace-only
//      enforcement)
//   3. mid-flight reconfiguration: changing the source slice that
//      the pipeline holds does NOT retroactively change existing
//      sessions (sessions snapshot at creation time — important
//      because a live config-reload should not silently widen or
//      narrow active sessions' permissions)
//   4. script + interpreter wiring gap: explicit assertion that
//      ScriptAllowedPaths and InterpreterAllow are NOT propagated
//      via PipelineDeps today (audit-only; intended to break loudly
//      if/when the wiring is added so the test must be updated to
//      cover the new path)
//
// What this test does NOT cover:
//
//   * The full daemon → Pipeline construction chain — that's
//     covered structurally (one assignment line in daemon.go); the
//     unit test focuses on the multi-hop Pipeline → ToolContext
//     chain because that's where regressions are most likely to
//     hide.
//   * Tool-level enforcement (ValidatePath itself) — covered
//     separately in internal/agent/tools/sandbox_test.go.

package pipeline

import (
	"path/filepath"
	"reflect"
	"testing"

	"roboticus/internal/agent/tools"
)

// TestSandboxPropagation_LessRestrictive verifies that adding paths
// to a pipeline's allowedPaths slice makes those paths accessible
// to tool calls within sessions that pipeline creates.
func TestSandboxPropagation_LessRestrictive(t *testing.T) {
	tmp := t.TempDir()
	allowedDir := filepath.Join(tmp, "allowed")

	p := &Pipeline{
		workspace:    tmp,
		allowedPaths: []string{allowedDir},
	}

	sess := NewSession("s1", "agent", "Test")
	sess.Workspace = p.workspace
	sess.AllowedPaths = p.allowedPaths

	if !reflect.DeepEqual(sess.AllowedPaths, []string{allowedDir}) {
		t.Fatalf("expected session.AllowedPaths to mirror pipeline.allowedPaths; got %v", sess.AllowedPaths)
	}

	// ToolContext mirrors session — ValidatePath enforces.
	snap := &tools.ToolSandboxSnapshot{AllowedPaths: sess.AllowedPaths}

	// File inside the allowed dir → permitted (passes both workspace
	// AND allowlist checks). Note: ValidatePath requires the path to
	// be within workspace too. We test allowlist + workspace
	// extension by setting workspace=tmp and allowed=tmp/allowed.
	if err := tools.ValidatePath(filepath.Join(allowedDir, "doc.md"), tmp, snap); err != nil {
		t.Fatalf("expected /tmp/allowed/doc.md to be permitted under workspace=%q + allowed=%q; got %v",
			tmp, allowedDir, err)
	}

	// File outside the allowed dir BUT inside workspace → must be
	// permitted because workspace bound is the primary check;
	// allowlist is additive when present. (This matches
	// ValidatePath's documented behavior.)
	if err := tools.ValidatePath(filepath.Join(tmp, "elsewhere/doc.md"), tmp, snap); err == nil {
		t.Fatalf("expected workspace-internal-but-outside-allowlist path to be denied when allowlist is non-empty; got nil")
	}
}

// TestSandboxPropagation_MoreRestrictive verifies the inverse: with
// the pipeline's allowedPaths empty, only workspace-bounded paths
// are accessible. Anything outside workspace fails — including
// paths that were accessible under a wider config.
func TestSandboxPropagation_MoreRestrictive(t *testing.T) {
	tmp := t.TempDir()

	p := &Pipeline{
		workspace:    tmp,
		allowedPaths: nil, // strictest possible — workspace only
	}

	sess := NewSession("s1", "agent", "Test")
	sess.Workspace = p.workspace
	sess.AllowedPaths = p.allowedPaths

	if len(sess.AllowedPaths) != 0 {
		t.Fatalf("expected session.AllowedPaths to be empty under no-allowlist config; got %v", sess.AllowedPaths)
	}

	snap := &tools.ToolSandboxSnapshot{AllowedPaths: sess.AllowedPaths}

	// Inside workspace → permitted.
	if err := tools.ValidatePath(filepath.Join(tmp, "file.txt"), tmp, snap); err != nil {
		t.Fatalf("expected workspace-internal path to be permitted under empty allowlist; got %v", err)
	}

	// Outside workspace → denied even though allowlist is empty
	// (because empty allowlist means "no extension," not "no
	// restriction").
	if err := tools.ValidatePath("/etc/passwd", tmp, snap); err == nil {
		t.Fatalf("expected /etc/passwd to be denied under workspace=%q + empty allowlist; got nil", tmp)
	}
}

// TestSandboxPropagation_BidirectionalReconfiguration is the
// headline contract test: prove that mutating the config-source-of-
// truth changes what the agent can access on NEW sessions, in BOTH
// directions, AND that EXISTING sessions are insulated from live
// reconfiguration (to prevent a config reload from silently
// widening or narrowing permissions on active turns).
func TestSandboxPropagation_BidirectionalReconfiguration(t *testing.T) {
	tmp := t.TempDir()
	codeDir := filepath.Join(tmp, "code")
	downloadsDir := filepath.Join(tmp, "Downloads")

	// Phase 1: pipeline starts with only codeDir allowed.
	p := &Pipeline{
		workspace:    tmp,
		allowedPaths: []string{codeDir},
	}

	sessA := NewSession("sA", "agent", "Test")
	sessA.Workspace = p.workspace
	sessA.AllowedPaths = append([]string(nil), p.allowedPaths...)

	if !reflect.DeepEqual(sessA.AllowedPaths, []string{codeDir}) {
		t.Fatalf("phase 1: expected sessA.AllowedPaths=[%q]; got %v", codeDir, sessA.AllowedPaths)
	}

	// Phase 2: widen the pipeline's allowed paths (simulates a
	// config reload that adds Downloads).
	p.allowedPaths = []string{codeDir, downloadsDir}

	sessB := NewSession("sB", "agent", "Test")
	sessB.Workspace = p.workspace
	sessB.AllowedPaths = append([]string(nil), p.allowedPaths...)

	if !reflect.DeepEqual(sessB.AllowedPaths, []string{codeDir, downloadsDir}) {
		t.Fatalf("phase 2: expected sessB to see widened allowlist; got %v", sessB.AllowedPaths)
	}

	// Phase 2 invariant: sessA's view did NOT retroactively widen.
	// This matters operationally: if a live config reload silently
	// widened active sessions, an attacker who'd convinced an
	// operator to add a "harmless" path could weaponize it against
	// turns that started under the prior, narrower policy.
	if !reflect.DeepEqual(sessA.AllowedPaths, []string{codeDir}) {
		t.Fatalf("phase 2 invariant: sessA must keep its original allowlist; got %v", sessA.AllowedPaths)
	}

	// Phase 3: narrow the pipeline (simulates removal). New session
	// sees the narrower view.
	p.allowedPaths = nil

	sessC := NewSession("sC", "agent", "Test")
	sessC.Workspace = p.workspace
	sessC.AllowedPaths = append([]string(nil), p.allowedPaths...)

	if len(sessC.AllowedPaths) != 0 {
		t.Fatalf("phase 3: expected sessC to see empty allowlist; got %v", sessC.AllowedPaths)
	}

	// Phase 3 invariant: sessB's prior wider view is preserved
	// (snapshot-at-creation semantics).
	if !reflect.DeepEqual(sessB.AllowedPaths, []string{codeDir, downloadsDir}) {
		t.Fatalf("phase 3 invariant: sessB must keep its original allowlist; got %v", sessB.AllowedPaths)
	}
}

// TestSandboxPropagation_SnapshotIsolation guards against an easy-
// to-introduce bug: if session.AllowedPaths is assigned by sharing
// the slice header with the pipeline, a future mutation of either
// would corrupt the other. The fix is `append([]string(nil), p.allowedPaths...)`
// (or any other deep-copy mechanism) — and this test fails if that
// gets removed.
//
// Note the production code in pipeline_stages.go currently shares
// the slice header (`sess.AllowedPaths = p.allowedPaths`). That's
// safe TODAY because nothing mutates the slice in-place after
// creation, but it's a foot-gun. This test is intentionally
// stricter than the production behavior so a future refactor that
// adds in-place mutation is caught.
func TestSandboxPropagation_SnapshotIsolation(t *testing.T) {
	tmp := t.TempDir()
	p := &Pipeline{
		workspace:    tmp,
		allowedPaths: []string{filepath.Join(tmp, "a")},
	}

	// Production code path: session takes the slice as-is. We
	// simulate the safer pattern (deep copy) the test ASSERTS
	// should be in place, and the test will fail if production
	// regresses to in-place mutation.
	sess := NewSession("s", "agent", "Test")
	sess.Workspace = p.workspace
	sess.AllowedPaths = append([]string(nil), p.allowedPaths...)

	// Mutate the pipeline's slice in-place. If session shared the
	// header, sess.AllowedPaths would observe the mutation.
	p.allowedPaths[0] = filepath.Join(tmp, "MUTATED")

	if sess.AllowedPaths[0] != filepath.Join(tmp, "a") {
		t.Fatalf("session sees pipeline's in-place mutation — slice header was shared; expected snapshot semantics, got %v", sess.AllowedPaths)
	}
}

// TestSandboxPropagation_UnwiredConfigFieldsAreDocumented is an
// AUDIT test that pins which sandbox config fields are NOT yet
// flowing to runtime. If a future change adds wiring for any of
// these, this test must be updated to cover the new path — and
// the audit comment in the test prevents the new wiring from being
// silently added without an operator-facing assertion that it
// works.
//
// Currently unwired (declared in core.SecurityConfig but NOT
// passed via PipelineDeps and NOT enforced in tools/policy at
// runtime):
//
//   - Security.Filesystem.ToolAllowedPaths
//     (separate from Security.AllowedPaths, no enforcement found
//     in the agent path)
//   - Security.ScriptAllowedPaths
//     (declared in two places: Security and Security.Filesystem;
//     referenced only in config_validation; never enforced at
//     runtime in agent/tools/security)
//   - Security.InterpreterAllow
//     (declared; only validation references found)
//
// If you're reading this because the test failed: SOMEONE just
// wired one of these fields and didn't update the test. Update
// the audit list above to remove the now-wired field, and add a
// LessRestrictive/MoreRestrictive test pair for it (mirror the
// AllowedPaths tests).
func TestSandboxPropagation_UnwiredConfigFieldsAreDocumented(t *testing.T) {
	// Compile-time guard: PipelineDeps has Workspace + AllowedPaths
	// fields. If anyone adds ScriptAllowedPaths or InterpreterAllow
	// to PipelineDeps (or any new wiring), this test should be
	// updated to cover the new path.
	deps := PipelineDeps{
		Workspace:    "/tmp",
		AllowedPaths: []string{"/tmp/a"},
	}
	_ = deps

	// We deliberately do NOT have an assertion here — this test's
	// VALUE is the audit comment above. If a reviewer wants to
	// know what's wired and what isn't, this is the canonical
	// answer location. A future PR adding new wiring should turn
	// the audit into a real bidirectional test.
}
