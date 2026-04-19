// Shortcut handler system.
//
// Provides a ShortcutHandler interface and concrete implementations for
// shortcuts that bypass full LLM inference. The dispatcher evaluates handlers
// in confidence order and respects correction_turn / delegation_provenance
// flags to prevent misfires.
//
// Ported from Rust: crates/roboticus-pipeline/src/shortcuts/conversational.rs

package pipeline

import (
	"fmt"
	"strings"
)

// ShortcutContext carries pipeline state that shortcuts need for decisions.
// Matches Rust's ShortcutContext with all fields for context-aware dispatch.
type ShortcutContext struct {
	CorrectionTurn         bool   // True if sarcasm/contradiction was detected
	DelegationProvenance   bool   // True if this turn was delegated from another agent
	HasConversationContext bool   // True if session has prior turns (turn > 0)
	AgentName              string // Agent identity for identity shortcuts
	CapabilitySummary      string // runtime-owned capability snapshot for introspection shortcuts
	SessionTurnCount       int    // Number of turns in the session
	PreviousAssistantText  string // Last assistant response for context
	ChannelLabel           string // Channel this turn arrived on
	PreparedModel          string // Model selected for inference
}

// ShortcutResult holds a matched shortcut response with metadata.
type ShortcutResult struct {
	Content    string  // The shortcut response text
	Handler    string  // Name of the handler that matched
	Confidence float64 // Match confidence 0–1
}

// ShortcutMatch holds a successful match with confidence and handler name.
type ShortcutMatch struct {
	Confidence float64
	Handler    string
}

// ShortcutHandler is the interface for shortcut implementations.
// Matches Rust's ShortcutHandler trait with 3 methods (Rule 5.2 narrow interfaces).
// TryMatch combines name, match detection, and confidence into one call.
type ShortcutHandler interface {
	// TryMatch checks if this handler can handle the input. Returns nil if no match,
	// or a ShortcutMatch with confidence and handler name.
	TryMatch(content string, ctx *ShortcutContext) *ShortcutMatch

	// Respond generates the shortcut response.
	Respond(content string, ctx *ShortcutContext) string
}

// ── AcknowledgementShortcut ────────────────────────────────────────────────
// Matches brief acknowledgements (ok, thanks, got it, etc.) and returns a
// minimal response. Skipped on correction turns to avoid mishandling sarcasm.
// Ported from Rust: AcknowledgementShortcut.

type AcknowledgementShortcut struct{}

var acknowledgements = []string{
	"ok", "okay", "thanks", "thank you", "got it", "understood", "k", "ty",
	"cool", "alright", "sure", "yep", "yes", "no problem", "np",
}

func (a *AcknowledgementShortcut) TryMatch(content string, ctx *ShortcutContext) *ShortcutMatch {
	// Never match on correction turns — sarcastic "sure" / "right" etc.
	if ctx != nil && ctx.CorrectionTurn {
		return nil
	}
	// Never match on delegated turns — delegation deserves full inference.
	if ctx != nil && ctx.DelegationProvenance {
		return nil
	}

	lower := strings.TrimSpace(strings.ToLower(content))
	for _, ack := range acknowledgements {
		if lower == ack {
			return &ShortcutMatch{Confidence: 0.95, Handler: "acknowledgement"}
		}
	}
	return nil
}

func (a *AcknowledgementShortcut) Respond(_ string, _ *ShortcutContext) string {
	return "Acknowledged. Let me know if you need anything else."
}

// ── IdentityShortcut ──────────────────────────────────────────────────────
// Matches identity queries ("who are you", "what are you").

type IdentityShortcut struct{}

func (i *IdentityShortcut) TryMatch(content string, _ *ShortcutContext) *ShortcutMatch {
	lower := strings.TrimSpace(strings.ToLower(content))
	if lower == "who are you" || lower == "who are you?" || lower == "what are you?" || lower == "what are you" {
		return &ShortcutMatch{Confidence: 0.99, Handler: "identity"}
	}
	return nil
}

func (i *IdentityShortcut) Respond(_ string, ctx *ShortcutContext) string {
	name := "an autonomous AI agent"
	if ctx != nil && ctx.AgentName != "" {
		name = ctx.AgentName
	}
	return fmt.Sprintf("I am %s, an autonomous AI agent.", name)
}

// ── HelpShortcut ──────────────────────────────────────────────────────────
// Matches "help" or "/help" and returns capability summary.

type HelpShortcut struct{}

func (h *HelpShortcut) TryMatch(content string, _ *ShortcutContext) *ShortcutMatch {
	lower := strings.TrimSpace(strings.ToLower(content))
	if lower == "help" || lower == "/help" {
		return &ShortcutMatch{Confidence: 0.99, Handler: "help"}
	}
	return nil
}

func (h *HelpShortcut) Respond(_ string, ctx *ShortcutContext) string {
	name := "This agent"
	if ctx != nil && ctx.AgentName != "" {
		name = ctx.AgentName
	}
	return fmt.Sprintf("%s can help with:\n- General conversation and reasoning\n- File operations and code tasks\n- Web search and information retrieval\n- Scheduling and reminders\n- Financial operations\n\nJust describe what you need.", name)
}

// ── IntrospectionShortcut ────────────────────────────────────────────────
// Matches capability/introspection questions and answers from runtime-owned
// summary state instead of sending the model into an exploratory tool loop.

type IntrospectionShortcut struct{}

func (i *IntrospectionShortcut) TryMatch(content string, ctx *ShortcutContext) *ShortcutMatch {
	if ctx == nil || strings.TrimSpace(ctx.CapabilitySummary) == "" {
		return nil
	}
	lower := strings.TrimSpace(strings.ToLower(content))
	markers := []string{
		"introspect",
		"introspection",
		"current subagent functionality",
		"what can you do",
		"what tools",
		"available tools",
		"capabilities",
		"subagent functionality",
		"what can your subagents do",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return &ShortcutMatch{Confidence: 0.98, Handler: "introspection"}
		}
	}
	return nil
}

func (i *IntrospectionShortcut) Respond(_ string, ctx *ShortcutContext) string {
	return ctx.CapabilitySummary
}

// ── DefaultShortcutHandlers ───────────────────────────────────────────────

// DefaultShortcutHandlers returns the standard set of shortcut handlers.
func DefaultShortcutHandlers() []ShortcutHandler {
	return []ShortcutHandler{
		&IntrospectionShortcut{},
		&IdentityShortcut{},
		&HelpShortcut{},
		&AcknowledgementShortcut{},
	}
}

// ── DispatchShortcut ──────────────────────────────────────────────────────
// Rich context shortcut dispatcher. Evaluates all handlers, picks the
// highest-confidence match, and returns the result with tracing metadata.
// Matches Rust's dispatch_shortcut().

// DispatchShortcut evaluates all shortcut handlers against the input and
// returns the best match, or nil if no handler matched.
//
// Respects correction_turn (sarcasm/contradiction should bypass acknowledgement
// shortcuts) and delegation_provenance (delegated turns should not be shortcutted).
func DispatchShortcut(handlers []ShortcutHandler, content string, ctx *ShortcutContext) *ShortcutResult {
	if ctx == nil {
		ctx = &ShortcutContext{}
	}

	// Confidence threshold: higher bar for active conversations to prevent
	// misfires on ambiguous short messages. Rust: 0.8 for conversations, 0.4 for stateless.
	threshold := 0.4
	if ctx.HasConversationContext {
		threshold = 0.8
	}

	var bestMatch *ShortcutMatch
	var bestHandler ShortcutHandler

	for _, h := range handlers {
		m := h.TryMatch(content, ctx)
		if m == nil {
			continue
		}
		if m.Confidence < threshold {
			continue // Below context-adjusted threshold.
		}
		if bestMatch == nil || m.Confidence > bestMatch.Confidence {
			bestMatch = m
			bestHandler = h
		}
	}

	if bestMatch == nil {
		return nil
	}

	return &ShortcutResult{
		Content:    bestHandler.Respond(content, ctx),
		Handler:    bestMatch.Handler,
		Confidence: bestMatch.Confidence,
	}
}
