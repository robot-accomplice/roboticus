// Package memory — compaction.go ports Rust's
// crates/roboticus-agent/src/compaction.rs.
//
// Compaction is the single authority that takes either
//
//   (a) a set of structured ContextEntry records coming out of retrieval
//       (Compact), or
//   (b) a pre-rendered memory text block that still needs to fit a token
//       budget (CompactText),
//
// and produces a minimal-footprint string ready to enter the token budget
// in the context assembly layer. Before v1.0.6 the live request path
// relied on a naïve `cb.memory[:maxChars]` truncation at the tail, which
// drops whatever happens to sit at the end regardless of priority and
// breaks multi-byte characters mid-rune. SYS-01-003 / SYS-03-001 track
// that gap in the parity-forensics program.
//
// Rust-parity notes:
//   - The ranking formula (0.4*relevance + 0.3*importance + 0.3*recency
//     when relevance>0.1; 0.2*importance + 0.8*recency otherwise) is
//     copied verbatim from `compute_priority` in Rust.
//   - The recency half-life of 1 hour and the ln(2)/3600 decay constant
//     match Rust exactly.
//   - The de-dup Jaccard-over-word-trigram measure and its 0.8 threshold
//     match Rust.
//   - The compress_entry markdown-strip (`**`, `## `, `### `), multi-line
//     collapse to `;`-joined, bullet marker stripping (`- `, `• `), and
//     metadata-bracket removal (`[ ... | ... ]`, `[sim=...]`,
//     `[..._memory]`) match Rust.
//   - Section header names match Rust (`[Working Memory]`,
//     `[Relevant Memories]`, etc.).
//
// Intentional Go deviations (logged as Idiomatic Shift):
//   - Token estimation uses Go's script-aware llm.EstimateTokens instead
//     of Rust's `text.len().div_ceil(4)`. Rust's naive divide overestimates
//     for CJK / emoji and underestimates for code; Go's version samples
//     the string and classifies. This is a Go improvement previously
//     recorded at the tokencount.go author comment and does NOT need to
//     be reverted for parity — the operator-facing contract is "stay
//     within the budget," which Go enforces more accurately.
//   - Word-trigram comparison lowercases via Unicode-aware strings.ToLower
//     rather than Rust's ASCII-only `to_ascii_lowercase`. Same reason.
//
// See docs/parity-forensics/systems/03-memory-retrieval-compaction-and-injection.md
// for the audit record, and docs/parity-forensics/systems/01-... for the
// live-request remediation.

package memory

import (
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"roboticus/internal/llm"
)

// ContextEntry is one piece of retrieved context on its way into the
// compactor. The caller is responsible for populating Importance (0–10),
// AgeSeconds, and Relevance (0–1); Compact will drop lowest-priority
// entries first when the budget is exceeded.
type ContextEntry struct {
	// Source is the memory tier or channel the entry originated from.
	// Known values that get custom section headers:
	//   "working", "ambient", "episodic", "semantic", "procedural",
	//   "relationship", "checkpoint", "hippocampus", "topic_summary".
	// Unknown values get a generic "[<source>]" header.
	Source string

	// Content is the raw text the retriever produced. Compact calls
	// compressEntry on it internally; callers do not need to pre-strip
	// markdown.
	Content string

	// Importance is the retriever's confidence that the entry is
	// load-bearing for the answer. Rust scale is 0–10; values outside
	// that range are clamped to that range for scoring purposes.
	Importance float32

	// AgeSeconds is seconds since creation. Lower = more recent =
	// higher recency score. 0 is treated as "now" (recency = 1.0).
	AgeSeconds uint64

	// Relevance is the cosine similarity to the current query, 0.0–1.0.
	// When Relevance <= 0.1 the ranker switches to the
	// recency/importance weighting (matches Rust).
	Relevance float32
}

// CompactedContext is the final budgeted text plus bookkeeping.
type CompactedContext struct {
	// Text is the compacted block ready to inject into a system
	// message. Never ends with whitespace.
	Text string

	// EntriesRetained is how many input entries survived budget
	// enforcement.
	EntriesRetained int

	// EntriesDropped is how many input entries were compressed,
	// deduplicated, or cut by the budget.
	EntriesDropped int

	// Tokens is the estimated token count of Text, computed with
	// llm.EstimateTokens (the same estimator the context builder
	// uses when budgeting).
	Tokens int
}

// CompactionConfig controls the tradeoff between detail and footprint.
// The zero value is NOT a useful default — use DefaultCompactionConfig.
type CompactionConfig struct {
	// MaxTokens is the hard ceiling for the emitted block. Entries
	// whose cumulative token cost would exceed this are dropped
	// (lowest priority first) before emission. The emitted block may
	// be slightly under this number because per-entry token estimates
	// round; it will never exceed it.
	MaxTokens int

	// MaxEntryChars is the per-entry character cap applied during
	// compressEntry. Entries longer than this are truncated at the
	// nearest word boundary past MaxEntryChars/2.
	MaxEntryChars int

	// DedupThreshold is the Jaccard-trigram similarity at which
	// entries are considered duplicates. 0.8 is the Rust default;
	// values outside [0,1] are clamped.
	DedupThreshold float64
}

// DefaultCompactionConfig returns the Rust-parity defaults (2000 tokens,
// 200 chars/entry, 0.8 dedup). Operators override via the compaction
// config surface wired into the retriever; this helper is for tests and
// ad-hoc tooling.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		MaxTokens:      2000,
		MaxEntryChars:  200,
		DedupThreshold: 0.8,
	}
}

// Compact is the structured entry point. It compresses each entry,
// removes near-duplicates, sorts by priority, and emits a section-
// grouped block within the token budget. Empty input produces an empty
// result (not an error) — callers are expected to pass through entries
// directly without pre-filtering.
func Compact(entries []ContextEntry, config CompactionConfig) CompactedContext {
	if len(entries) == 0 {
		return CompactedContext{}
	}

	threshold := clampThreshold(config.DedupThreshold)

	type row struct {
		priority float64
		text     string
		source   string
	}

	// Phase 1: compress + priority.
	compressed := make([]row, 0, len(entries))
	for _, e := range entries {
		compressed = append(compressed, row{
			priority: computePriority(e),
			text:     compressEntry(e.Content, config.MaxEntryChars),
			source:   e.Source,
		})
	}

	// Phase 2: de-duplicate.
	//
	// Walk in index order and keep an entry only if no already-kept
	// entry overlaps above the threshold. Lower indices take precedence,
	// matching Rust's behavior; typical memory-context sets are small
	// enough that O(n²) is fine.
	deduped := make([]row, 0, len(compressed))
	for _, r := range compressed {
		duplicate := false
		for _, k := range deduped {
			if textOverlap(k.text, r.text) > threshold {
				duplicate = true
				break
			}
		}
		if !duplicate {
			deduped = append(deduped, r)
		}
	}

	// Phase 3: stable sort by priority DESC. SliceStable keeps the
	// original insertion order for equal-priority entries so the
	// emitted block is deterministic for a given input.
	sort.SliceStable(deduped, func(i, j int) bool {
		return deduped[i].priority > deduped[j].priority
	})

	// Phase 4: enforce token budget. We accumulate retained entries in
	// priority order; the first entry that would break the budget ends
	// the pass (matching Rust's greedy cutoff — we do not try to
	// cherry-pick smaller lower-priority entries to fill leftover
	// budget, because that changes which memory tiers survive at all).
	retained := make([]row, 0, len(deduped))
	tokensUsed := 0
	for _, r := range deduped {
		entryTokens := llm.EstimateTokens(r.text)
		if tokensUsed+entryTokens > config.MaxTokens {
			break
		}
		tokensUsed += entryTokens
		retained = append(retained, r)
	}

	// Phase 5: group by source for readable output. BTree-equivalent
	// is a sorted slice of keys; we want stable output for tests and
	// for trace diffs.
	grouped := make(map[string][]string)
	sourceOrder := make([]string, 0, len(retained))
	for _, r := range retained {
		if _, seen := grouped[r.source]; !seen {
			sourceOrder = append(sourceOrder, r.source)
		}
		grouped[r.source] = append(grouped[r.source], r.text)
	}
	sort.Strings(sourceOrder)

	var b strings.Builder
	for _, src := range sourceOrder {
		b.WriteString(sourceHeader(src))
		b.WriteByte('\n')
		for _, t := range grouped[src] {
			b.WriteString("- ")
			b.WriteString(t)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	text := strings.TrimRight(b.String(), "\n ")
	return CompactedContext{
		Text:            text,
		EntriesRetained: len(retained),
		EntriesDropped:  len(entries) - len(retained),
		Tokens:          llm.EstimateTokens(text),
	}
}

// CompactText is the text-level entry point: given a pre-rendered memory
// block (typically the retriever's `[Section]\n- bullet\n- bullet` output),
// return a version that fits within maxTokens.
//
// Behavior:
//   - Empty input or maxTokens <= 0 returns "".
//   - If the input already fits, it is returned unchanged.
//   - Section headers (lines starting with "[" and ending with "]") pass
//     through uncompressed; bullet lines are passed through compressEntry
//     with a 150-char cap before budget accounting.
//   - Lines are emitted in input order; when the next line would exceed
//     the budget, emission stops. This means later sections are dropped
//     before earlier ones — the retriever is expected to put the most
//     important sections first.
//
// This function exists for callers that hold rendered text and cannot
// easily reconstruct the structured ContextEntry list. Prefer Compact
// when structured evidence is available.
func CompactText(text string, maxTokens int) string {
	if text == "" || maxTokens <= 0 {
		return ""
	}

	if llm.EstimateTokens(text) <= maxTokens {
		return text
	}

	var out strings.Builder
	used := 0

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		// Section headers pass through unchanged.
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			headerTokens := llm.EstimateTokens(line)
			if used+headerTokens > maxTokens {
				break
			}
			if out.Len() > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(line)
			out.WriteByte('\n')
			used += headerTokens
			continue
		}

		compressed := compressEntry(line, 150)
		lineTokens := llm.EstimateTokens(compressed)
		if used+lineTokens > maxTokens {
			break
		}
		out.WriteString(compressed)
		out.WriteByte('\n')
		used += lineTokens
	}

	return strings.TrimRight(out.String(), "\n ")
}

// TextOverlapScore is the exported Jaccard-over-word-trigrams similarity.
// Identical strings return 1.0; strings with no common trigrams return 0.0.
// Used by the topic-boundary detector and by external callers that need
// the same similarity measure the compactor uses internally.
func TextOverlapScore(a, b string) float64 {
	return textOverlap(a, b)
}

// ───── internals ────────────────────────────────────────────────────

func clampThreshold(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}

// compressEntry reduces a single entry to its minimal form. Port of
// Rust's compress_entry, preserving the same stripping rules so
// round-tripping memory text across the boundary is stable.
func compressEntry(content string, maxChars int) string {
	text := strings.TrimSpace(content)

	// Strip markdown formatting artifacts.
	text = strings.ReplaceAll(text, "**", "")
	text = strings.ReplaceAll(text, "## ", "")
	text = strings.ReplaceAll(text, "### ", "")

	// Collapse multi-line entries to a single line joined by "; ".
	if strings.Contains(text, "\n") {
		lines := strings.Split(text, "\n")
		cleaned := lines[:0]
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l != "" {
				cleaned = append(cleaned, l)
			}
		}
		text = strings.Join(cleaned, "; ")
	}

	// Strip leading bullet/list markers.
	text = strings.TrimPrefix(text, "- ")
	text = strings.TrimPrefix(text, "• ")

	// Strip metadata-bracket runs like "[episodic_memory | sim=0.85]"
	// anywhere in the string. We only strip bracket runs that look
	// like metadata — contain '|', "sim=", or "_memory" — so natural
	// square-bracket content ("[example]") survives.
	for {
		start := strings.Index(text, "[")
		if start < 0 {
			break
		}
		end := strings.Index(text[start:], "]")
		if end < 0 {
			break
		}
		inner := text[start+1 : start+end]
		if strings.Contains(inner, "|") ||
			strings.Contains(inner, "sim=") ||
			strings.Contains(inner, "_memory") {
			left := strings.TrimRight(text[:start], " ")
			right := strings.TrimLeft(text[start+end+1:], " ")
			text = left + right
			continue
		}
		break
	}

	// Truncate to max chars — rune-safe, not byte-safe, so multi-byte
	// characters are never split. Rust's byte-based truncation is
	// still correct because Rust strings are UTF-8 and `chars().take()`
	// does the right thing; Go's explicit rune counting is equivalent.
	if utf8.RuneCountInString(text) > maxChars {
		runes := []rune(text)
		truncated := runes[:maxChars]
		text = string(truncated)

		// Clean truncation — don't cut mid-word if there's a later
		// space past half the cap.
		if lastSpace := strings.LastIndex(text, " "); lastSpace > maxChars/2 {
			text = text[:lastSpace]
		}
		text += "..."
	}

	return text
}

// computePriority is the Rust-verbatim ranking formula.
//
// When relevance > 0.1, the formula weights relevance highest:
//
//	0.4*relevance + 0.3*importance_norm + 0.3*recency
//
// When relevance is at or below 0.1 (typical for ambient entries that
// were never ranked against a query), the formula falls back to a
// recency-dominant weighting:
//
//	0.2*importance_norm + 0.8*recency
//
// importance_norm is importance/10.0 clamped to [0,1]; recency is
// exp(-ln(2) * age_seconds / 3600), giving a 1-hour half-life.
func computePriority(e ContextEntry) float64 {
	importance := float64(e.Importance) / 10.0
	if importance < 0 {
		importance = 0
	}
	if importance > 1 {
		importance = 1
	}

	relevance := float64(e.Relevance)
	if relevance < 0 {
		relevance = 0
	}
	if relevance > 1 {
		relevance = 1
	}

	var recency float64
	if e.AgeSeconds == 0 {
		recency = 1.0
	} else {
		recency = math.Exp(-math.Ln2 * float64(e.AgeSeconds) / 3600.0)
	}

	if relevance > 0.1 {
		return 0.4*relevance + 0.3*importance + 0.3*recency
	}
	return 0.2*importance + 0.8*recency
}

// textOverlap computes Jaccard similarity on word trigrams. The exact
// measure Rust uses; identical algorithm, just expressed with maps
// instead of HashSets.
func textOverlap(a, b string) float64 {
	ta := wordTrigrams(a)
	tb := wordTrigrams(b)
	if len(ta) == 0 && len(tb) == 0 {
		return 1.0
	}

	intersect := 0
	for k := range ta {
		if _, ok := tb[k]; ok {
			intersect++
		}
	}
	union := len(ta) + len(tb) - intersect
	if union == 0 {
		return 0
	}
	return float64(intersect) / float64(union)
}

// wordTrigrams emits the lowercase three-word windows of text. Strings
// with fewer than three words emit their individual words as-is (Rust
// does the same) so single-word entries can still be compared against
// each other.
func wordTrigrams(text string) map[string]struct{} {
	out := make(map[string]struct{})
	words := strings.Fields(text)
	if len(words) < 3 {
		for _, w := range words {
			out[strings.ToLower(w)] = struct{}{}
		}
		return out
	}
	for i := 0; i+3 <= len(words); i++ {
		out[strings.ToLower(words[i]+" "+words[i+1]+" "+words[i+2])] = struct{}{}
	}
	return out
}

// sourceHeader maps a memory tier name to the section header that goes
// above the bulleted entries from that tier. The mapping matches Rust
// so the emitted text is stable across runtimes and the
// retriever/verifier can continue to recognize section headers without
// changes.
func sourceHeader(source string) string {
	switch source {
	case "working":
		return "[Working Memory]"
	case "ambient":
		return "[Recent Activity]"
	case "episodic", "semantic":
		return "[Relevant Memories]"
	case "procedural":
		return "[Skills]"
	case "relationship":
		return "[Relationships]"
	case "checkpoint":
		return "[Session Context]"
	case "hippocampus":
		return "[Storage]"
	case "topic_summary":
		return "[Earlier Topics]"
	default:
		return "[" + source + "]"
	}
}
