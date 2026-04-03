package pipeline

import (
	"context"

	"goboticus/internal/core"
	"goboticus/internal/llm"
	"goboticus/internal/session"
)

// InjectionChecker scores input text for prompt injection risk.
type InjectionChecker interface {
	CheckInput(text string) core.ThreatScore
	Sanitize(text string) string
}

// MemoryRetriever fetches relevant memories for context assembly.
// Returns a pre-formatted block of memory text ready for system prompt injection.
type MemoryRetriever interface {
	Retrieve(ctx context.Context, sessionID, query string, budget int) string
}

// SkillMatcher attempts to fulfill a request via skill triggers before LLM inference.
// Returns nil if no skill matches.
type SkillMatcher interface {
	TryMatch(ctx context.Context, session *session.Session, content string) *Outcome
}

// ToolExecutor runs the ReAct tool-calling loop for standard inference.
// This is the boundary between the pipeline (orchestration) and agent (reasoning).
type ToolExecutor interface {
	RunLoop(ctx context.Context, session *session.Session) (content string, turns int, err error)
}

// Ingestor handles post-turn memory ingestion in the background.
type Ingestor interface {
	IngestTurn(ctx context.Context, session *session.Session)
}

// NicknameRefiner generates session nicknames via LLM.
type NicknameRefiner interface {
	Refine(ctx context.Context, session *session.Session)
}

// StreamPreparer builds a streaming inference request without executing it.
type StreamPreparer interface {
	PrepareStream(ctx context.Context, session *session.Session) (*llm.Request, error)
}
