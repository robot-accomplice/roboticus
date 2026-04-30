package llm

import "strings"

const (
	ContextKindSystem           = "system"
	ContextKindTools            = "tools"
	ContextKindMemory           = "memory"
	ContextKindMemoryIndex      = "memory_index"
	ContextKindAmbient          = "ambient"
	ContextKindExecutionOverlay = "execution_overlay"
	ContextKindReflection       = "reflection"
	ContextKindTopicSummary     = "topic_summary"
	ContextKindHistory          = "history"
	ContextKindCurrentUser      = "current_user"
	ContextKindUnused           = "unused"
)

// ContextFootprint is the request-shaped token attribution artifact consumed by
// RCA and dashboard context-pressure views. Counts are approximate but owned by
// the final llm.Request, not reconstructed from incomplete UI/session state.
type ContextFootprint struct {
	TokenBudget     int                                 `json:"token_budget"`
	UsedTokens      int                                 `json:"used_tokens"`
	UnusedTokens    int                                 `json:"unused_tokens"`
	OverheadTokens  int                                 `json:"overhead_tokens"`
	OverheadPercent float64                             `json:"overhead_pct"`
	Categories      map[string]int                      `json:"categories"`
	Details         map[string][]ContextFootprintDetail `json:"details,omitempty"`
}

type ContextFootprintDetail struct {
	Kind    string `json:"kind"`
	Role    string `json:"role,omitempty"`
	Name    string `json:"name,omitempty"`
	Tokens  int    `json:"tokens"`
	Preview string `json:"preview,omitempty"`
}

// RequestContextFootprint attributes request tokens by explicit ContextKind.
// Unknown conversational messages fall back to history, and unknown system
// messages fall back to system so older callers remain diagnostically useful.
func RequestContextFootprint(req *Request) ContextFootprint {
	fp := ContextFootprint{
		TokenBudget: req.ContextBudget,
		Categories:  make(map[string]int),
		Details:     make(map[string][]ContextFootprintDetail),
	}
	for _, msg := range req.Messages {
		text := messageTextForFootprint(msg)
		tokens := EstimateTokens(text)
		if tokens == 0 && (len(msg.ToolCalls) > 0 || msg.ToolCallID != "") {
			tokens = 1
		}
		kind := normalizeContextKind(msg.ContextKind, msg.Role)
		fp.Categories[kind] += tokens
		fp.Details[kind] = append(fp.Details[kind], ContextFootprintDetail{
			Kind:    "message",
			Role:    msg.Role,
			Name:    msg.Name,
			Tokens:  tokens,
			Preview: boundedPreview(text, 240),
		})
		fp.UsedTokens += tokens
	}
	toolTokens := 0
	for _, detail := range toolDefinitionDetails(req.Tools) {
		toolTokens += detail.Tokens
		fp.Details[ContextKindTools] = append(fp.Details[ContextKindTools], detail)
	}
	fp.Categories[ContextKindTools] += toolTokens
	fp.UsedTokens += toolTokens
	if fp.TokenBudget <= 0 {
		fp.TokenBudget = fp.UsedTokens
	}
	fp.UnusedTokens = fp.TokenBudget - fp.UsedTokens
	if fp.UnusedTokens < 0 {
		fp.UnusedTokens = 0
	}
	fp.Categories[ContextKindUnused] = fp.UnusedTokens
	if fp.UnusedTokens > 0 {
		fp.Details[ContextKindUnused] = []ContextFootprintDetail{{
			Kind:    "unused_budget",
			Tokens:  fp.UnusedTokens,
			Preview: "Available context budget not consumed by this request.",
		}}
	}
	currentUser := fp.Categories[ContextKindCurrentUser]
	fp.OverheadTokens = fp.UsedTokens - currentUser
	if fp.OverheadTokens < 0 {
		fp.OverheadTokens = 0
	}
	if fp.TokenBudget > 0 {
		fp.OverheadPercent = float64(fp.OverheadTokens) / float64(fp.TokenBudget)
	}
	return fp
}

func messageTextForFootprint(msg Message) string {
	if len(msg.ContentParts) == 0 {
		return msg.Content
	}
	var b strings.Builder
	for _, part := range msg.ContentParts {
		if strings.TrimSpace(part.Text) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(part.Text)
	}
	if b.Len() == 0 {
		return msg.Content
	}
	return b.String()
}

func normalizeContextKind(kind, role string) string {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case ContextKindSystem, ContextKindTools, ContextKindMemory, ContextKindMemoryIndex,
		ContextKindAmbient, ContextKindExecutionOverlay, ContextKindReflection,
		ContextKindTopicSummary, ContextKindHistory, ContextKindCurrentUser:
		return strings.TrimSpace(strings.ToLower(kind))
	}
	if strings.EqualFold(role, "system") {
		return ContextKindSystem
	}
	return ContextKindHistory
}

func toolDefinitionDetails(tools []ToolDef) []ContextFootprintDetail {
	details := make([]ContextFootprintDetail, 0, len(tools))
	for _, td := range tools {
		text := td.Function.Name + "\n" + td.Function.Description + "\n" + string(td.Function.Parameters)
		details = append(details, ContextFootprintDetail{
			Kind:    "tool",
			Name:    td.Function.Name,
			Tokens:  EstimateTokens(text),
			Preview: boundedPreview(td.Function.Description, 240),
		})
	}
	return details
}

func boundedPreview(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}
