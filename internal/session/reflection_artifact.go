package session

import (
	"fmt"
	"strings"

	"roboticus/internal/llm"
)

// TOTOF is the canonical reflection artifact:
// task, authoritative observed results, tool outcomes, open issues,
// and bounded finalization instruction.
//
// The artifact is provider-agnostic. Individual providers/models may need
// different renderings, but they should all consume the same canonical state.
type TOTOF struct {
	UserTask                    string
	AuthoritativeObservedResult []string
	ToolOutcomes                []TOTOFToolOutcome
	OpenIssues                  []string
	FinalizationInstruction     string
}

// ContinuationArtifact is the canonical continuation payload used when the
// reflective R explicitly determines that more execution is required.
// It preserves the same authoritative observation state as TOTOF while adding
// one bounded remaining-work summary and a continuation instruction.
type ContinuationArtifact struct {
	Task                      string
	AuthoritativeObservations []string
	ToolOutcomes              []TOTOFToolOutcome
	OpenIssues                []string
	RemainingWork             string
	ContinuationInstruction   string
}

type TOTOFToolOutcome struct {
	ToolName string
	Status   string
	Outcome  string
}

// BuildTOTOF derives the canonical reflection artifact from current session
// state without replaying raw assistant tool-call transcript.
func (s *Session) BuildTOTOF(finalizationInstruction string) TOTOF {
	observed, outcomes := currentObservationWindow(s.messages)
	openIssues := currentOpenIssues(s)
	return TOTOF{
		UserTask:                    lastUserTask(s.messages),
		AuthoritativeObservedResult: observed,
		ToolOutcomes:                outcomes,
		OpenIssues:                  openIssues,
		FinalizationInstruction:     strings.TrimSpace(finalizationInstruction),
	}
}

// BuildContinuationArtifact derives a provider-agnostic continuation payload
// from the latest canonical reflection state plus the explicit remaining-work
// reason supplied by the reflect phase.
func (s *Session) BuildContinuationArtifact(remainingWork, continuationInstruction string) ContinuationArtifact {
	totof := s.BuildTOTOF("")
	return ContinuationArtifact{
		Task:                      totof.UserTask,
		AuthoritativeObservations: append([]string(nil), totof.AuthoritativeObservedResult...),
		ToolOutcomes:              append([]TOTOFToolOutcome(nil), totof.ToolOutcomes...),
		OpenIssues:                append([]string(nil), totof.OpenIssues...),
		RemainingWork:             strings.TrimSpace(remainingWork),
		ContinuationInstruction:   strings.TrimSpace(continuationInstruction),
	}
}

// Messages converts the artifact into a minimal [system instruction, user
// payload] pair. Prefer Render() and the agent loop's trailing-system-overlay
// request path for LLM calls: Messages() drops full session history and is
// retained only for legacy unit tests and gradual migration off synthetic
// scaffolds.
func (t TOTOF) Messages() []llm.Message {
	payload := t.Render()
	if payload == "" {
		return nil
	}
	var messages []llm.Message
	if strings.TrimSpace(t.FinalizationInstruction) != "" {
		messages = append(messages, llm.Message{
			Role:    "system",
			Content: t.FinalizationInstruction,
		})
	}
	messages = append(messages, llm.Message{
		Role:    "user",
		Content: payload,
	})
	return messages
}

func (t TOTOF) Render() string {
	var sections []string
	if task := strings.TrimSpace(t.UserTask); task != "" {
		sections = append(sections, "TASK\n"+task)
	}
	if len(t.AuthoritativeObservedResult) > 0 {
		sections = append(sections, "AUTHORITATIVE OBSERVED RESULTS\n"+renderBullets(t.AuthoritativeObservedResult))
	}
	if len(t.ToolOutcomes) > 0 {
		var outcomes []string
		for _, outcome := range t.ToolOutcomes {
			line := strings.TrimSpace(outcome.ToolName)
			if line == "" {
				line = "tool"
			}
			if status := strings.TrimSpace(outcome.Status); status != "" {
				line = fmt.Sprintf("%s [%s]", line, status)
			}
			if body := strings.TrimSpace(outcome.Outcome); body != "" {
				line = fmt.Sprintf("%s: %s", line, body)
			}
			outcomes = append(outcomes, line)
		}
		sections = append(sections, "KEY TOOL OUTCOMES\n"+renderBullets(outcomes))
	}
	if len(t.OpenIssues) > 0 {
		sections = append(sections, "OPEN ISSUES\n"+renderBullets(t.OpenIssues))
	}
	if instr := strings.TrimSpace(t.FinalizationInstruction); instr != "" {
		sections = append(sections, "FINALIZATION INSTRUCTION\n"+instr)
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

// Messages converts the artifact into a minimal [system instruction, user
// payload] pair. Prefer Render() and the trailing-system-overlay request path
// — see TOTOF.Messages() deprecation note.
func (c ContinuationArtifact) Messages() []llm.Message {
	payload := c.Render()
	if payload == "" {
		return nil
	}
	var messages []llm.Message
	if strings.TrimSpace(c.ContinuationInstruction) != "" {
		messages = append(messages, llm.Message{
			Role:    "system",
			Content: c.ContinuationInstruction,
		})
	}
	messages = append(messages, llm.Message{
		Role:    "user",
		Content: payload,
	})
	return messages
}

func (c ContinuationArtifact) Render() string {
	var sections []string
	if task := strings.TrimSpace(c.Task); task != "" {
		sections = append(sections, "TASK\n"+task)
	}
	if len(c.AuthoritativeObservations) > 0 {
		sections = append(sections, "AUTHORITATIVE OBSERVED RESULTS\n"+renderBullets(c.AuthoritativeObservations))
	}
	if len(c.ToolOutcomes) > 0 {
		var outcomes []string
		for _, outcome := range c.ToolOutcomes {
			line := strings.TrimSpace(outcome.ToolName)
			if line == "" {
				line = "tool"
			}
			if status := strings.TrimSpace(outcome.Status); status != "" {
				line = fmt.Sprintf("%s [%s]", line, status)
			}
			if body := strings.TrimSpace(outcome.Outcome); body != "" {
				line = fmt.Sprintf("%s: %s", line, body)
			}
			outcomes = append(outcomes, line)
		}
		sections = append(sections, "KEY TOOL OUTCOMES\n"+renderBullets(outcomes))
	}
	if len(c.OpenIssues) > 0 {
		sections = append(sections, "OPEN ISSUES\n"+renderBullets(c.OpenIssues))
	}
	if remaining := strings.TrimSpace(c.RemainingWork); remaining != "" {
		sections = append(sections, "REMAINING WORK\n"+remaining)
	}
	if instr := strings.TrimSpace(c.ContinuationInstruction); instr != "" {
		sections = append(sections, "CONTINUATION INSTRUCTION\n"+instr)
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func renderBullets(items []string) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		lines = append(lines, "- "+trimmed)
	}
	return strings.Join(lines, "\n")
}

func lastUserTask(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func currentObservationWindow(messages []llm.Message) ([]string, []TOTOFToolOutcome) {
	if len(messages) == 0 {
		return nil, nil
	}
	var toolMessages []llm.Message
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == "tool" {
			toolMessages = append(toolMessages, msg)
			continue
		}
		if len(toolMessages) > 0 {
			break
		}
	}
	if len(toolMessages) == 0 {
		return nil, nil
	}
	// Reverse back into chronological order.
	for i, j := 0, len(toolMessages)-1; i < j; i, j = i+1, j-1 {
		toolMessages[i], toolMessages[j] = toolMessages[j], toolMessages[i]
	}
	observed := make([]string, 0, len(toolMessages))
	outcomes := make([]TOTOFToolOutcome, 0, len(toolMessages))
	for _, msg := range toolMessages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		status := "ok"
		if strings.HasPrefix(content, "Error:") {
			status = "error"
		}
		observed = append(observed, content)
		outcomes = append(outcomes, TOTOFToolOutcome{
			ToolName: strings.TrimSpace(msg.Name),
			Status:   status,
			Outcome:  content,
		})
	}
	return observed, outcomes
}

func currentOpenIssues(s *Session) []string {
	if s == nil {
		return nil
	}
	var issues []string
	observed, outcomes := currentObservationWindow(s.messages)
	_ = observed
	for _, outcome := range outcomes {
		if outcome.Status == "error" {
			issues = append(issues, outcome.Outcome)
		}
	}
	if ve := s.VerificationEvidence(); ve != nil {
		for _, c := range ve.Contradictions {
			if summary := strings.TrimSpace(c.Summary); summary != "" {
				issues = append(issues, "Contradiction: "+summary)
			}
		}
		for _, q := range ve.UnresolvedQuestions {
			if strings.TrimSpace(q) != "" {
				issues = append(issues, "Unresolved question: "+q)
			}
		}
	}
	for i := len(s.messages) - 1; i >= 0 && len(issues) < 8; i-- {
		msg := s.messages[i]
		if msg.Role != "system" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		switch {
		case strings.HasPrefix(content, "Your previous response failed verification:"):
			issues = append(issues, content)
		case strings.HasPrefix(content, "Post-observation reflection requires more execution:"):
			issues = append(issues, content)
		case strings.HasPrefix(content, "Your previous response was rejected"):
			issues = append(issues, content)
		}
	}
	return dedupeStrings(issues)
}

func dedupeStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
