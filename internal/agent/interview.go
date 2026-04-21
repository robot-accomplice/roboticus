package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"roboticus/internal/core"
)

// InterviewCategory represents one of the 8 personality interview domains.
type InterviewCategory string

const (
	CatIdentity      InterviewCategory = "identity_voice"
	CatCommunication InterviewCategory = "communication_style"
	CatProactiveness InterviewCategory = "proactiveness_autonomy"
	CatDomain        InterviewCategory = "domain_expertise"
	CatBoundaries    InterviewCategory = "boundaries_guardrails"
	CatOperator      InterviewCategory = "operator_profile"
	CatGoals         InterviewCategory = "goals_directives"
	CatIntegrations  InterviewCategory = "integrations_workflow"
)

// AllInterviewCategories lists all 8 categories.
var AllInterviewCategories = []InterviewCategory{
	CatIdentity, CatCommunication, CatProactiveness, CatDomain,
	CatBoundaries, CatOperator, CatGoals, CatIntegrations,
}

// MinCategoriesForGeneration is the minimum coverage before TOML generation.
const MinCategoriesForGeneration = 5

// InterviewState tracks an in-progress personality interview.
type InterviewState struct {
	mu                sync.Mutex
	SessionID         string
	StartedAt         time.Time
	Turns             []InterviewTurn
	CoveredCategories map[InterviewCategory]bool
	Finished          bool
}

// InterviewTurn is a single Q&A exchange in the interview.
type InterviewTurn struct {
	Question string            `json:"question"`
	Answer   string            `json:"answer"`
	Category InterviewCategory `json:"category"`
}

// NewInterviewState creates a fresh interview.
func NewInterviewState(sessionID string) *InterviewState {
	return &InterviewState{
		SessionID:         sessionID,
		StartedAt:         time.Now(),
		CoveredCategories: make(map[InterviewCategory]bool),
	}
}

// AddTurn records a Q&A exchange and marks the category as covered.
func (s *InterviewState) AddTurn(category InterviewCategory, question, answer string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Turns = append(s.Turns, InterviewTurn{
		Question: question,
		Answer:   answer,
		Category: category,
	})
	s.CoveredCategories[category] = true
}

// Coverage returns the number of covered categories.
func (s *InterviewState) Coverage() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.CoveredCategories)
}

// CanGenerate returns true if enough categories are covered.
func (s *InterviewState) CanGenerate() bool {
	return s.Coverage() >= MinCategoriesForGeneration
}

// BuildInterviewPrompt returns the system prompt for the personality interview.
func BuildInterviewPrompt() string {
	return core.InterviewSystemPrompt()
}

// GeneratePersonalityTOML produces the 4 TOML configuration files from interview answers.
func GeneratePersonalityTOML(state *InterviewState) map[string]string {
	state.mu.Lock()
	defer state.mu.Unlock()

	// Group answers by category.
	answers := make(map[InterviewCategory][]string)
	for _, turn := range state.Turns {
		answers[turn.Category] = append(answers[turn.Category], turn.Answer)
	}

	result := make(map[string]string, 4)

	// OS.toml — identity and voice.
	var os strings.Builder
	os.WriteString("[identity]\n")
	os.WriteString("version = \"1.0\"\n")
	os.WriteString("generated_by = \"interview\"\n\n")
	os.WriteString("[voice]\n")
	os.WriteString("formality = \"balanced\"\n")
	os.WriteString("proactiveness = \"suggest\"\n")
	os.WriteString("verbosity = \"concise\"\n")
	os.WriteString("humor = \"dry\"\n")
	os.WriteString("domain = \"general\"\n\n")
	if vals, ok := answers[CatIdentity]; ok {
		fmt.Fprintf(&os, "# Based on interview:\n# %s\n\n", strings.Join(vals, "\n# "))
	}
	os.WriteString("prompt_text = \"\"\"\nYou are a helpful AI assistant.\n\"\"\"\n")
	result["OS.toml"] = os.String()

	// FIRMWARE.toml — guardrails.
	var fw strings.Builder
	fw.WriteString("[approvals]\n")
	fw.WriteString("spending_threshold = 50.0\n")
	fw.WriteString("require_confirmation = \"risky\"\n\n")
	if vals, ok := answers[CatBoundaries]; ok {
		for _, v := range vals {
			fmt.Fprintf(&fw, "[[rules]]\nrule_type = \"boundary\"\nrule = %q\n\n", v)
		}
	}
	result["FIRMWARE.toml"] = fw.String()

	// OPERATOR.toml — user profile.
	var op strings.Builder
	op.WriteString("[identity]\n")
	if vals, ok := answers[CatOperator]; ok && len(vals) > 0 {
		fmt.Fprintf(&op, "context = %q\n", strings.Join(vals, " "))
	}
	op.WriteString("\n[preferences]\n")
	if vals, ok := answers[CatCommunication]; ok && len(vals) > 0 {
		fmt.Fprintf(&op, "communication_notes = %q\n", strings.Join(vals, " "))
	}
	result["OPERATOR.toml"] = op.String()

	// DIRECTIVES.toml — goals.
	var dir strings.Builder
	dir.WriteString("[goals]\n")
	if vals, ok := answers[CatGoals]; ok {
		for i, v := range vals {
			fmt.Fprintf(&dir, "goal_%d = %q\n", i+1, v)
		}
	}
	dir.WriteString("\n[integrations]\n")
	if vals, ok := answers[CatIntegrations]; ok && len(vals) > 0 {
		fmt.Fprintf(&dir, "platforms = %q\n", strings.Join(vals, ", "))
	}
	result["DIRECTIVES.toml"] = dir.String()

	return result
}
