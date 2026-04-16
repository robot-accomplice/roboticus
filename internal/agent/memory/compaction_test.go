package memory

import (
	"fmt"
	"strings"
	"testing"
)

func makeEntry(source, content string, importance float32, ageSecs uint64, relevance float32) ContextEntry {
	return ContextEntry{
		Source:     source,
		Content:    content,
		Importance: importance,
		AgeSeconds: ageSecs,
		Relevance:  relevance,
	}
}

// TestCompact_EmptyInputProducesEmptyOutput mirrors the Rust
// empty_input_produces_empty_output test. An empty slice must not
// produce section headers or whitespace — the compacted block is an
// empty string and the retained/dropped counts are both zero.
func TestCompact_EmptyInputProducesEmptyOutput(t *testing.T) {
	got := Compact(nil, DefaultCompactionConfig())
	if got.Text != "" {
		t.Errorf("Text = %q; want empty", got.Text)
	}
	if got.EntriesRetained != 0 {
		t.Errorf("EntriesRetained = %d; want 0", got.EntriesRetained)
	}
	if got.EntriesDropped != 0 {
		t.Errorf("EntriesDropped = %d; want 0", got.EntriesDropped)
	}
}

// TestCompact_SinglePassesThrough mirrors single_entry_passes_through.
// One entry should survive compaction verbatim (modulo compression) and
// appear under its tier's section header.
func TestCompact_SinglePassesThrough(t *testing.T) {
	entries := []ContextEntry{
		makeEntry("episodic", "User asked about workspace cleanup", 5, 300, 0.8),
	}
	got := Compact(entries, DefaultCompactionConfig())
	if !strings.Contains(got.Text, "workspace cleanup") {
		t.Errorf("Text missing content: %q", got.Text)
	}
	if got.EntriesRetained != 1 {
		t.Errorf("EntriesRetained = %d; want 1", got.EntriesRetained)
	}
	if got.EntriesDropped != 0 {
		t.Errorf("EntriesDropped = %d; want 0", got.EntriesDropped)
	}
}

// TestCompact_DuplicatesAreRemoved mirrors duplicates_are_removed.
// Exact-text duplicates across different source tiers should collapse
// to a single entry — lower index wins, so the episodic copy survives
// and the ambient copy is dropped.
func TestCompact_DuplicatesAreRemoved(t *testing.T) {
	entries := []ContextEntry{
		makeEntry("episodic", "Agent cleaned up workspace files", 5, 300, 0.8),
		makeEntry("ambient", "Agent cleaned up workspace files", 5, 300, 0.0),
	}
	got := Compact(entries, DefaultCompactionConfig())
	if got.EntriesRetained != 1 {
		t.Errorf("EntriesRetained = %d; want 1 (duplicate should be removed)", got.EntriesRetained)
	}
	if got.EntriesDropped != 1 {
		t.Errorf("EntriesDropped = %d; want 1", got.EntriesDropped)
	}
}

// TestCompact_BudgetEnforced mirrors budget_enforced. With 100 input
// entries and a 100-token budget, the compactor must drop enough
// entries to stay at or below the budget (with a small estimator
// margin).
func TestCompact_BudgetEnforced(t *testing.T) {
	entries := make([]ContextEntry, 0, 100)
	for i := 0; i < 100; i++ {
		entries = append(entries, makeEntry(
			"episodic",
			fmt.Sprintf("Memory entry number %d with some content to take up space", i),
			5, uint64(i*60), 0.5,
		))
	}
	cfg := DefaultCompactionConfig()
	cfg.MaxTokens = 100
	got := Compact(entries, cfg)
	if got.Tokens > 110 {
		t.Errorf("Tokens = %d; want <= 110 (small estimator margin)", got.Tokens)
	}
	if got.EntriesRetained >= 100 {
		t.Errorf("EntriesRetained = %d; want < 100", got.EntriesRetained)
	}
	if got.EntriesDropped == 0 {
		t.Errorf("EntriesDropped = 0; want > 0 with tight budget")
	}
}

// TestCompact_HighPriorityRetainedFirst mirrors
// high_priority_entries_retained_first. Under a very tight budget, the
// oldest low-relevance entry must be dropped in favor of recent or
// highly-relevant entries.
func TestCompact_HighPriorityRetainedFirst(t *testing.T) {
	entries := []ContextEntry{
		makeEntry("episodic", "Old low-relevance memory", 1, 7200, 0.1),
		makeEntry("ambient", "Very recent high-importance fact", 9, 30, 0.0),
		makeEntry("semantic", "Highly relevant stored fact", 5, 3600, 0.9),
	}
	cfg := DefaultCompactionConfig()
	cfg.MaxTokens = 15 // extremely tight
	got := Compact(entries, cfg)

	if got.EntriesRetained < 1 {
		t.Errorf("EntriesRetained = %d; want >= 1", got.EntriesRetained)
	}
	if got.EntriesRetained < 3 && strings.Contains(got.Text, "Old low-relevance") {
		t.Errorf("low-relevance entry survived a tight-budget drop: %q", got.Text)
	}
}

// TestCompressEntry_StripsFormatting mirrors compress_strips_formatting.
// compressEntry must strip markdown bold, collapse multi-line to a
// single line, and drop metadata-looking brackets.
func TestCompressEntry_StripsFormatting(t *testing.T) {
	raw := "**Important**: This is a [episodic_memory | sim=0.85] formatted entry\nWith multiple lines\nAnd verbose content"
	got := compressEntry(raw, 200)
	if strings.Contains(got, "**") {
		t.Errorf("compressed kept markdown bold: %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("compressed kept newlines: %q", got)
	}
	if strings.Contains(got, "sim=0.85") {
		t.Errorf("compressed kept metadata brackets: %q", got)
	}
}

// TestCompressEntry_TruncatesLong mirrors compress_truncates_long_entries.
// Entries beyond maxChars get truncated with an ellipsis; the result
// stays within maxChars + len("...") + small word-boundary slack.
func TestCompressEntry_TruncatesLong(t *testing.T) {
	long := strings.Repeat("a ", 200)
	got := compressEntry(long, 50)
	if len(got) >= 60 {
		t.Errorf("compressed length = %d; want < 60", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("compressed not truncated with ellipsis: %q", got)
	}
}

// TestComputePriority_FavorsRecentRelevant mirrors
// priority_favors_recent_relevant_entries. A recent high-relevance
// entry outranks an old low-relevance one with the same importance.
func TestComputePriority_FavorsRecentRelevant(t *testing.T) {
	recentRelevant := makeEntry("episodic", "test", 5, 60, 0.9)
	oldIrrelevant := makeEntry("episodic", "test", 5, 86400, 0.1)
	if computePriority(recentRelevant) <= computePriority(oldIrrelevant) {
		t.Errorf("recent+relevant (%v) should outrank old+irrelevant (%v)",
			computePriority(recentRelevant), computePriority(oldIrrelevant))
	}
}

// TestTextOverlap_Identical mirrors text_overlap_identical — identical
// strings return ~1.0.
func TestTextOverlap_Identical(t *testing.T) {
	got := textOverlap("the quick brown fox", "the quick brown fox")
	if got < 0.99 {
		t.Errorf("identical overlap = %v; want ~1.0", got)
	}
}

// TestTextOverlap_Different mirrors text_overlap_different — strings
// with no common trigrams return near zero.
func TestTextOverlap_Different(t *testing.T) {
	got := textOverlap("the quick brown fox", "completely different words here")
	if got >= 0.1 {
		t.Errorf("different overlap = %v; want < 0.1", got)
	}
}

// TestCompact_GroupedOutputHasSectionHeaders mirrors
// grouped_output_has_section_headers. Retained entries are grouped under
// their tier header, and the emitted text contains every tier present
// in the input.
func TestCompact_GroupedOutputHasSectionHeaders(t *testing.T) {
	entries := []ContextEntry{
		makeEntry("ambient", "Recent thing happened", 5, 60, 0.0),
		makeEntry("procedural", "How to run a scan", 5, 3600, 0.7),
	}
	got := Compact(entries, DefaultCompactionConfig())
	if !strings.Contains(got.Text, "[Recent Activity]") {
		t.Errorf("missing [Recent Activity] header: %q", got.Text)
	}
	if !strings.Contains(got.Text, "[Skills]") {
		t.Errorf("missing [Skills] header: %q", got.Text)
	}
}

// TestCompactText_PassesThroughUnderBudget verifies that input already
// within the token budget is returned unchanged. This is the common
// fast path when no truncation is needed.
func TestCompactText_PassesThroughUnderBudget(t *testing.T) {
	input := "[Working Memory]\n- short bullet\n"
	got := CompactText(input, 1000)
	if got != input {
		t.Errorf("CompactText mutated under-budget input: got %q, want %q", got, input)
	}
}

// TestCompactText_RespectsBudget verifies that oversized input is
// trimmed; the return value must be strictly shorter and must not
// exceed the budget.
func TestCompactText_RespectsBudget(t *testing.T) {
	lines := []string{"[Relevant Memories]"}
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("- bullet entry number %d with some content to take up space", i))
	}
	input := strings.Join(lines, "\n")
	got := CompactText(input, 40)
	if len(got) >= len(input) {
		t.Errorf("CompactText did not trim oversized input: got len %d, want < %d", len(got), len(input))
	}
}

// TestCompactText_EmptyAndZeroBudget asserts both guard rails: empty
// input and non-positive budgets both produce an empty string.
func TestCompactText_EmptyAndZeroBudget(t *testing.T) {
	if got := CompactText("", 100); got != "" {
		t.Errorf("empty input produced %q; want empty", got)
	}
	if got := CompactText("hello", 0); got != "" {
		t.Errorf("zero budget produced %q; want empty", got)
	}
	if got := CompactText("hello", -5); got != "" {
		t.Errorf("negative budget produced %q; want empty", got)
	}
}

// TestCompactText_KeepsSectionHeaders verifies that section headers are
// emitted verbatim even when the budget forces bullet truncation.
func TestCompactText_KeepsSectionHeaders(t *testing.T) {
	input := "[Working Memory]\n" + strings.Repeat("- a long bullet entry that should be compressed\n", 30)
	got := CompactText(input, 20)
	if !strings.HasPrefix(got, "[Working Memory]") {
		t.Errorf("CompactText dropped section header: %q", got)
	}
}
