package pipeline

import (
	"strings"
	"testing"

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
