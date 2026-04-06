package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"roboticus/internal/llm"
	"roboticus/testutil"
)

// ---------------------------------------------------------------------------
// Stringer coverage for enum types (all at 0%)
// ---------------------------------------------------------------------------

func TestPlannedAction_String(t *testing.T) {
	cases := []struct {
		action PlannedAction
		want   string
	}{
		{ActionInfer, "infer"},
		{ActionDelegate, "delegate"},
		{ActionSkillExec, "skill_exec"},
		{ActionRetrieve, "retrieve"},
		{ActionEscalate, "escalate"},
		{ActionWait, "wait"},
		{ActionNormRetry, "normalization_retry"},
		{ActionSurfaceBlock, "return_blocker"},
		{PlannedAction(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.action.String(); got != tc.want {
			t.Errorf("PlannedAction(%d).String() = %q, want %q", int(tc.action), got, tc.want)
		}
	}
}

func TestGovernorDecision_String(t *testing.T) {
	cases := []struct {
		d    GovernorDecision
		want string
	}{
		{GovernorAllow, "allow"},
		{GovernorThrottle, "throttle"},
		{GovernorDeny, "deny"},
		{GovernorDecision(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.d.String(); got != tc.want {
			t.Errorf("GovernorDecision(%d).String() = %q, want %q", int(tc.d), got, tc.want)
		}
	}
}

func TestTaskPlannedAction_String(t *testing.T) {
	cases := []struct {
		a    TaskPlannedAction
		want string
	}{
		{ActionAnswerDirectly, "answer_directly"},
		{ActionContinueCentralized, "continue_centralized"},
		{ActionInspectMemory, "inspect_memory"},
		{ActionComposeSkill, "compose_skill"},
		{ActionComposeSubagent, "compose_subagent"},
		{ActionDelegateToSpecialist, "delegate_to_specialist"},
		{TaskActionReturnBlocker, "return_blocker"},
		{TaskActionNormalizationRetry, "normalization_retry"},
		{TaskPlannedAction(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.a.String(); got != tc.want {
			t.Errorf("TaskPlannedAction(%d).String() = %q, want %q", int(tc.a), got, tc.want)
		}
	}
}

func TestLoopState_String(t *testing.T) {
	cases := []struct {
		s    LoopState
		want string
	}{
		{StateThinking, "thinking"},
		{StateActing, "acting"},
		{StateObserving, "observing"},
		{StatePersisting, "persisting"},
		{StateIdle, "idle"},
		{StateDone, "done"},
		{LoopState(99), "unknown(99)"},
	}
	for _, tc := range cases {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("LoopState(%d).String() = %q, want %q", int(tc.s), got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Interview tests (all at 0%)
// ---------------------------------------------------------------------------

func TestNewInterviewState(t *testing.T) {
	s := NewInterviewState("sess-1")
	if s.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", s.SessionID, "sess-1")
	}
	if s.StartedAt.IsZero() {
		t.Error("StartedAt should be set")
	}
	if s.Coverage() != 0 {
		t.Errorf("fresh state should have 0 coverage, got %d", s.Coverage())
	}
	if s.CanGenerate() {
		t.Error("fresh state should not be able to generate")
	}
}

func TestInterviewState_AddTurnAndCoverage(t *testing.T) {
	s := NewInterviewState("sess-2")

	categories := []InterviewCategory{
		CatIdentity, CatCommunication, CatProactiveness, CatDomain, CatBoundaries,
	}
	for i, cat := range categories {
		s.AddTurn(cat, "Q"+string(rune('0'+i)), "A"+string(rune('0'+i)))
	}

	if got := s.Coverage(); got != 5 {
		t.Errorf("Coverage() = %d, want 5", got)
	}
	if !s.CanGenerate() {
		t.Error("should be able to generate after 5 categories")
	}
	if len(s.Turns) != 5 {
		t.Errorf("Turns count = %d, want 5", len(s.Turns))
	}
}

func TestInterviewState_DuplicateCategory(t *testing.T) {
	s := NewInterviewState("sess-3")
	s.AddTurn(CatIdentity, "Q1", "A1")
	s.AddTurn(CatIdentity, "Q2", "A2") // same category again
	if s.Coverage() != 1 {
		t.Errorf("duplicate category should still count as 1, got %d", s.Coverage())
	}
	if len(s.Turns) != 2 {
		t.Errorf("should have 2 turns even with duplicate category, got %d", len(s.Turns))
	}
}

func TestBuildInterviewPrompt(t *testing.T) {
	prompt := BuildInterviewPrompt()
	if prompt == "" {
		t.Error("interview prompt should not be empty")
	}
	if len(prompt) < 100 {
		t.Errorf("interview prompt suspiciously short: %d chars", len(prompt))
	}
}

func TestGeneratePersonalityTOML(t *testing.T) {
	s := NewInterviewState("sess-4")
	s.AddTurn(CatIdentity, "What name?", "Call me Hal")
	s.AddTurn(CatBoundaries, "Any limits?", "No spending over $100")
	s.AddTurn(CatOperator, "Who are you?", "I'm an engineer")
	s.AddTurn(CatCommunication, "Style?", "Keep it brief")
	s.AddTurn(CatGoals, "Goals?", "Ship faster")
	s.AddTurn(CatIntegrations, "Platforms?", "Slack and GitHub")

	result := GeneratePersonalityTOML(s)

	expectedFiles := []string{"OS.toml", "FIRMWARE.toml", "OPERATOR.toml", "DIRECTIVES.toml"}
	for _, f := range expectedFiles {
		content, ok := result[f]
		if !ok {
			t.Errorf("missing file %q in result", f)
			continue
		}
		if content == "" {
			t.Errorf("%q is empty", f)
		}
	}

	// Verify identity answers are referenced in OS.toml.
	if os := result["OS.toml"]; os != "" {
		if !strContains(os, "Hal") {
			t.Error("OS.toml should reference interview identity answer")
		}
	}

	// Verify boundary rules in FIRMWARE.toml.
	if fw := result["FIRMWARE.toml"]; fw != "" {
		if !strContains(fw, "No spending over $100") {
			t.Error("FIRMWARE.toml should include boundary answer")
		}
	}

	// Verify goals in DIRECTIVES.toml.
	if dir := result["DIRECTIVES.toml"]; dir != "" {
		if !strContains(dir, "Ship faster") {
			t.Error("DIRECTIVES.toml should include goal")
		}
	}
}

func TestGeneratePersonalityTOML_EmptyState(t *testing.T) {
	s := NewInterviewState("sess-empty")
	result := GeneratePersonalityTOML(s)
	// Should produce all 4 files even with no answers.
	if len(result) != 4 {
		t.Errorf("expected 4 files, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// Obsidian tests (all at 0%)
// ---------------------------------------------------------------------------

func TestParseWikiLink(t *testing.T) {
	cases := []struct {
		raw     string
		target  string
		display string
		heading string
	}{
		{"[[Note]]", "Note", "Note", ""},
		{"[[Note|Custom]]", "Note", "Custom", ""},
		{"[[Note#Section]]", "Note", "Note", "Section"},
		{"[[Note#Section|Custom]]", "Note", "Custom", "Section"},
		{"[[]]", "", "", ""},
	}
	for _, tc := range cases {
		wl := ParseWikiLink(tc.raw)
		if wl.Target != tc.target {
			t.Errorf("ParseWikiLink(%q).Target = %q, want %q", tc.raw, wl.Target, tc.target)
		}
		if wl.Display != tc.display {
			t.Errorf("ParseWikiLink(%q).Display = %q, want %q", tc.raw, wl.Display, tc.display)
		}
		if wl.Heading != tc.heading {
			t.Errorf("ParseWikiLink(%q).Heading = %q, want %q", tc.raw, wl.Heading, tc.heading)
		}
	}
}

func TestTitleFromFilename(t *testing.T) {
	cases := []struct {
		name, want string
	}{
		{"My Note.md", "My Note"},
		{"simple.md", "simple"},
		{"no-extension", "no-extension"},
		{"dotted.file.name.md", "dotted.file.name"},
	}
	for _, tc := range cases {
		got := titleFromFilename(tc.name)
		if got != tc.want {
			t.Errorf("titleFromFilename(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestParseFrontmatter(t *testing.T) {
	content := `---
title: My Note
tags: projects
author: test
---
# Content here`

	fm := parseFrontmatter(content)
	if fm["title"] != "My Note" {
		t.Errorf("title = %q", fm["title"])
	}
	if fm["tags"] != "projects" {
		t.Errorf("tags = %q", fm["tags"])
	}
	if fm["author"] != "test" {
		t.Errorf("author = %q", fm["author"])
	}
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	fm := parseFrontmatter("# Just a heading\nSome content")
	if len(fm) != 0 {
		t.Errorf("expected empty map for no frontmatter, got %d entries", len(fm))
	}
}

func TestExtractTags(t *testing.T) {
	content := "Some text #project and #todo and #project again"
	tags := extractTags(content)
	if len(tags) != 2 {
		t.Errorf("expected 2 unique tags, got %d: %v", len(tags), tags)
	}
	tagSet := make(map[string]bool)
	for _, tag := range tags {
		tagSet[tag] = true
	}
	if !tagSet["project"] || !tagSet["todo"] {
		t.Errorf("expected project and todo tags, got %v", tags)
	}
}

func TestExtractTags_NoTags(t *testing.T) {
	tags := extractTags("No tags here at all")
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(tags))
	}
}

func TestExtractWikiLinkTargets(t *testing.T) {
	content := "See [[Note A]] and [[Note B#Section]] for details."
	targets := extractWikiLinkTargets(content)
	if len(targets) != 2 {
		t.Errorf("expected 2 targets, got %d: %v", len(targets), targets)
	}
}

func TestObsidianVault_ScanAndQuery(t *testing.T) {
	dir := t.TempDir()

	// Create test notes.
	_ = os.WriteFile(filepath.Join(dir, "Note A.md"), []byte(`---
title: Note Alpha
tags: project
---
# Note Alpha
Content of note A with #todo tag and link to [[Note B]].
`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "Note B.md"), []byte(`# Note B
Content of note B with [[Note A]] backlink.
`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("not a note"), 0o644)

	// Create a hidden dir that should be skipped.
	_ = os.MkdirAll(filepath.Join(dir, ".obsidian"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".obsidian", "config.md"), []byte("# Config"), 0o644)

	vault := NewObsidianVault(ObsidianConfig{VaultPath: dir})
	if vault.Root() != dir {
		t.Errorf("Root() = %q, want %q", vault.Root(), dir)
	}

	if err := vault.Scan(); err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	if vault.NoteCount() != 2 {
		t.Errorf("NoteCount() = %d, want 2", vault.NoteCount())
	}

	// ResolveWikiLink.
	path, ok := vault.ResolveWikiLink("Note Alpha")
	if !ok {
		t.Error("should resolve 'Note Alpha'")
	}
	if path != "Note A.md" {
		t.Errorf("resolved path = %q, want 'Note A.md'", path)
	}

	// ResolveWikiLink with heading anchor.
	_, ok = vault.ResolveWikiLink("Note Alpha#section")
	if !ok {
		t.Error("should resolve 'Note Alpha#section' by stripping anchor")
	}

	// Unresolvable link.
	_, ok = vault.ResolveWikiLink("Nonexistent")
	if ok {
		t.Error("should not resolve nonexistent note")
	}

	// GetBacklinks.
	backlinks := vault.GetBacklinks("Note B")
	if len(backlinks) == 0 {
		t.Error("Note B should have backlinks from Note A")
	}

	// SearchNotes.
	results := vault.SearchNotes("Alpha", 10)
	if len(results) == 0 {
		t.Error("search for 'Alpha' should find Note A")
	}

	// SearchNotes with default limit.
	results = vault.SearchNotes("Content", 0)
	if len(results) == 0 {
		t.Error("search for 'Content' should find notes")
	}

	// ReadNote.
	note := vault.ReadNote("Note B.md")
	if note == nil {
		t.Error("ReadNote should find Note B.md")
	}

	// ReadNote nonexistent.
	if vault.ReadNote("nope.md") != nil {
		t.Error("ReadNote for nonexistent should return nil")
	}

	// ListAllTags.
	tags := vault.ListAllTags()
	if len(tags) == 0 {
		t.Error("should find tags in vault")
	}
}

func TestObsidianVault_EmptyRoot(t *testing.T) {
	vault := NewObsidianVault(ObsidianConfig{VaultPath: ""})
	if err := vault.Scan(); err != nil {
		t.Errorf("Scan on empty root should not error, got %v", err)
	}
	if vault.NoteCount() != 0 {
		t.Errorf("empty vault should have 0 notes, got %d", vault.NoteCount())
	}
}

func TestObsidianVault_IgnoreDirs(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "templates"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "templates", "template.md"), []byte("# Template"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "real.md"), []byte("# Real Note"), 0o644)

	vault := NewObsidianVault(ObsidianConfig{
		VaultPath:  dir,
		IgnoreDirs: []string{"templates"},
	})
	if err := vault.Scan(); err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if vault.NoteCount() != 1 {
		t.Errorf("NoteCount = %d, want 1 (templates should be ignored)", vault.NoteCount())
	}
}

// ---------------------------------------------------------------------------
// Knowledge graph tests (0%)
// ---------------------------------------------------------------------------

func TestKnowledgeGraph_AddAndQuery(t *testing.T) {
	kg := NewKnowledgeGraph()

	kg.AddFact(Fact{ID: "1", Subject: "Go", Relation: "is_a", Object: "language", Confidence: 0.9})
	kg.AddFact(Fact{ID: "2", Subject: "Go", Relation: "created_by", Object: "Google", Confidence: 0.95})
	kg.AddFact(Fact{ID: "3", Subject: "Rust", Relation: "is_a", Object: "language", Confidence: 0.9})

	if kg.FactCount() != 3 {
		t.Errorf("FactCount = %d, want 3", kg.FactCount())
	}

	bySubject := kg.QueryBySubject("Go")
	if len(bySubject) != 2 {
		t.Errorf("QueryBySubject('Go') = %d facts, want 2", len(bySubject))
	}

	byRelation := kg.QueryByRelation("is_a")
	if len(byRelation) != 2 {
		t.Errorf("QueryByRelation('is_a') = %d facts, want 2", len(byRelation))
	}

	// Empty queries.
	if len(kg.QueryBySubject("Python")) != 0 {
		t.Error("QueryBySubject for unknown subject should return empty")
	}
	if len(kg.QueryByRelation("invented_at")) != 0 {
		t.Error("QueryByRelation for unknown relation should return empty")
	}
}

func TestKnowledgeGraph_FormatForPrompt_BudgetTruncation(t *testing.T) {
	kg := NewKnowledgeGraph()
	for i := 0; i < 20; i++ {
		kg.AddFact(Fact{
			ID:         fmt.Sprintf("f%d", i),
			Subject:    "BigSubject",
			Relation:   fmt.Sprintf("rel_%d", i),
			Object:     fmt.Sprintf("obj_%d", i),
			Confidence: 0.9,
		})
	}

	// Full budget should return all facts.
	full := kg.FormatForPrompt("BigSubject", 10000)
	if full == "" {
		t.Error("full budget should return content")
	}

	// Small budget should return fewer facts (truncated).
	small := kg.FormatForPrompt("BigSubject", 20) // 20*4=80 chars
	if small == "" {
		t.Error("small budget should still return some content")
	}
	if len(small) >= len(full) {
		t.Error("small budget should return less content than full budget")
	}

	// Tiny budget (1 token = 4 chars) may return empty.
	tiny := kg.FormatForPrompt("BigSubject", 1)
	_ = tiny // just ensure no panic
}

func TestWithKnowledgeGraph_RoundTrip(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.AddFact(Fact{ID: "1", Subject: "test", Relation: "for", Object: "coverage"})

	ctx := WithKnowledgeGraph(context.Background(), kg)
	got := KnowledgeGraphFromContext(ctx)
	if got == nil {
		t.Fatal("KnowledgeGraphFromContext returned nil")
	}
	if got.FactCount() != 1 {
		t.Errorf("FactCount = %d, want 1", got.FactCount())
	}
}

func TestKnowledgeGraphFromContext_Missing(t *testing.T) {
	got := KnowledgeGraphFromContext(context.Background())
	if got != nil {
		t.Error("should return nil for context without KnowledgeGraph")
	}
}

// ---------------------------------------------------------------------------
// Learning extractor tests (mostly 0%)
// ---------------------------------------------------------------------------

func TestLearningExtractor_ExtractFromTurn(t *testing.T) {
	le := NewLearningExtractor()

	// Successful tool use.
	patterns := le.ExtractFromTurn("run test", []string{"test result"}, true)
	if len(patterns) == 0 {
		t.Error("should extract pattern from successful tool use")
	}

	// Failed tool use.
	patterns = le.ExtractFromTurn("run test", []string{"error"}, false)
	foundProc := false
	for _, p := range patterns {
		if p.Pattern == "successful_tool_use" {
			t.Error("should not mark failed tool use as successful")
		}
	}
	_ = foundProc

	// Procedural query.
	patterns = le.ExtractFromTurn("how to deploy to production", nil, false)
	found := false
	for _, p := range patterns {
		if p.Pattern == "procedural_query" {
			found = true
		}
	}
	if !found {
		t.Error("should detect procedural query")
	}

	// Empty results.
	patterns = le.ExtractFromTurn("hello", nil, true)
	if len(patterns) != 0 {
		t.Errorf("no tools and no 'how to' should yield 0 patterns, got %d", len(patterns))
	}
}

func TestLearningExtractor_RegisterAndOutcome(t *testing.T) {
	le := NewLearningExtractor()

	le.Register(LearnedPattern{ID: "p1", Pattern: "test"})

	le.RecordOutcome("p1", true)
	le.RecordOutcome("p1", true)
	le.RecordOutcome("p1", false)

	rate := le.SuccessRate("p1")
	// 2 successes out of 3 total = ~0.667.
	if rate < 0.6 || rate > 0.7 {
		t.Errorf("SuccessRate = %f, want ~0.667", rate)
	}

	// Unknown pattern.
	le.RecordOutcome("unknown", true) // should not panic.
	if le.SuccessRate("unknown") != 0 {
		t.Error("unknown pattern should have 0 success rate")
	}
}

func TestDetectCandidateProcedures(t *testing.T) {
	le := NewLearningExtractor()

	// Not enough calls.
	procs := le.DetectCandidateProcedures([]ToolCallRecord{
		{ToolName: "a", Success: true},
	})
	if len(procs) != 0 {
		t.Error("too few calls should return nil")
	}

	// Create a repeating sequence: a→b→c appears twice.
	calls := []ToolCallRecord{
		{ToolName: "a", Success: true},
		{ToolName: "b", Success: true},
		{ToolName: "c", Success: true},
		{ToolName: "a", Success: true},
		{ToolName: "b", Success: true},
		{ToolName: "c", Success: true},
	}
	procs = le.DetectCandidateProcedures(calls)
	if len(procs) == 0 {
		t.Error("should detect repeating a→b→c procedure")
	}

	// All failures: no successful calls.
	failCalls := []ToolCallRecord{
		{ToolName: "a", Success: false},
		{ToolName: "b", Success: false},
		{ToolName: "c", Success: false},
		{ToolName: "a", Success: false},
	}
	procs = le.DetectCandidateProcedures(failCalls)
	if len(procs) != 0 {
		t.Error("all-failed calls should yield no procedures")
	}
}

func TestSynthesizeSkillMarkdown(t *testing.T) {
	proc := Procedure{
		Steps: []string{"fetch", "parse", "store"},
		Count: 5,
	}
	md := SynthesizeSkillMarkdown(proc)
	if md == "" {
		t.Error("should produce non-empty markdown")
	}
	if !strContains(md, "fetch-parse-store") {
		t.Error("should contain joined step name")
	}
	if !strContains(md, "5 successful") {
		t.Error("should mention count")
	}
	if !strContains(md, "1. Execute `fetch`") {
		t.Error("should list steps")
	}
}

func TestPersistLearnedSkill(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()
	proc := Procedure{
		Steps: []string{"toolA", "toolB", "toolC"},
		Count: 3,
	}
	// Should not panic or error.
	PersistLearnedSkill(ctx, store, proc)

	// Insert again to test ON CONFLICT.
	PersistLearnedSkill(ctx, store, proc)
}

func TestReinforceLearning(t *testing.T) {
	store := testutil.TempStore(t)
	ctx := context.Background()

	// First persist a skill to reinforce.
	proc := Procedure{Steps: []string{"x", "y", "z"}, Count: 1}
	PersistLearnedSkill(ctx, store, proc)

	// Reinforce with success and failure.
	ReinforceLearning(ctx, store, "x-y-z", true)
	ReinforceLearning(ctx, store, "x-y-z", false)
	// Nonexistent skill (should not panic).
	ReinforceLearning(ctx, store, "nonexistent", true)
}

func TestTruncateForLearning(t *testing.T) {
	if got := truncateForLearning("short", 100); got != "short" {
		t.Errorf("should not truncate short string, got %q", got)
	}
	long := "a very long string that exceeds the limit"
	if got := truncateForLearning(long, 10); len(got) != 10 {
		t.Errorf("should truncate to 10 chars, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Context builder: compact / extractTopic (low coverage)
// ---------------------------------------------------------------------------

func TestExtractTopic(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Hello world.", "Hello world."},
		{"First sentence. Second sentence.", "First sentence."},
		{"No period at all", "No period at all"},
		{"Question? Answer.", "Question?"},
		{"Exciting! More text.", "Exciting!"},
	}
	for _, tc := range cases {
		got := extractTopic(tc.input)
		if got != tc.want {
			t.Errorf("extractTopic(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}

	// Long input should be capped at ~120 chars.
	long := ""
	for i := 0; i < 200; i++ {
		long += "a"
	}
	got := extractTopic(long)
	if len(got) > 130 { // 120 + "..."
		t.Errorf("extractTopic should cap long input, got %d chars", len(got))
	}
}

func TestContextBuilder_Compact(t *testing.T) {
	cb := NewContextBuilder(DefaultContextConfig())

	msg := llm.Message{Role: "user", Content: "Hello, how are you?"}

	// Verbatim.
	if got := cb.compact(msg, StageVerbatim); got != msg.Content {
		t.Errorf("Verbatim: got %q, want %q", got, msg.Content)
	}

	// SelectiveTrim: short filler should be dropped.
	fillerMsg := llm.Message{Role: "user", Content: "ok"}
	if got := cb.compact(fillerMsg, StageSelectiveTrim); got != "" {
		t.Errorf("SelectiveTrim on filler should return empty, got %q", got)
	}

	// SelectiveTrim: system messages are never dropped.
	sysMsg := llm.Message{Role: "system", Content: "ok"}
	if got := cb.compact(sysMsg, StageSelectiveTrim); got != "ok" {
		t.Errorf("SelectiveTrim should preserve system messages, got %q", got)
	}

	// SemanticCompress: short messages pass through.
	shortMsg := llm.Message{Role: "user", Content: "short"}
	if got := cb.compact(shortMsg, StageSemanticCompress); got != "short" {
		t.Errorf("SemanticCompress on short msg should return as-is, got %q", got)
	}

	// SemanticCompress: long messages get compressed.
	longContent := ""
	for i := 0; i < 30; i++ {
		longContent += "This is a sentence with some content. "
	}
	longMsg := llm.Message{Role: "user", Content: longContent}
	got := cb.compact(longMsg, StageSemanticCompress)
	if len(got) >= len(longContent) {
		t.Errorf("SemanticCompress should reduce length")
	}

	// TopicExtract.
	topicMsg := llm.Message{Role: "user", Content: "First sentence here. Then more stuff follows."}
	got = cb.compact(topicMsg, StageTopicExtract)
	if got != "First sentence here." {
		t.Errorf("TopicExtract got %q, want first sentence", got)
	}

	// TopicExtract preserves system messages.
	sysTopic := llm.Message{Role: "system", Content: "System prompt text."}
	if got := cb.compact(sysTopic, StageTopicExtract); got != "System prompt text." {
		t.Errorf("TopicExtract should preserve system, got %q", got)
	}

	// Skeleton.
	skelMsg := llm.Message{Role: "user", Content: "any content"}
	if got := cb.compact(skelMsg, StageSkeleton); got != "[user message]" {
		t.Errorf("Skeleton got %q, want '[user message]'", got)
	}

	// Skeleton preserves system.
	if got := cb.compact(sysMsg, StageSkeleton); got != "ok" {
		t.Errorf("Skeleton should preserve system, got %q", got)
	}
}

func TestEstimateTokens(t *testing.T) {
	cb := NewContextBuilder(DefaultContextConfig())
	tokens := cb.estimateTokens("hello world!") // 12 chars / 4 = 3
	if tokens != 3 {
		t.Errorf("estimateTokens = %d, want 3", tokens)
	}

	// Zero CharsPerToken should fall back to 4.
	cb2 := NewContextBuilder(ContextConfig{CharsPerToken: 0})
	tokens = cb2.estimateTokens("hello world!") // 12/4 = 3
	if tokens != 3 {
		t.Errorf("estimateTokens with 0 CharsPerToken = %d, want 3", tokens)
	}
}

// ---------------------------------------------------------------------------
// Apps: Install from file (0%)
// ---------------------------------------------------------------------------

func TestAppManager_InstallFromFile(t *testing.T) {
	dir := t.TempDir()
	manifestContent := `[package]
name = "test-app"
version = "1.0.0"
description = "A test application"
author = "tester"

[profile]
agent_name = "TestAgent"

[requirements]
recommended_model = "gpt-4"
`
	manifestPath := filepath.Join(dir, "manifest.toml")
	_ = os.WriteFile(manifestPath, []byte(manifestContent), 0o644)

	am := NewAppManager(t.TempDir())
	app, err := am.Install(manifestPath)
	if err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if app.Manifest.Package.Name != "test-app" {
		t.Errorf("app name = %q", app.Manifest.Package.Name)
	}
	if !app.Enabled {
		t.Error("installed app should be enabled")
	}

	// Duplicate install should fail.
	_, err = am.Install(manifestPath)
	if err == nil {
		t.Error("duplicate install should error")
	}
}

func TestAppManager_InstallBadFile(t *testing.T) {
	am := NewAppManager(t.TempDir())

	// Nonexistent file.
	_, err := am.Install("/nonexistent/manifest.toml")
	if err == nil {
		t.Error("should error on nonexistent file")
	}

	// Invalid TOML.
	dir := t.TempDir()
	badPath := filepath.Join(dir, "bad.toml")
	_ = os.WriteFile(badPath, []byte("not valid toml [[["), 0o644)
	_, err = am.Install(badPath)
	if err == nil {
		t.Error("should error on bad TOML")
	}

	// Missing package name.
	emptyPath := filepath.Join(dir, "empty.toml")
	_ = os.WriteFile(emptyPath, []byte("[package]\nversion = \"1.0\""), 0o644)
	_, err = am.Install(emptyPath)
	if err == nil {
		t.Error("should error on missing package name")
	}
}

// ---------------------------------------------------------------------------
// AbuseTracker: evictOldest (0%)
// ---------------------------------------------------------------------------

func TestAbuseTracker_EvictOldest(t *testing.T) {
	store := testutil.TempStore(t)
	tracker := NewAbuseTracker(AbuseTrackerConfig{
		Enabled:             true,
		WindowMinutes:       5,
		SlowdownThreshold:   0.5,
		QuarantineThreshold: 0.8,
		MaxTrackedActors:    2, // small limit to trigger eviction
	}, store)
	ctx := context.Background()

	// Fill to capacity.
	_, _ = tracker.RecordSignal(ctx, AbuseSignal{ActorID: "actor1", SignalType: SignalRateBurst, Severity: 0.3})
	_, _ = tracker.RecordSignal(ctx, AbuseSignal{ActorID: "actor2", SignalType: SignalRateBurst, Severity: 0.3})
	// Adding a third should trigger eviction of the oldest.
	_, _ = tracker.RecordSignal(ctx, AbuseSignal{ActorID: "actor3", SignalType: SignalRateBurst, Severity: 0.3})

	// actor3 should still be tracked.
	score3 := tracker.GetActorScore("actor3")
	if score3 == 0 {
		t.Error("actor3 should have a score after recording signal")
	}
}

// ---------------------------------------------------------------------------
// foldHomoglyph (40% coverage — cover more branches)
// ---------------------------------------------------------------------------

func TestFoldHomoglyph(t *testing.T) {
	cases := []struct {
		input rune
		want  rune
	}{
		{'а', 'a'}, // Cyrillic а
		{'е', 'e'}, // Cyrillic е
		{'о', 'o'}, // Cyrillic о
		{'р', 'p'}, // Cyrillic р
		{'с', 'c'}, // Cyrillic с
		{'у', 'y'}, // Cyrillic у
		{'х', 'x'}, // Cyrillic х
		{'А', 'A'}, // Cyrillic А
		{'В', 'B'}, // Cyrillic В
		{'Е', 'E'}, // Cyrillic Е
		{'К', 'K'}, // Cyrillic К
		{'М', 'M'}, // Cyrillic М
		{'Н', 'H'}, // Cyrillic Н
		{'О', 'O'}, // Cyrillic О
		{'z', 'z'}, // Latin (passthrough)
	}
	for _, tc := range cases {
		got := foldHomoglyph(tc.input)
		if got != tc.want {
			t.Errorf("foldHomoglyph(%U) = %c, want %c", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// stageFromExcess edge cases (context.go)
// ---------------------------------------------------------------------------

func TestStageFromExcess_Boundaries(t *testing.T) {
	// Test exact boundary values.
	if stageFromExcess(1.0) != StageVerbatim {
		t.Error("1.0 should be Verbatim")
	}
	if stageFromExcess(1.01) != StageSelectiveTrim {
		t.Error("1.01 should be SelectiveTrim")
	}
	if stageFromExcess(1.5) != StageSelectiveTrim {
		t.Error("1.5 should be SelectiveTrim")
	}
	if stageFromExcess(1.51) != StageSemanticCompress {
		t.Error("1.51 should be SemanticCompress")
	}
	if stageFromExcess(2.5) != StageSemanticCompress {
		t.Error("2.5 should be SemanticCompress")
	}
	if stageFromExcess(2.51) != StageTopicExtract {
		t.Error("2.51 should be TopicExtract")
	}
	if stageFromExcess(4.01) != StageSkeleton {
		t.Error("4.01 should be Skeleton")
	}
}

// ---------------------------------------------------------------------------
// isSocialFiller additional coverage
// ---------------------------------------------------------------------------

func TestIsSocialFiller_MoreCases(t *testing.T) {
	// Various filler words.
	fillers := []string{"hi", "hey", "thank you", "ok", "okay", "got it", "sure", "yes", "no", "ack", "np"}
	for _, f := range fillers {
		if !isSocialFiller(f) {
			t.Errorf("%q should be filler", f)
		}
	}
	// Non-filler.
	if isSocialFiller("something random") {
		t.Error("non-filler short text should not match")
	}
	// Exactly at length boundary (40 chars).
	long39 := "a]b]c]d]e]f]g]h]i]j]k]l]m]n]o]p]q]r]s" // 39 chars
	if isSocialFiller(long39) {
		t.Error("39-char non-filler string should not match")
	}
}

// ---------------------------------------------------------------------------
// isMemoryOnlyQuery
// ---------------------------------------------------------------------------

func TestIsMemoryOnlyQuery(t *testing.T) {
	positives := []string{
		"what did we discuss yesterday",
		"do you remember the last meeting",
		"last time we talked about refactoring",
		"previously we agreed on the approach",
		"you mentioned earlier that Go is fast",
		"our earlier conversation about testing",
	}
	for _, q := range positives {
		if !isMemoryOnlyQuery(q) {
			t.Errorf("expected memory query: %q", q)
		}
	}

	negatives := []string{
		"how do I deploy",
		"write a test for this",
		"what is Go",
	}
	for _, q := range negatives {
		if isMemoryOnlyQuery(q) {
			t.Errorf("should not be memory query: %q", q)
		}
	}
}

// ---------------------------------------------------------------------------
// countNonSystem
// ---------------------------------------------------------------------------

func TestCountNonSystem(t *testing.T) {
	msgs := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "system", Content: "sys2"},
	}
	if got := countNonSystem(msgs); got != 2 {
		t.Errorf("countNonSystem = %d, want 2", got)
	}
	if got := countNonSystem(nil); got != 0 {
		t.Errorf("countNonSystem(nil) = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func strContains(s, substr string) bool {
	return len(s) >= len(substr) && len(substr) > 0 &&
		strHas(s, substr)
}

func strHas(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
