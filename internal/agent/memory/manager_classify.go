// manager_classify.go — turn classification and tool output summarization.
// Extracted from manager.go to keep file under 500-line cap.

package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"roboticus/internal/llm"
)

// classifyTurnPrototypes are prototype texts for each turn type.
var classifyTurnPrototypes = map[TurnType]string{
	TurnFinancial: "financial transaction payment transfer balance wallet money send receive funds",
	TurnSocial:    "greeting hello thanks social conversation how are you good morning",
	TurnCreative:  "create write design compose generate build make produce draft",
	TurnReasoning: "analyze explain reason think evaluate compare understand research",
}

// classifyTurnWithEmbeddings uses cosine similarity against prototype embeddings.
func classifyTurnWithEmbeddings(ctx context.Context, ec *llm.EmbeddingClient, text string) (TurnType, bool) {
	queryVec, err := ec.EmbedSingle(ctx, text)
	if err != nil || len(queryVec) == 0 {
		return TurnReasoning, false
	}

	bestType := TurnReasoning
	bestSim := float64(0)
	const threshold = 0.3

	for turnType, proto := range classifyTurnPrototypes {
		protoVec, err := ec.EmbedSingle(ctx, proto)
		if err != nil {
			continue
		}
		sim := llm.CosineSimilarity(queryVec, protoVec)
		if sim > bestSim {
			bestSim = sim
			bestType = turnType
		}
	}

	if bestSim >= threshold {
		return bestType, true
	}
	return TurnReasoning, false
}

// classifyTurn determines the type of the most recent exchange.
func classifyTurn(messages []llm.Message) TurnType {
	for _, m := range messages {
		if m.Role == "tool" {
			return TurnToolUse
		}
	}

	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lower := strings.ToLower(messages[i].Content)

			financialWords := []string{"transfer", "balance", "wallet", "payment", "usdc", "send funds"}
			count := 0
			for _, word := range financialWords {
				if strings.Contains(lower, word) {
					count++
				}
			}
			if count >= 2 {
				return TurnFinancial
			}

			socialWords := []string{"hello", "thanks", "thank you", "please", "how are you", "hi ", "hey ", "good morning", "good evening"}
			for _, word := range socialWords {
				if strings.Contains(lower, word) {
					return TurnSocial
				}
			}

			creativeWords := []string{"create", "write", "design", "compose", "generate"}
			for _, word := range creativeWords {
				if strings.Contains(lower, word) {
					return TurnCreative
				}
			}
			break
		}
	}

	return TurnReasoning
}

// isToolFailure checks if tool output indicates an error.
func isToolFailure(output string) bool {
	lower := strings.ToLower(output)
	prefixes := []string{"error:", "failed:", "failure:", "fatal:", "panic:"}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	if strings.HasPrefix(lower, `{"error`) || strings.HasPrefix(lower, `{"err`) {
		return true
	}
	return false
}

// summarizeToolOutput produces a human-readable summary of a tool's output.
func summarizeToolOutput(toolName, content string) string {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		if s, ok := summarizeJSON(toolName, trimmed); ok {
			return s
		}
	}
	return toolName + ": " + safeUTF8Truncate(content, 150)
}

// summarizeJSON attempts to parse content as JSON and returns a concise summary.
func summarizeJSON(toolName, content string) (string, bool) {
	var arr []json.RawMessage
	if json.Unmarshal([]byte(content), &arr) == nil {
		return safeUTF8Truncate(fmt.Sprintf("%s: %d items returned", toolName, len(arr)), 150), true
	}

	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(content), &obj) != nil {
		return "", false
	}

	if raw, ok := obj["error"]; ok {
		var errMsg string
		if json.Unmarshal(raw, &errMsg) != nil {
			errMsg = strings.Trim(string(raw), `"`)
		}
		return safeUTF8Truncate(fmt.Sprintf("%s: error — %s", toolName, errMsg), 150), true
	}

	if raw, ok := obj["status"]; ok {
		var status string
		if json.Unmarshal(raw, &status) != nil {
			status = strings.Trim(string(raw), `"`)
		}
		return safeUTF8Truncate(fmt.Sprintf("%s: status=%s", toolName, status), 150), true
	}

	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	keyList := strings.Join(keys, ", ")
	return safeUTF8Truncate(fmt.Sprintf("%s: {%s}", toolName, keyList), 150), true
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractFirstSentence returns the first sentence.
func extractFirstSentence(s string) string {
	for i, r := range s {
		if r == '.' || r == '?' || r == '!' || r == '\n' {
			if i > 0 {
				return s[:i]
			}
		}
		if i > 100 {
			return s[:100]
		}
	}
	if len(s) > 100 {
		return s[:100]
	}
	return s
}
