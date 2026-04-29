package session

import (
	"fmt"
	"strings"
)

// PendingActionArtifact is the typed continuity state for assistant replies
// that explicitly ask the operator whether to proceed with a concrete next
// action. Short confirmations bind to this before generic chat shortcuts.
type PendingActionArtifact struct {
	SourceAssistantExcerpt string
	ProposedAction         string
	ConfirmationPrompt     string
	Evidence               string
}

// DetectPendingActionArtifact derives typed pending-action state from an
// assistant reply. The detector is intentionally state-first: it looks for a
// concrete assistant-owned action candidate such as a structured "next steps"
// block or a direct action commitment. Confirmation wording can strengthen the
// artifact but is not the control path.
func DetectPendingActionArtifact(reply string) *PendingActionArtifact {
	compact := compactWhitespace(reply)
	if compact == "" {
		return nil
	}
	action, evidence := extractPendingActionCandidate(reply)
	if action == "" {
		return nil
	}
	return &PendingActionArtifact{
		SourceAssistantExcerpt: truncateRunes(compact, 600),
		ProposedAction:         truncateRunes(action, 360),
		ConfirmationPrompt:     truncateRunes(extractConfirmationSentence(compact), 240),
		Evidence:               evidence,
	}
}

func (p PendingActionArtifact) Render() string {
	var sections []string
	if action := strings.TrimSpace(p.ProposedAction); action != "" {
		sections = append(sections, "PROPOSED ACTION\n"+action)
	}
	if prompt := strings.TrimSpace(p.ConfirmationPrompt); prompt != "" {
		sections = append(sections, "CONFIRMATION PROMPT\n"+prompt)
	}
	if excerpt := strings.TrimSpace(p.SourceAssistantExcerpt); excerpt != "" {
		sections = append(sections, "SOURCE ASSISTANT EXCERPT\n"+excerpt)
	}
	if evidence := strings.TrimSpace(p.Evidence); evidence != "" {
		sections = append(sections, "EVIDENCE\n"+evidence)
	}
	if len(sections) == 0 {
		return ""
	}
	return "PENDING ACTION\n" + strings.Join(sections, "\n\n")
}

func extractPendingActionCandidate(s string) (string, string) {
	if action := extractStructuredNextSteps(s); action != "" {
		return action, "structured_next_steps"
	}
	for _, sentence := range splitSentences(s) {
		if isAssistantActionCommitment(sentence) {
			return sentence, "assistant_action_commitment"
		}
	}
	return "", ""
}

func extractStructuredNextSteps(s string) string {
	lines := strings.Split(s, "\n")
	inNextSteps := false
	var actions []string
	for _, line := range lines {
		cleaned := strings.TrimSpace(line)
		if cleaned == "" {
			if inNextSteps && len(actions) > 0 {
				break
			}
			continue
		}
		if isNextStepsHeading(cleaned) {
			inNextSteps = true
			continue
		}
		if !inNextSteps {
			continue
		}
		if looksLikeHeading(cleaned) && len(actions) > 0 {
			break
		}
		if action := normalizeListAction(cleaned); action != "" {
			actions = append(actions, action)
		}
	}
	if len(actions) == 0 {
		return ""
	}
	return strings.Join(actions, " ")
}

func isNextStepsHeading(line string) bool {
	lower := strings.Trim(strings.ToLower(line), " :#*")
	return lower == "next steps" || lower == "next step" || lower == "next"
}

func looksLikeHeading(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		return true
	}
	if strings.HasSuffix(trimmed, ":") && len(strings.Fields(trimmed)) <= 6 {
		return true
	}
	return false
}

func normalizeListAction(line string) string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimLeft(trimmed, "-*• \t")
	trimmed = strings.TrimSpace(trimNumericPrefix(trimmed))
	if trimmed == "" {
		return ""
	}
	if !isAssistantActionCommitment(trimmed) && !startsLikeAction(trimmed) {
		return ""
	}
	return trimmed
}

func trimNumericPrefix(s string) string {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(s) {
		return s
	}
	if s[i] == '.' || s[i] == ')' {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

func isAssistantActionCommitment(sentence string) bool {
	lower := strings.ToLower(strings.TrimSpace(sentence))
	if lower == "" {
		return false
	}
	commitmentStarts := []string{
		"i will ",
		"i'll ",
		"i need to ",
		"i should ",
		"i can ",
		"next step is ",
		"next action is ",
		"the next step is ",
		"the next action is ",
	}
	for _, prefix := range commitmentStarts {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func startsLikeAction(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "" {
		return false
	}
	for _, verb := range []string{
		"review", "analyze", "compare", "inspect", "examine", "read",
		"parse", "extract", "retrieve", "query", "run", "test", "audit",
		"verify", "summarize", "locate", "open", "check",
	} {
		if lower == verb || strings.HasPrefix(lower, verb+" ") {
			return true
		}
	}
	return false
}

func extractConfirmationSentence(s string) string {
	for _, sentence := range splitSentences(s) {
		if strings.Contains(sentence, "?") || isExplicitConfirmationSentence(sentence) {
			return sentence
		}
	}
	return ""
}

func isExplicitConfirmationSentence(sentence string) bool {
	lower := strings.ToLower(strings.TrimSpace(sentence))
	if lower == "" {
		return false
	}
	confirmationStarts := []string{
		"please confirm",
		"confirm if",
		"let me know if",
	}
	for _, prefix := range confirmationStarts {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func splitSentences(s string) []string {
	var sentences []string
	var b strings.Builder
	for _, r := range s {
		b.WriteRune(r)
		if r == '.' || r == '?' || r == '!' || r == '\n' {
			if sentence := strings.TrimSpace(b.String()); sentence != "" {
				sentences = append(sentences, sentence)
			}
			b.Reset()
		}
	}
	if sentence := strings.TrimSpace(b.String()); sentence != "" {
		sentences = append(sentences, sentence)
	}
	return sentences
}

func compactWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateRunes(s string, max int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= max {
		return string(runes)
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return fmt.Sprintf("%s...", string(runes[:max-3]))
}
