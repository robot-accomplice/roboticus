package pipeline_test

import (
	"context"

	"roboticus/internal/core"
	"roboticus/internal/llm"
	"roboticus/internal/pipeline"
	"roboticus/internal/session"
)

var (
	_ pipeline.InjectionChecker = (*mockInjectionChecker)(nil)
	_ pipeline.MemoryRetriever  = (*mockMemoryRetriever)(nil)
	_ pipeline.SkillMatcher     = (*mockSkillMatcher)(nil)
	_ pipeline.ToolExecutor     = (*mockToolExecutor)(nil)
	_ pipeline.Ingestor         = (*mockIngestor)(nil)
	_ pipeline.NicknameRefiner  = (*mockNicknameRefiner)(nil)
	_ pipeline.StreamPreparer   = (*mockStreamPreparer)(nil)
)

// mockInjectionChecker is a test double for pipeline.InjectionChecker.
type mockInjectionChecker struct {
	CheckInputFunc func(string) core.ThreatScore
	SanitizeFunc   func(string) string
}

func (m *mockInjectionChecker) CheckInput(text string) core.ThreatScore {
	if m.CheckInputFunc != nil {
		return m.CheckInputFunc(text)
	}
	return core.ThreatScore(0)
}

func (m *mockInjectionChecker) Sanitize(text string) string {
	if m.SanitizeFunc != nil {
		return m.SanitizeFunc(text)
	}
	return text
}

// mockMemoryRetriever is a test double for pipeline.MemoryRetriever.
type mockMemoryRetriever struct {
	RetrieveFunc func(ctx context.Context, sessionID, query string, budget int) string
}

func (m *mockMemoryRetriever) Retrieve(ctx context.Context, sessionID, query string, budget int) string {
	if m.RetrieveFunc != nil {
		return m.RetrieveFunc(ctx, sessionID, query, budget)
	}
	return ""
}

// mockSkillMatcher is a test double for pipeline.SkillMatcher.
type mockSkillMatcher struct {
	TryMatchFunc func(ctx context.Context, session *session.Session, content string) *pipeline.Outcome
}

func (m *mockSkillMatcher) TryMatch(ctx context.Context, session *session.Session, content string) *pipeline.Outcome {
	if m.TryMatchFunc != nil {
		return m.TryMatchFunc(ctx, session, content)
	}
	return nil
}

// mockToolExecutor is a test double for pipeline.ToolExecutor.
type mockToolExecutor struct {
	RunLoopFunc func(ctx context.Context, session *session.Session) (string, int, error)
}

func (m *mockToolExecutor) RunLoop(ctx context.Context, session *session.Session) (string, int, error) {
	if m.RunLoopFunc != nil {
		return m.RunLoopFunc(ctx, session)
	}
	return "mock response", 1, nil
}

// mockIngestor is a test double for pipeline.Ingestor.
type mockIngestor struct {
	IngestTurnFunc func(ctx context.Context, session *session.Session)
}

func (m *mockIngestor) IngestTurn(ctx context.Context, session *session.Session) {
	if m.IngestTurnFunc != nil {
		m.IngestTurnFunc(ctx, session)
	}
}

// mockNicknameRefiner is a test double for pipeline.NicknameRefiner.
type mockNicknameRefiner struct {
	RefineFunc func(ctx context.Context, session *session.Session)
}

func (m *mockNicknameRefiner) Refine(ctx context.Context, session *session.Session) {
	if m.RefineFunc != nil {
		m.RefineFunc(ctx, session)
	}
}

// mockStreamPreparer is a test double for pipeline.StreamPreparer.
type mockStreamPreparer struct {
	PrepareStreamFunc func(ctx context.Context, session *session.Session) (*llm.Request, error)
}

func (m *mockStreamPreparer) PrepareStream(ctx context.Context, session *session.Session) (*llm.Request, error) {
	if m.PrepareStreamFunc != nil {
		return m.PrepareStreamFunc(ctx, session)
	}
	return &llm.Request{Stream: true}, nil
}
