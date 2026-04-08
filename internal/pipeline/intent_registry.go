package pipeline

import (
	"strings"

	"roboticus/internal/core"
)

// Intent represents a classified user intent.
type Intent string

const (
	IntentQuestion Intent = "question"
	IntentCommand  Intent = "command"
	IntentCreative Intent = "creative"
	IntentAnalysis Intent = "analysis"
	IntentChat     Intent = "chat"

	// Expanded semantic intents (Wave 8, #70).
	IntentModelIdentity    Intent = "model_identity"
	IntentAcknowledgement  Intent = "acknowledgement"
	IntentFinancialAction  Intent = "financial_action"
	IntentCreativeWriting  Intent = "creative_writing"
	IntentCodeGeneration   Intent = "code_generation"
	IntentSystemAdmin      Intent = "system_admin"
	IntentCurrentEvents    Intent = "current_events"
	IntentDelegation       Intent = "delegation"
	IntentMemoryQuery      Intent = "memory_query"
	IntentFileOperation    Intent = "file_operation"
	IntentWebSearch        Intent = "web_search"
	IntentScheduling       Intent = "scheduling"
	IntentMathCalculation  Intent = "math_calculation"
	IntentTranslation      Intent = "translation"
	IntentSummarization    Intent = "summarization"
	IntentDebug            Intent = "debug"
	IntentExplanation      Intent = "explanation"
	IntentRolePlay         Intent = "role_play"
	IntentImageGeneration  Intent = "image_generation"
	IntentDataExtraction   Intent = "data_extraction"
	IntentConversation     Intent = "conversation"
	IntentFeedback         Intent = "feedback"
	IntentNavigation       Intent = "navigation"
	IntentConfiguration    Intent = "configuration"
	IntentHealthCheck      Intent = "health_check"
	IntentPluginInvocation Intent = "plugin_invocation"
	IntentToolUse          Intent = "tool_use"
	IntentSecurityAudit    Intent = "security_audit"
	IntentDocumentation    Intent = "documentation"
	IntentRefactoring      Intent = "refactoring"
	IntentTesting          Intent = "testing"
)

// IntentMetadata carries routing and caching hints for a classified intent.
type IntentMetadata struct {
	Priority      int            // Higher = more urgent (0 = default)
	BypassCache   bool           // If true, never serve from cache
	PreferredTier core.ModelTier // Routing hint for model selection
}

// intentMetadata maps expanded intents to their metadata.
var intentMetadata = map[Intent]IntentMetadata{
	IntentModelIdentity:    {Priority: 10, BypassCache: true, PreferredTier: core.ModelTierSmall},
	IntentAcknowledgement:  {Priority: 1, BypassCache: false, PreferredTier: core.ModelTierSmall},
	IntentFinancialAction:  {Priority: 9, BypassCache: true, PreferredTier: core.ModelTierLarge},
	IntentCreativeWriting:  {Priority: 5, BypassCache: false, PreferredTier: core.ModelTierLarge},
	IntentCodeGeneration:   {Priority: 7, BypassCache: false, PreferredTier: core.ModelTierFrontier},
	IntentSystemAdmin:      {Priority: 8, BypassCache: true, PreferredTier: core.ModelTierMedium},
	IntentCurrentEvents:    {Priority: 6, BypassCache: true, PreferredTier: core.ModelTierMedium},
	IntentDelegation:       {Priority: 7, BypassCache: true, PreferredTier: core.ModelTierLarge},
	IntentMemoryQuery:      {Priority: 4, BypassCache: false, PreferredTier: core.ModelTierSmall},
	IntentFileOperation:    {Priority: 6, BypassCache: true, PreferredTier: core.ModelTierMedium},
	IntentWebSearch:        {Priority: 5, BypassCache: true, PreferredTier: core.ModelTierMedium},
	IntentScheduling:       {Priority: 6, BypassCache: true, PreferredTier: core.ModelTierMedium},
	IntentMathCalculation:  {Priority: 5, BypassCache: false, PreferredTier: core.ModelTierMedium},
	IntentTranslation:      {Priority: 4, BypassCache: false, PreferredTier: core.ModelTierMedium},
	IntentSummarization:    {Priority: 4, BypassCache: false, PreferredTier: core.ModelTierMedium},
	IntentDebug:            {Priority: 7, BypassCache: true, PreferredTier: core.ModelTierLarge},
	IntentExplanation:      {Priority: 4, BypassCache: false, PreferredTier: core.ModelTierMedium},
	IntentRolePlay:         {Priority: 3, BypassCache: false, PreferredTier: core.ModelTierLarge},
	IntentImageGeneration:  {Priority: 5, BypassCache: true, PreferredTier: core.ModelTierLarge},
	IntentDataExtraction:   {Priority: 5, BypassCache: false, PreferredTier: core.ModelTierMedium},
	IntentConversation:     {Priority: 2, BypassCache: false, PreferredTier: core.ModelTierSmall},
	IntentFeedback:         {Priority: 3, BypassCache: false, PreferredTier: core.ModelTierSmall},
	IntentNavigation:       {Priority: 3, BypassCache: false, PreferredTier: core.ModelTierSmall},
	IntentConfiguration:    {Priority: 7, BypassCache: true, PreferredTier: core.ModelTierMedium},
	IntentHealthCheck:      {Priority: 8, BypassCache: true, PreferredTier: core.ModelTierSmall},
	IntentPluginInvocation: {Priority: 6, BypassCache: true, PreferredTier: core.ModelTierMedium},
	IntentToolUse:          {Priority: 6, BypassCache: true, PreferredTier: core.ModelTierMedium},
	IntentSecurityAudit:    {Priority: 8, BypassCache: true, PreferredTier: core.ModelTierFrontier},
	IntentDocumentation:    {Priority: 4, BypassCache: false, PreferredTier: core.ModelTierMedium},
	IntentRefactoring:      {Priority: 6, BypassCache: false, PreferredTier: core.ModelTierLarge},
	IntentTesting:          {Priority: 5, BypassCache: false, PreferredTier: core.ModelTierMedium},
	IntentQuestion:         {Priority: 3, BypassCache: false, PreferredTier: core.ModelTierMedium},
	IntentCommand:          {Priority: 6, BypassCache: true, PreferredTier: core.ModelTierMedium},
	IntentCreative:         {Priority: 5, BypassCache: false, PreferredTier: core.ModelTierLarge},
	IntentAnalysis:         {Priority: 5, BypassCache: false, PreferredTier: core.ModelTierLarge},
	IntentChat:             {Priority: 1, BypassCache: false, PreferredTier: core.ModelTierSmall},
}

// GetIntentMetadata returns metadata for the given intent, or a zero-value default.
func GetIntentMetadata(intent Intent) IntentMetadata {
	if m, ok := intentMetadata[intent]; ok {
		return m
	}
	return IntentMetadata{}
}

// IntentClassifier classifies user input into an intent with confidence.
type IntentClassifier interface {
	Classify(content string) (Intent, float64)
}

// IntentClassifierFunc adapts a function to the IntentClassifier interface.
type IntentClassifierFunc func(string) (Intent, float64)

func (f IntentClassifierFunc) Classify(content string) (Intent, float64) {
	return f(content)
}

// IntentRegistry holds classifiers and picks the highest-confidence result.
type IntentRegistry struct {
	classifiers []IntentClassifier
}

// NewIntentRegistry creates a registry with the default keyword classifier.
func NewIntentRegistry() *IntentRegistry {
	return &IntentRegistry{
		classifiers: []IntentClassifier{&keywordClassifier{}},
	}
}

// AddClassifier adds a custom classifier. Custom classifiers are checked
// before the default keyword classifier.
func (ir *IntentRegistry) AddClassifier(c IntentClassifier) {
	// Prepend so custom classifiers take priority.
	ir.classifiers = append([]IntentClassifier{c}, ir.classifiers...)
}

// Classify returns the highest-confidence intent across all classifiers.
func (ir *IntentRegistry) Classify(content string) (Intent, float64) {
	bestIntent := IntentChat
	var bestConf float64

	for _, c := range ir.classifiers {
		intent, conf := c.Classify(content)
		if conf > bestConf {
			bestIntent = intent
			bestConf = conf
		}
	}
	return bestIntent, bestConf
}

// keywordClassifier uses simple keyword heuristics for intent classification.
type keywordClassifier struct{}

func (k *keywordClassifier) Classify(content string) (Intent, float64) {
	if content == "" {
		return IntentChat, 0.0
	}

	lower := strings.ToLower(strings.TrimSpace(content))

	// --- Highest-specificity intents first (before generic question/command/creative) ---

	// Slash commands: always take priority.
	if strings.HasPrefix(lower, "/") {
		return IntentCommand, 0.9
	}

	// Acknowledgements (exact match, highest confidence).
	for _, kw := range []string{"ok", "thanks", "thank you", "got it", "understood"} {
		if lower == kw || lower == kw+"." || lower == kw+"!" {
			return IntentAcknowledgement, 0.9
		}
	}

	// Model identity questions.
	for _, kw := range []string{"who are you", "what are you", "what model", "which model", "your name"} {
		if strings.Contains(lower, kw) {
			return IntentModelIdentity, 0.85
		}
	}

	// Financial actions.
	for _, kw := range []string{"transfer ", "send money", "pay ", "deposit", "withdraw", "balance", "wallet"} {
		if strings.Contains(lower, kw) {
			return IntentFinancialAction, 0.75
		}
	}

	// Code generation (checked before generic "write" creative).
	for _, kw := range []string{"write code", "implement ", "function ", "class ", "refactor", "debug "} {
		if strings.Contains(lower, kw) {
			return IntentCodeGeneration, 0.7
		}
	}

	// System admin (checked before generic "restart" command).
	for _, kw := range []string{"restart ", "shutdown", "config ", "configure ", "status ", "health"} {
		if strings.Contains(lower, kw) {
			return IntentSystemAdmin, 0.7
		}
	}

	// Summarization (checked before generic "summarize" analysis).
	for _, kw := range []string{"summarize", "tldr", "summary of", "recap ", "condense"} {
		if strings.Contains(lower, kw) {
			return IntentSummarization, 0.7
		}
	}

	// Scheduling.
	for _, kw := range []string{"schedule ", "remind ", "cron ", "timer ", "alarm "} {
		if strings.Contains(lower, kw) {
			return IntentScheduling, 0.7
		}
	}

	// File operations.
	for _, kw := range []string{"read file", "write file", "list files", "open file", "save file", "delete file"} {
		if strings.Contains(lower, kw) {
			return IntentFileOperation, 0.7
		}
	}

	// Web search.
	for _, kw := range []string{"search for", "look up", "find online", "google ", "web search"} {
		if strings.Contains(lower, kw) {
			return IntentWebSearch, 0.7
		}
	}

	// Translation.
	for _, kw := range []string{"translate ", "in spanish", "in french", "in german", "to english"} {
		if strings.Contains(lower, kw) {
			return IntentTranslation, 0.7
		}
	}

	// --- Generic intents (lower specificity) ---

	// Commands: imperative verbs (slash commands handled above).
	for _, verb := range []string{"run ", "execute ", "start ", "stop ", "deploy ", "install "} {
		if strings.HasPrefix(lower, verb) {
			return IntentCommand, 0.7
		}
	}

	// Questions: ends with ? or starts with question words
	if strings.HasSuffix(lower, "?") {
		return IntentQuestion, 0.8
	}
	for _, qw := range []string{"what ", "how ", "why ", "when ", "where ", "who ", "which ", "can ", "could ", "is ", "are ", "do ", "does "} {
		if strings.HasPrefix(lower, qw) {
			return IntentQuestion, 0.7
		}
	}

	// Creative: generation keywords
	for _, cw := range []string{"write ", "compose ", "create ", "generate ", "draft ", "poem", "story", "song"} {
		if strings.Contains(lower, cw) {
			return IntentCreative, 0.7
		}
	}

	// Analysis: data/analysis keywords
	for _, aw := range []string{"analyze ", "analyse ", "compare ", "evaluate ", "assess ", "review ", "breakdown"} {
		if strings.Contains(lower, aw) {
			return IntentAnalysis, 0.7
		}
	}

	// Default: chat — non-empty inputs that match nothing specific are informal chat
	return IntentChat, 0.5
}
