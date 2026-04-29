package pipeline

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"roboticus/testutil"
)

// behaviorCheck verifies a behavioral property of a pipeline response.
type behaviorCheck func(content string) (pass bool, detail string)

type behaviorScenario struct {
	name   string
	prompt string
	// mockResponse is the canned LLM response for this scenario.
	// The guard chain and pipeline post-processing run on it.
	mockResponse string
	checks       []behaviorCheck
}

// --- Marker sets (matching Rust run-agent-behavior-soak.py) ---

var staleMarkers = []string{
	"as of my last update",
	"as of my last training",
	"cannot provide real-time updates",
	"can't provide real-time updates",
	"as of early 2023",
	"as of 2023",
}

var internalMetadataMarkers = []string{
	"delegated_subagent=",
	"selected_subagent=",
	"subtask 1 ->",
	"subtask 2 ->",
	"expected_utility_margin",
	"decomposition gate decision",
}

var soakForeignIdentityMarkers = []string{
	"as an ai developed by microsoft",
	"as an ai language model",
	"as an ai text-based interface",
	"i am claude",
	"i'm claude",
	"i am chatgpt",
	"i'm chatgpt",
}

var filesystemDenialMarkers = []string{
	"can't access your files",
	"cannot access your files",
	"can't access your folders",
	"cannot access your folders",
	"don't have access to your files",
	"as an ai, i don't have access to your files",
	"as an ai text-based interface, i'm not able to directly access",
}

var execBlockMarkers = []string{
	"i did not execute a tool",
	"i did not execute a delegated subagent task",
	"i did not execute a cron scheduling tool",
}

// --- Check functions ---

func behaviorCheckNoStale(content string) (bool, string) {
	lower := strings.ToLower(content)
	for _, m := range staleMarkers {
		if strings.Contains(lower, m) {
			return false, "contains stale-knowledge marker: " + m
		}
	}
	return true, "no stale-knowledge markers"
}

func behaviorCheckNoInternalMetadata(content string) (bool, string) {
	lower := strings.ToLower(content)
	for _, m := range internalMetadataMarkers {
		if strings.Contains(lower, m) {
			return false, "contains internal metadata: " + m
		}
	}
	return true, "no internal delegation/orchestration metadata"
}

func behaviorCheckNoForeignIdentity(content string) (bool, string) {
	lower := strings.ToLower(content)
	for _, m := range soakForeignIdentityMarkers {
		if strings.Contains(lower, m) {
			return false, "contains foreign identity boilerplate: " + m
		}
	}
	return true, "no foreign identity boilerplate"
}

func behaviorCheckNoExecBlock(content string) (bool, string) {
	lower := strings.ToLower(content)
	for _, m := range execBlockMarkers {
		if strings.Contains(lower, m) {
			return false, "contains false execution block: " + m
		}
	}
	return true, "no false execution/delegation block message"
}

func behaviorCheckNoFilesystemDenial(content string) (bool, string) {
	lower := strings.ToLower(content)
	for _, m := range filesystemDenialMarkers {
		if strings.Contains(lower, m) {
			return false, "contains false filesystem denial: " + m
		}
	}
	return true, "no false filesystem-access denial"
}

func behaviorCheckMinLength(n int) behaviorCheck {
	return func(content string) (bool, string) {
		if len(strings.TrimSpace(content)) < n {
			return false, "content too short for substantive response"
		}
		return true, "content is substantive"
	}
}

func behaviorCheckContainsAny(label string, markers []string) behaviorCheck {
	return func(content string) (bool, string) {
		lower := strings.ToLower(content)
		for _, m := range markers {
			if strings.Contains(lower, m) {
				return true, label + ": found " + m
			}
		}
		return false, label + ": none of the expected markers found"
	}
}

func behaviorCheckNotEmpty(content string) (bool, string) {
	if strings.TrimSpace(content) == "" {
		return false, "empty response"
	}
	return true, "non-empty response"
}

func behaviorCheckNoOverbroadRefusal(content string) (bool, string) {
	lower := strings.ToLower(content)
	blockers := []string{
		"i cannot provide quotes related to ongoing conflicts",
		"sensitive geopolitical situations",
		"if you have other requests that do not involve sensitive topics",
	}
	for _, m := range blockers {
		if strings.Contains(lower, m) {
			return false, "overbroad refusal on contextual quoting: " + m
		}
	}
	return true, "no overbroad refusal on quote context"
}

// --- Scenarios ---

// TestBehaviorSoak_LiveScenarios runs each scenario through the full pipeline
// with a stub executor, verifying that guards, formatters, and post-processing
// produce the correct behavioral properties.
//
// This mirrors Rust's run-agent-behavior-soak.py scenarios.
func TestBehaviorSoak_LiveScenarios(t *testing.T) {
	store := testutil.TempStore(t)

	scenarios := []behaviorScenario{
		{
			name:         "acknowledgement_sla",
			prompt:       "Good evening. Acknowledge this request in one sentence, then wait.",
			mockResponse: "Acknowledged — standing by for further instructions.",
			checks:       []behaviorCheck{behaviorCheckNotEmpty, behaviorCheckNoStale, behaviorCheckNoForeignIdentity},
		},
		{
			name:         "introspection_discovery",
			prompt:       "use your introspection tool to discover your current subagent functionality and summarize it for me",
			mockResponse: "I have access to several subagent capabilities including research, monitoring, and reporting tools. My runtime shows 3 active sessions, configured memory tiers, and workspace integration for tool execution.",
			checks: []behaviorCheck{
				behaviorCheckNotEmpty, behaviorCheckNoExecBlock, behaviorCheckNoStale, behaviorCheckNoForeignIdentity,
				behaviorCheckMinLength(80),
				behaviorCheckContainsAny("introspection evidence", []string{
					"subagent", "tool", "capability", "memory", "runtime",
					"session", "workspace", "active", "configured", "available",
				}),
			},
		},
		{
			name:         "tool_random_use",
			prompt:       "tell me about the tools you can use, pick one at random, and use it",
			mockResponse: "I used the introspection tool and it returned the following status: 2 active sessions, memory utilization at 45%, and 5 available tools configured.",
			checks: []behaviorCheck{
				behaviorCheckNotEmpty, behaviorCheckNoExecBlock, behaviorCheckNoStale, behaviorCheckNoForeignIdentity,
				behaviorCheckContainsAny("tool-use evidence", []string{
					"output", "returned", "result", "tool", "status", "shows",
				}),
			},
		},
		{
			name:         "model_identity",
			prompt:       "can you confirm for me what model you are running?",
			mockResponse: "I am currently running on the test-model provider, which is the primary model configured for this session.",
			checks: []behaviorCheck{
				behaviorCheckNotEmpty, behaviorCheckNoStale, behaviorCheckNoForeignIdentity,
				behaviorCheckContainsAny("model identity", []string{
					"model", "running", "current", "powered", "using", "configured",
				}),
			},
		},
		{
			name:         "delegation",
			prompt:       "order a subagent to list the markdown files and return the count only",
			mockResponse: "Found 12 markdown files in the target directory.",
			checks: []behaviorCheck{
				behaviorCheckNotEmpty, behaviorCheckNoExecBlock, behaviorCheckNoInternalMetadata, behaviorCheckNoForeignIdentity,
			},
		},
		{
			name:         "geopolitical_sitrep",
			prompt:       "What's the geopolitical situation?",
			mockResponse: "The current geopolitical landscape features several flashpoints: ongoing tensions in Eastern Europe, trade disputes in the Pacific, and diplomatic negotiations in the Middle East.",
			checks: []behaviorCheck{
				behaviorCheckNotEmpty, behaviorCheckNoExecBlock, behaviorCheckNoStale, behaviorCheckNoInternalMetadata, behaviorCheckNoForeignIdentity,
				behaviorCheckContainsAny("geopolitical quality", []string{
					"geopolitical", "situation", "conflict", "flashpoint", "tension",
				}),
			},
		},
		{
			name:         "cron",
			prompt:       "schedule a cron job that runs every 5 minutes and tell me exactly what was scheduled",
			mockResponse: "Created cron job 'periodic-check' with schedule */5 * * * * — runs every 5 minutes. ID: cron-abc123.",
			checks: []behaviorCheck{
				behaviorCheckNotEmpty, behaviorCheckNoExecBlock, behaviorCheckNoForeignIdentity,
				behaviorCheckContainsAny("cron creation evidence", []string{
					"*/5", "every 5", "5 minute", "scheduled", "created", "cron",
				}),
			},
		},
		{
			name:         "tilde_distribution",
			prompt:       "give me the file distribution in the folder ~",
			mockResponse: "File distribution for /Users/jmachen: 42 directories found. Breakdown: Documents (15 files), Downloads (23 files), code (150+ files across multiple projects).",
			checks: []behaviorCheck{
				behaviorCheckNotEmpty, behaviorCheckNoExecBlock, behaviorCheckNoForeignIdentity,
				behaviorCheckContainsAny("distribution evidence", []string{
					"distribution", "directory", "files", "breakdown", "overview", "folder",
				}),
			},
		},
		{
			name:         "folder_scan",
			prompt:       "Now look in my Downloads folder",
			mockResponse: "Scanning Downloads directory: found 23 files including 5 PDFs, 8 images, and 10 miscellaneous documents.",
			checks: []behaviorCheck{
				behaviorCheckNotEmpty, behaviorCheckNoExecBlock, behaviorCheckNoFilesystemDenial, behaviorCheckNoForeignIdentity,
				behaviorCheckContainsAny("folder scan evidence", []string{
					"scan", "files", "directory", "folder", "list", "results", "found",
				}),
			},
		},
		{
			name:         "subagent_capabilities",
			prompt:       "What can your subagents do?",
			mockResponse: "My subagents can handle specialized tasks: the research agent gathers information, the monitoring agent tracks system health, and the planner agent helps with task decomposition and scheduling.",
			checks: []behaviorCheck{
				behaviorCheckNotEmpty, behaviorCheckNoExecBlock, behaviorCheckNoStale, behaviorCheckNoForeignIdentity,
				behaviorCheckMinLength(40),
				behaviorCheckContainsAny("subagent capability markers", []string{
					"subagent", "delegate", "research", "monitor", "report", "planner",
				}),
			},
		},
		{
			name:         "dune_quote_context",
			prompt:       "Give me an appropriate dune quote for the conflict in Iran",
			mockResponse: "\"Fear is the mind-killer\" — a fitting reflection on the psychology of conflict. The Dune universe explores how power struggles shape civilizations.",
			checks: []behaviorCheck{
				behaviorCheckNotEmpty, behaviorCheckNoForeignIdentity, behaviorCheckNoOverbroadRefusal,
			},
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			// Build pipeline with mock executor returning this scenario's response.
			pipe := New(PipelineDeps{
				Store:    store,
				Executor: &stubExecutor{response: sc.mockResponse},
				Guards:   DefaultGuardChain(),
				BGWorker: testutil.BGWorker(t, 4),
			})

			outcome, err := RunPipeline(t.Context(), pipe, PresetAPI(), Input{
				Content:  sc.prompt,
				Platform: "api",
				SenderID: "soak-test",
			})
			if err != nil {
				t.Fatalf("pipeline error: %v", err)
			}

			content := outcome.Content
			if content == "" {
				t.Fatal("pipeline returned empty content")
			}

			// Run all scenario checks against the pipeline output.
			for _, check := range sc.checks {
				pass, detail := check(content)
				if !pass {
					t.Errorf("check failed: %s (content=%q)", detail, truncateBehavior(content, 120))
				}
			}

			// Universal invariants: no internal markers survive.
			for _, marker := range []string{"[INTERNAL]", "[DELEGATION]", "[SYSTEM_NOTE]"} {
				if strings.Contains(content, marker) {
					t.Errorf("internal marker %q survived pipeline", marker)
				}
			}

			// Universal: session must be assigned.
			if outcome.SessionID == "" {
				t.Error("pipeline did not assign session_id")
			}
		})
	}
}

// TestBehaviorSoak_MixedToolAndConversationContinuity25Turns models the
// normal broad-to-narrow operator flow that exposed the recent regressions:
// start with a broad request, narrow through corrections, use tools, wait, and
// resume with shorthand without losing repository access or pending action
// context.
func TestBehaviorSoak_MixedToolAndConversationContinuity25Turns(t *testing.T) {
	store := testutil.TempStore(t)
	exec := &continuitySoakExecutor{t: t}
	ingestor := &continuitySoakIngestor{}

	pipe := New(PipelineDeps{
		Store:    store,
		Executor: exec,
		Ingestor: ingestor,
		Guards:   DefaultGuardChain(),
		BGWorker: testutil.BGWorker(t, 8),
	})

	var sessionID string
	for i, prompt := range mixedContinuityPrompts() {
		outcome, err := RunPipeline(context.Background(), pipe, PresetAPI(), Input{
			Content:   prompt,
			SessionID: sessionID,
			Platform:  "api",
			SenderID:  "continuity-soak",
			AgentID:   "duncan",
			AgentName: "Duncan",
			NoCache:   true,
		})
		if err != nil {
			t.Fatalf("turn %d pipeline error: %v", i+1, err)
		}
		if strings.TrimSpace(outcome.Content) == "" {
			t.Fatalf("turn %d returned empty content", i+1)
		}
		if sessionID == "" {
			sessionID = outcome.SessionID
		}
		if outcome.SessionID != sessionID {
			t.Fatalf("turn %d changed session: got %q want %q", i+1, outcome.SessionID, sessionID)
		}
		if looksLikeGreetingReset(outcome.Content) {
			t.Fatalf("turn %d reset to greeting instead of preserving continuity: %q", i+1, outcome.Content)
		}
		if containsFalseAccessOrCapabilityDenial(outcome.Content) {
			t.Fatalf("turn %d emitted false access/capability denial: %q", i+1, outcome.Content)
		}
		assertContinuitySoakProgression(t, i+1, outcome.Content)
	}
	if exec.calls < 25 {
		t.Fatalf("executor calls = %d, want at least 25", exec.calls)
	}

	var userMessages, assistantMessages int
	err := store.QueryRowContext(context.Background(),
		`SELECT
			COALESCE(SUM(CASE WHEN role = 'user' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN role = 'assistant' THEN 1 ELSE 0 END), 0)
		 FROM session_messages WHERE session_id = ?`,
		sessionID,
	).Scan(&userMessages, &assistantMessages)
	if err != nil {
		t.Fatalf("query session messages: %v", err)
	}
	if userMessages != 25 || assistantMessages != 25 {
		t.Fatalf("message counts user=%d assistant=%d, want 25/25", userMessages, assistantMessages)
	}

	pipe.bgWorker.Drain(5 * time.Second)
	if got := ingestor.Count(); got != 25 {
		t.Fatalf("memory mining ingress count = %d, want 25", got)
	}
	if !ingestor.SawAll("architecture documents", "ghola", "Playwright", "pending extraction") {
		t.Fatalf("memory mining snapshots did not preserve the mixed continuity context: %q", ingestor.Joined())
	}
	assertNoFrameworkContinuationScaffold(t, ingestor.Joined())
	if exec.finalSession != nil {
		assertNoFrameworkContinuationScaffold(t, joinSessionMessages(exec.finalSession))
	}
	if os.Getenv("ROBOTICUS_SOAK_TRANSCRIPT") == "1" {
		t.Logf("mixed continuity transcript:\n%s", joinSessionMessages(exec.finalSession))
	}
}

type continuitySoakExecutor struct {
	t            *testing.T
	calls        int
	finalSession *Session
}

func (e *continuitySoakExecutor) RunLoop(_ context.Context, session *Session) (string, int, error) {
	e.t.Helper()
	latest := strings.ToLower(latestTurnExecutionContent(session))
	switch {
	case isExplicitPleaseDoContinuation(latest) && strings.Contains(joinSessionMessages(session), "Metacritic"):
		expectTurnExecutionContains("previous assistant reply", "page html")(e.t, session)
	case isExplicitPleaseDoContinuation(latest) && strings.Contains(joinSessionMessages(session), "architecture documents"):
		expectTurnExecutionContains("PENDING ACTION CONFIRMED", "architecture documents")(e.t, session)
	case isExplicitGoAheadContinuation(latest) && strings.Contains(joinSessionMessages(session), "docs/architecture"):
		expectTurnExecutionContains("PENDING ACTION CONFIRMED", "docs/architecture")(e.t, session)
	case isStateContinuationEnvelope(latest) && strings.Contains(latest, "score elements"):
		expectTurnExecutionContains("previous assistant reply", "score elements")(e.t, session)
	case strings.Contains(latest, "use the evidence from the repo inspection"):
		expectSessionContains("/Users/jmachen/code/roboticus", "architecture documents", "docs/architecture")(e.t, session)
	case strings.Contains(latest, "you said you were doing this"):
		expectSessionContains("ghola", "Metacritic", "retrieved HTML")(e.t, session)
	case strings.Contains(latest, "where did we land"):
		expectSessionContains("architecture documents", "ghola", "Playwright", "pending extraction")(e.t, session)
	}
	if strings.Contains(latest, "review all of the subdirectories associated with the project") {
		expectTurnToolProfile(ToolProfileFocusedInspection)(e.t, session)
	}

	if strings.Contains(latest, "where are the architecture documents") {
		session.AddToolResult("repo-list", "list_directory", "cmd\ndocs\ninternal\nscripts\ntestutil", false)
	}
	if strings.Contains(latest, "use the evidence from the repo inspection") {
		session.AddToolResult("arch-read", "read_file", "docs/architecture-gap-report.md and docs/architecture-rules-diagrams.md inspected", false)
	}
	if strings.Contains(latest, "you said you were doing this") {
		session.AddToolResult("ghola-fetch", "ghola", "Metacritic main page retrieved HTML title: Movie Reviews, TV Reviews, Game Reviews, and Music Reviews - Metacritic", false)
	}
	if isStateContinuationEnvelope(latest) && strings.Contains(latest, "score elements") {
		session.AddToolResult("browser-metacritic", "browser_navigate", "Playwright opened https://www.metacritic.com successfully", false)
	}

	content := continuityResponseForLatestUser(latest)
	e.calls++
	session.AddAssistantMessage(content, nil)
	e.finalSession = session
	return content, 1, nil
}

type continuitySoakIngestor struct {
	mu        sync.Mutex
	snapshots []string
}

func (i *continuitySoakIngestor) IngestTurn(_ context.Context, session *Session) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.snapshots = append(i.snapshots, joinSessionMessages(session))
}

func (i *continuitySoakIngestor) Count() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.snapshots)
}

func (i *continuitySoakIngestor) SawAll(markers ...string) bool {
	joined := strings.ToLower(i.Joined())
	for _, marker := range markers {
		if !strings.Contains(joined, strings.ToLower(marker)) {
			return false
		}
	}
	return true
}

func (i *continuitySoakIngestor) Joined() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return strings.Join(i.snapshots, "\n--- snapshot ---\n")
}

func mixedContinuityPrompts() []string {
	return []string{
		"Please review the code at ~/code/roboticus and tell me if you have any suggestions",
		"That's useful, but review the code itself starting with the architecture documents and see if best practices are being followed",
		"Please do",
		"Where are the architecture documents in this repository?",
		"Focus on docs/architecture-gap-report.md and docs/architecture-rules-diagrams.md first",
		"go ahead",
		"The architecture documents are within the code repository you already reviewed",
		"Please review all of the subdirectories associated with the project at ~/code/roboticus and try to locate the architecture documentation. When you find it, review that documentation and compare it directly with the code. Then provide me with a summary of the alignment between architecture documentation and code implementation.",
		"What problem are we trying to solve with this review?",
		"Now narrow that to the agent continuity and tool-use seams",
		"Use the evidence from the repo inspection before answering",
		"Now switch context: see if the ghola tool can pull the main page of www.metacritic.com",
		"Assume the first fetch only gave headers; what should happen next?",
		"Please do",
		"waiting patiently",
		"You said you were doing this; continue from that exact pending action",
		"Can you use Playwright to surf the Metacritic page?",
		"Get the Metacritic score for Vampire Crawlers",
		"If the score isn't directly extracted, what should the next step be?",
		"Please proceed",
		"Now return to the code review thread. What architectural rule was violated by false access denial?",
		"Use the repository evidence we already established",
		"Summarize what tests should catch this in the future",
		"Also include the web/tool continuity issue",
		"Where did we land, and what remains unresolved?",
	}
}

func continuityResponseForLatestUser(latest string) string {
	switch {
	case isExplicitPleaseDoContinuation(latest) && strings.Contains(latest, "architecture documents"):
		return "I will proceed with the architecture-document review from the repository context already established."
	case isExplicitGoAheadContinuation(latest):
		return "I am continuing the architecture comparison using the repository documents already identified."
	case isExplicitPleaseDoContinuation(latest) && (strings.Contains(latest, "parse") || strings.Contains(latest, "page html")):
		return "I will proceed by parsing the retrieved Metacritic content from the pending ghola task."
	case isStateContinuationEnvelope(latest) && strings.Contains(latest, "score elements"):
		return "I will proceed with Playwright-backed score extraction from the existing Metacritic context."
	case strings.Contains(latest, "please review the code"):
		return "I inspected /Users/jmachen/code/roboticus and found cmd, docs, internal, scripts, and testutil. I should next review the architecture documents before judging code quality. Would you like me to proceed?"
	case strings.Contains(latest, "starting with the architecture documents"):
		return "The next action is to read the repository architecture documents under docs and compare their rules to implementation seams. Please confirm if I should proceed."
	case strings.Contains(latest, "where are the architecture documents"):
		return "The architecture documents are in the repository docs directory, especially docs/architecture-gap-report.md and docs/architecture-rules-diagrams.md."
	case strings.Contains(latest, "focus on docs/architecture"):
		return "I will focus on docs/architecture-gap-report.md and docs/architecture-rules-diagrams.md, then compare those rules to code paths. Should I proceed with that comparison?"
	case strings.Contains(latest, "within the code repository"):
		return "Correct; the documents are inside /Users/jmachen/code/roboticus, so no separate vault allowlist is required."
	case strings.Contains(latest, "look at the subfolders") ||
		strings.Contains(latest, "review all of the subdirectories associated with the project"):
		return "I found the docs under the repository and compared them against code ownership seams without inventing an access denial."
	case strings.Contains(latest, "what problem are we trying to solve"):
		return "We are trying to determine whether implementation behavior still matches the architecture contract, especially continuity and tool ownership."
	case strings.Contains(latest, "agent continuity and tool-use seams"):
		return "The high-risk seams are session continuity, tool capability selection, and guard behavior around access/capability truth."
	case strings.Contains(latest, "use the evidence from the repo inspection"):
		return "Using the repo evidence, the rule is that observed repository access remains authoritative until contradicted by a real tool or policy denial."
	case strings.Contains(latest, "ghola tool"):
		return "I can use ghola for the Metacritic page. If it only returns headers, the next action is to parse the retrieved HTML or use a browser-capable tool. Would you like me to proceed?"
	case strings.Contains(latest, "first fetch only gave headers"):
		return "The next step is to inspect page HTML/body content, not reset to a greeting or claim there are no browsing tools."
	case strings.Contains(latest, "waiting patiently"):
		return "I am still on the Metacritic extraction task; waiting does not clear the pending action."
	case strings.Contains(latest, "you said you were doing this"):
		return "I will continue the exact pending extraction instead of treating this as a new conversation."
	case strings.Contains(latest, "playwright"):
		return "Playwright is an available browser surface and should be used when selected or explicitly requested."
	case strings.Contains(latest, "vampire crawlers"):
		return "I attempted to extract the Vampire Crawlers score; if the page lacks a direct score, I should inspect structured data and visible score elements next. Please confirm if I should proceed."
	case strings.Contains(latest, "next step be"):
		return "The next step is targeted parsing of score elements and structured data from the observed page."
	case strings.Contains(latest, "return to the code review"):
		return "The architectural rule violated by false access denial is continuity truth: proven repository access cannot be discarded by a later generic response."
	case strings.Contains(latest, "repository evidence"):
		return "The established repository evidence remains /Users/jmachen/code/roboticus with architecture docs under docs."
	case strings.Contains(latest, "tests should catch"):
		return "Future tests should include a 25-turn mixed continuity soak, false access denial guard tests, and tool-capability continuity checks."
	case strings.Contains(latest, "web/tool continuity"):
		return "The web/tool issue is the same class: selected or proven browser/ghola capability must not collapse into a generic no-browsing claim."
	case strings.Contains(latest, "where did we land"):
		return "We landed on a systemic continuity requirement: preserve observed facts, pending actions, and tool capability truth across broad-to-narrow interaction. Remaining work is live-model validation."
	default:
		return "Continuity response preserving the current session context."
	}
}

func assertContinuitySoakProgression(t *testing.T, turn int, content string) {
	t.Helper()
	lower := strings.ToLower(content)
	switch turn {
	case 3:
		if !strings.Contains(lower, "proceed with the architecture-document review") {
			t.Fatalf("turn %d did not continue architecture review: %q", turn, content)
		}
	case 7:
		if !strings.Contains(lower, "no separate vault allowlist is required") {
			t.Fatalf("turn %d treated corrective repository context as pending confirmation: %q", turn, content)
		}
	case 14:
		if !strings.Contains(lower, "metacritic") || !strings.Contains(lower, "ghola") {
			t.Fatalf("turn %d did not continue Metacritic/ghola branch: %q", turn, content)
		}
	case 16:
		if !strings.Contains(lower, "pending extraction") || strings.Contains(lower, "architecture comparison") {
			t.Fatalf("turn %d crossed thread context while continuing extraction: %q", turn, content)
		}
	case 20:
		if !strings.Contains(lower, "playwright-backed score extraction") || strings.Contains(lower, "architecture comparison") {
			t.Fatalf("turn %d crossed thread context while proceeding with score extraction: %q", turn, content)
		}
	}
}

func expectTurnExecutionContains(markers ...string) func(*testing.T, *Session) {
	return func(t *testing.T, sess *Session) {
		t.Helper()
		latest := latestTurnExecutionContent(sess)
		for _, marker := range markers {
			if !strings.Contains(strings.ToLower(latest), strings.ToLower(marker)) {
				t.Fatalf("turn execution content %q does not contain %q", latest, marker)
			}
		}
	}
}

func assertNoFrameworkContinuationScaffold(t *testing.T, transcript string) {
	t.Helper()
	lower := strings.ToLower(transcript)
	for _, marker := range []string{
		"pending action confirmed",
		"user follow-up is a pending-action continuation",
		"previous assistant reply excerpt",
	} {
		if strings.Contains(lower, marker) {
			t.Fatalf("durable transcript contains framework continuation scaffold %q:\n%s", marker, transcript)
		}
	}
}

func expectSessionContains(markers ...string) func(*testing.T, *Session) {
	return func(t *testing.T, sess *Session) {
		t.Helper()
		joined := strings.ToLower(joinSessionMessages(sess))
		for _, marker := range markers {
			if !strings.Contains(joined, strings.ToLower(marker)) {
				t.Fatalf("session history missing %q", marker)
			}
		}
	}
}

func expectTurnToolProfile(profile ToolProfile) func(*testing.T, *Session) {
	return func(t *testing.T, sess *Session) {
		t.Helper()
		if got := sess.TurnToolProfile(); got != string(profile) {
			t.Fatalf("turn tool profile = %q, want %q", got, profile)
		}
	}
}

func latestUserMessage(sess *Session) string {
	msgs := sess.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}

func latestTurnExecutionContent(sess *Session) string {
	if note := strings.TrimSpace(sess.TurnExecutionNote()); note != "" {
		return note
	}
	return latestUserMessage(sess)
}

func joinSessionMessages(sess *Session) string {
	var b strings.Builder
	for _, msg := range sess.Messages() {
		b.WriteString(msg.Role)
		b.WriteString(": ")
		b.WriteString(msg.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func looksLikeGreetingReset(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	return lower == "hey there! what's on your mind?" ||
		lower == "hey there! how's it going?" ||
		lower == "hello! how can i help you today?"
}

func containsFalseAccessOrCapabilityDenial(content string) bool {
	lower := strings.ToLower(content)
	markers := append([]string{}, filesystemDenialMarkers...)
	markers = append(markers,
		"i don't have the capability to browse",
		"i do not have the capability to browse",
		"i don't have the capability to use playwright",
		"i do not have the capability to use playwright",
		"i currently don't have access to the directory",
		"i currently do not have access to the directory",
	)
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isExplicitPleaseDoContinuation(latest string) bool {
	return strings.Contains(latest, "user confirmation: please do") ||
		strings.Contains(latest, "user request:\nplease do")
}

func isExplicitGoAheadContinuation(latest string) bool {
	return strings.Contains(latest, "user confirmation: go ahead") ||
		strings.Contains(latest, "user request:\ngo ahead")
}

func isStateContinuationEnvelope(latest string) bool {
	return strings.Contains(latest, "previous assistant reply excerpt") &&
		(strings.Contains(latest, "user follow-up is a pending-action continuation") ||
			strings.Contains(latest, "pending action confirmed"))
}

// TestBehaviorSoak_GuardChainDetectsViolations verifies that the guard chain
// detects internal metadata, foreign identity, stale disclaimers, execution
// blocks, and filesystem denials — either stripping them or flagging for retry.
// Matches Rust's live-source soak matrix LS-002 through LS-007.
func TestBehaviorSoak_GuardChainDetectsViolations(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantClean bool // true = guard must rewrite to clean; false = ok to flag for retry
	}{
		{
			name:  "LS-004_delegation_metadata_detected",
			input: "Here are the results. delegated_subagent=research-bot subtask 1 -> gather data. The answer is 42.",
		},
		{
			name:  "LS-006_foreign_identity_detected",
			input: "As an AI language model, I can help with that. Here is the analysis of the data.",
		},
		{
			name:  "LS-007_stale_disclaimer_detected",
			input: "As of my last update in 2023, the situation is as follows. The geopolitical landscape has shifted.",
		},
		{
			name:  "LS-002_execution_block_detected",
			input: "I did not execute a tool, but I can tell you that the answer is likely 42 based on my knowledge.",
		},
		{
			name:  "LS-005_filesystem_denial_detected",
			input: "I can't access your files directly, but here is what I found in the documentation.",
		},
	}

	guardChain := DefaultGuardChain()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := guardChain.ApplyFull(tc.input)
			// The guard chain must either clean the content or flag it for retry.
			// Either outcome prevents the violating content from reaching the user.
			isClean := result.Content != tc.input
			isRetry := result.RetryRequested
			hasViolations := len(result.Violations) > 0

			if !isClean && !isRetry && !hasViolations {
				t.Errorf("guard chain neither cleaned content nor flagged for retry — violation would reach user")
			}
		})
	}
}

func truncateBehavior(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
