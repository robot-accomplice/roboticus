// tool_facts.go implements the Milestone 7 follow-on: harvest
// assumption-like facts from tool-result messages and record them as
// `assumption` executive entries, but only when the agent's final response
// actually references them in reasoning.
//
// Harvesting policy (per the roadmap design discussion):
//   - Narrow allowlist of tools whose output is structured enough to yield
//     a reusable premise. Generic shell / procedural tools are excluded.
//   - Per-source confidence, not a flat value:
//       * recall_memory  — inherit stored confidence (capped at 0.9).
//       * search_memories — 0.65 (inventory/discovery).
//       * read_file      — 0.75 (authoritative structured read).
//       * query_knowledge_graph — 0.75 (authoritative graph fact).
//       * find_workflow  — 0.65 (inventory) or inherit workflow confidence
//         (when the tool returned a single named workflow).
//   - Reference gate: a harvested fact is only persisted when its keywords
//     appear in the final assistant response. Observation alone does not
//     count — the fact must have been used in reasoning, planning, or
//     verification. This keeps working memory from flooding with ambient
//     facts the agent saw but never relied on.

package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolFactSource categorises a harvested tool fact for per-source scoring
// and downstream auditability.
type ToolFactSource string

const (
	FactSourceMemoryRecall   ToolFactSource = "memory_recall"
	FactSourceMemorySearch   ToolFactSource = "memory_search"
	FactSourceFileRead       ToolFactSource = "file_read"
	FactSourceGraphLookup    ToolFactSource = "graph_lookup"
	FactSourceWorkflowLookup ToolFactSource = "workflow_lookup"
)

// ToolFact is a candidate fact harvested from a tool-result message. It is
// not persisted until the reference-gate check passes.
type ToolFact struct {
	Subject    string
	Value      string
	ToolName   string
	Source     ToolFactSource
	Confidence float64
	Keywords   []string
}

// toolFactContentLimit caps tool-result bodies that will be scanned for
// facts. Above this, the output is almost always a large blob (file dump,
// multi-record search result truncation) with no reusable premise, so the
// harvester skips it entirely.
const toolFactContentLimit = 8000

// ExtractToolFacts scans the session's tool-result messages via the
// allowlist harvesters. The caller is expected to pass the result through
// FilterFactsReferencedByResponse before persisting any of them.
func ExtractToolFacts(session *Session) []ToolFact {
	if session == nil {
		return nil
	}
	msgs := session.Messages()
	var out []ToolFact
	for _, msg := range msgs {
		if msg.Role != "tool" || msg.Name == "" || msg.Content == "" {
			continue
		}
		body := strings.TrimSpace(msg.Content)
		if body == "" {
			continue
		}
		if isToolFactFailureBody(body) {
			continue
		}
		if len(body) > toolFactContentLimit {
			continue
		}
		out = append(out, harvestToolResult(msg.Name, body)...)
	}
	return out
}

// FilterFactsReferencedByResponse implements the "only persist when
// referenced" rule. A fact is kept only when enough of its keywords appear
// in the final assistant response to be considered used in reasoning.
func FilterFactsReferencedByResponse(facts []ToolFact, response string) []ToolFact {
	trimmed := strings.TrimSpace(response)
	if len(facts) == 0 || trimmed == "" {
		return nil
	}
	lowerResp := strings.ToLower(trimmed)
	var kept []ToolFact
	for _, f := range facts {
		if len(f.Keywords) == 0 {
			continue
		}
		matched := 0
		for _, kw := range f.Keywords {
			if strings.Contains(lowerResp, strings.ToLower(kw)) {
				matched++
			}
		}
		threshold := 1
		if len(f.Keywords) >= 3 {
			threshold = 2
		}
		if matched >= threshold {
			kept = append(kept, f)
		}
	}
	return kept
}

// harvestToolResult dispatches to the per-tool harvester. Tools not in the
// allowlist return no facts — the harvester is intentionally conservative.
func harvestToolResult(toolName, body string) []ToolFact {
	switch toolName {
	case "recall_memory":
		return harvestRecallMemory(body)
	case "search_memories":
		return harvestSearchMemories(body)
	case "read_file":
		return harvestReadFile(body)
	case "query_knowledge_graph":
		return harvestGraphLookup(body)
	case "find_workflow":
		return harvestWorkflowLookup(body)
	default:
		return nil
	}
}

// harvestRecallMemory extracts one fact from a recall_memory JSON payload,
// inheriting the stored confidence of the underlying row (capped at 0.9 to
// acknowledge the transformation from row → assumption).
func harvestRecallMemory(body string) []ToolFact {
	var data map[string]any
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return nil
	}
	sourceTable, _ := data["source_table"].(string)
	inheritConfidence := func() float64 {
		if c, ok := data["confidence"].(float64); ok && c > 0 {
			if c > 0.9 {
				return 0.9
			}
			return c
		}
		return 0.75
	}
	switch sourceTable {
	case "semantic_memory":
		key, _ := data["key"].(string)
		value, _ := data["value"].(string)
		if key == "" || value == "" {
			return nil
		}
		return []ToolFact{{
			Subject:    key,
			Value:      value,
			ToolName:   "recall_memory",
			Source:     FactSourceMemoryRecall,
			Confidence: inheritConfidence(),
			Keywords:   extractToolFactKeywords(key, value),
		}}
	case "knowledge_facts":
		subj, _ := data["subject"].(string)
		rel, _ := data["relation"].(string)
		obj, _ := data["object"].(string)
		if subj == "" || rel == "" || obj == "" {
			return nil
		}
		return []ToolFact{{
			Subject:    subj,
			Value:      fmt.Sprintf("%s %s %s", subj, rel, obj),
			ToolName:   "recall_memory",
			Source:     FactSourceMemoryRecall,
			Confidence: inheritConfidence(),
			Keywords:   extractToolFactKeywords(subj, rel, obj),
		}}
	}
	return nil
}

// harvestSearchMemories extracts the subjects surfaced by a search as an
// inventory of candidate premises. The output format is:
//
//	Found N memories matching 'QUERY':
//	[{"source_table":..., "source_id":..., "content":...}, ...]
//
// We parse the JSON tail and treat each record as a 0.65-confidence fact.
func harvestSearchMemories(body string) []ToolFact {
	lbracket := strings.Index(body, "[")
	if lbracket < 0 {
		return nil
	}
	jsonPart := body[lbracket:]
	var results []struct {
		SourceTable string `json:"source_table"`
		SourceID    string `json:"source_id"`
		Content     string `json:"content"`
		Category    string `json:"category,omitempty"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &results); err != nil {
		return nil
	}
	var out []ToolFact
	for _, r := range results {
		if strings.TrimSpace(r.Content) == "" {
			continue
		}
		subj := r.Category
		if subj == "" {
			subj = r.SourceTable
		}
		value := r.Content
		if len(value) > 240 {
			value = value[:240]
		}
		out = append(out, ToolFact{
			Subject:    subj,
			Value:      value,
			ToolName:   "search_memories",
			Source:     FactSourceMemorySearch,
			Confidence: 0.65,
			Keywords:   extractToolFactKeywords(subj, value),
		})
		if len(out) >= 5 {
			break
		}
	}
	return out
}

// harvestReadFile extracts narrow `key: value` pairs from the top of a
// file-read output. Giant blobs and lines that look like code / shell are
// skipped — a reusable premise is a single discrete fact, not a document
// fragment.
func harvestReadFile(body string) []ToolFact {
	if len(body) > 2000 {
		return nil
	}
	var out []ToolFact
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		colon := strings.Index(line, ": ")
		if colon <= 0 || colon >= len(line)-2 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		val := strings.TrimSpace(line[colon+2:])
		if key == "" || val == "" {
			continue
		}
		if len(key) > 60 || len(val) > 200 {
			continue
		}
		// Skip noisy / structural lines — keys with spaces, quotes, parens
		// are almost always log fragments, not YAML-style facts.
		if strings.ContainsAny(key, " \t\"'()[]{}=;") {
			continue
		}
		out = append(out, ToolFact{
			Subject:    key,
			Value:      val,
			ToolName:   "read_file",
			Source:     FactSourceFileRead,
			Confidence: 0.75,
			Keywords:   extractToolFactKeywords(key, val),
		})
		if len(out) >= 6 {
			break
		}
	}
	return out
}

// harvestGraphLookup parses a query_knowledge_graph JSON result and emits
// one ToolFact per discovered edge / path hop. Subject is the `from` node,
// value is "from --relation--> to".
func harvestGraphLookup(body string) []ToolFact {
	// Try path result first (has `hops` field).
	var pathResult struct {
		Summary string `json:"summary"`
		Found   bool   `json:"found"`
		Hops    []struct {
			From     string  `json:"from"`
			To       string  `json:"to"`
			Relation string  `json:"relation"`
			Score    float64 `json:"confidence"`
		} `json:"hops"`
	}
	if err := json.Unmarshal([]byte(body), &pathResult); err == nil && pathResult.Found && len(pathResult.Hops) > 0 {
		var out []ToolFact
		for _, hop := range pathResult.Hops {
			if hop.From == "" || hop.To == "" {
				continue
			}
			out = append(out, ToolFact{
				Subject:    hop.From,
				Value:      fmt.Sprintf("%s --%s--> %s", hop.From, hop.Relation, hop.To),
				ToolName:   "query_knowledge_graph",
				Source:     FactSourceGraphLookup,
				Confidence: 0.75,
				Keywords:   extractToolFactKeywords(hop.From, hop.Relation, hop.To),
			})
		}
		return out
	}

	// Fall back to traversal result (has `paths` + `nodes`).
	var travResult struct {
		Summary string `json:"summary"`
		Paths   []struct {
			End   string `json:"end"`
			Edges []struct {
				From     string `json:"from"`
				To       string `json:"to"`
				Relation string `json:"relation"`
			} `json:"edges"`
		} `json:"paths"`
	}
	if err := json.Unmarshal([]byte(body), &travResult); err != nil {
		return nil
	}
	var out []ToolFact
	for _, p := range travResult.Paths {
		if len(p.Edges) == 0 {
			continue
		}
		// Emit the terminal edge only — the nearest structural fact.
		last := p.Edges[len(p.Edges)-1]
		if last.From == "" || last.To == "" {
			continue
		}
		out = append(out, ToolFact{
			Subject:    last.From,
			Value:      fmt.Sprintf("%s --%s--> %s", last.From, last.Relation, last.To),
			ToolName:   "query_knowledge_graph",
			Source:     FactSourceGraphLookup,
			Confidence: 0.75,
			Keywords:   extractToolFactKeywords(last.From, last.Relation, last.To),
		})
		if len(out) >= 4 {
			break
		}
	}
	return out
}

// harvestWorkflowLookup parses a find_workflow result and emits one
// ToolFact per returned workflow, with confidence either inherited
// (get-by-name, operation returned one workflow) or 0.65 (inventory).
func harvestWorkflowLookup(body string) []ToolFact {
	// get result: {workflow: {...}, found: true}
	var getResult struct {
		Found    bool `json:"found"`
		Workflow *struct {
			Name        string   `json:"name"`
			Version     int      `json:"version"`
			Confidence  float64  `json:"confidence"`
			Steps       []string `json:"steps"`
			ContextTags []string `json:"context_tags"`
		} `json:"workflow"`
	}
	if err := json.Unmarshal([]byte(body), &getResult); err == nil && getResult.Found && getResult.Workflow != nil {
		wf := getResult.Workflow
		conf := wf.Confidence
		if conf <= 0 {
			conf = 0.65
		}
		if conf > 0.9 {
			conf = 0.9
		}
		value := fmt.Sprintf("workflow %s v%d", wf.Name, wf.Version)
		if len(wf.Steps) > 0 {
			value += ": " + strings.Join(wf.Steps, " → ")
		}
		return []ToolFact{{
			Subject:    wf.Name,
			Value:      value,
			ToolName:   "find_workflow",
			Source:     FactSourceWorkflowLookup,
			Confidence: conf,
			Keywords:   extractToolFactKeywords(append([]string{wf.Name}, wf.ContextTags...)...),
		}}
	}

	// find result: {matches: [{...}, ...]}
	var findResult struct {
		Matches []struct {
			Name        string   `json:"name"`
			Version     int      `json:"version"`
			Confidence  float64  `json:"confidence"`
			Steps       []string `json:"steps"`
			ContextTags []string `json:"context_tags"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(body), &findResult); err != nil {
		return nil
	}
	var out []ToolFact
	for _, m := range findResult.Matches {
		if m.Name == "" {
			continue
		}
		value := fmt.Sprintf("workflow %s v%d", m.Name, m.Version)
		out = append(out, ToolFact{
			Subject:    m.Name,
			Value:      value,
			ToolName:   "find_workflow",
			Source:     FactSourceWorkflowLookup,
			Confidence: 0.65,
			Keywords:   extractToolFactKeywords(append([]string{m.Name}, m.ContextTags...)...),
		})
		if len(out) >= 4 {
			break
		}
	}
	return out
}

// extractToolFactKeywords returns a deduplicated lowercase token list from
// the input strings, suitable for the reference-gate match.
func extractToolFactKeywords(inputs ...string) []string {
	seen := make(map[string]struct{})
	var out []string
	stop := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "with": {}, "that": {}, "this": {},
		"from": {}, "into": {}, "what": {}, "when": {}, "where": {},
	}
	for _, s := range inputs {
		tokens := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
		})
		for _, tok := range tokens {
			if len(tok) < 4 {
				continue
			}
			if _, skip := stop[tok]; skip {
				continue
			}
			if _, dup := seen[tok]; dup {
				continue
			}
			seen[tok] = struct{}{}
			out = append(out, tok)
		}
	}
	return out
}

// isToolFactFailureBody returns true when a tool-result body reads as an
// error rather than a successful payload. Failure output already lands in
// ErrorsSeen on the episode summary; we should not double-record it as a
// fact.
func isToolFactFailureBody(body string) bool {
	lower := strings.ToLower(body)
	return strings.HasPrefix(lower, "error") ||
		strings.HasPrefix(lower, "failed") ||
		strings.HasPrefix(lower, `{"error`)
}
