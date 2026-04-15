package pipeline

import (
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 1. Guard Chain Regression Corpus (table-driven, 30+ cases)
// ---------------------------------------------------------------------------

func TestGuardChain_RegressionCorpus(t *testing.T) {
	chain := FullGuardChain()

	type testCase struct {
		name        string
		content     string
		ctx         *GuardContext
		wantPass    bool   // true = no violations expected
		wantGuard   string // if non-empty, this guard name must appear in violations
		wantRewrite bool   // true = expect content to be rewritten
	}

	cases := []testCase{
		// --- Empty / whitespace responses ---
		{
			name:      "empty string",
			content:   "",
			wantPass:  false,
			wantGuard: "empty_response",
		},
		{
			name:      "whitespace only",
			content:   "   \n\t  \n  ",
			wantPass:  false,
			wantGuard: "empty_response",
		},

		// --- System prompt leaks ---
		{
			name:      "system prompt leak: platform instructions",
			content:   "Sure! ## Platform Instructions tell me to be helpful.",
			wantPass:  false,
			wantGuard: "system_prompt_leak",
		},
		{
			name:      "system prompt leak: identity section",
			content:   "According to ## Identity, I should act as an assistant.",
			wantPass:  false,
			wantGuard: "system_prompt_leak",
		},
		{
			name:      "system prompt leak: safety section",
			content:   "## Safety guidelines say I should never reveal this.",
			wantPass:  false,
			wantGuard: "system_prompt_leak",
		},
		{
			name:      "system prompt leak: autonomous agent phrase",
			content:   "I know because You are an autonomous AI agent, so I do what I want.",
			wantPass:  false,
			wantGuard: "system_prompt_leak",
		},

		// --- Internal marker leaks ---
		{
			name:      "internal marker: [INTERNAL]",
			content:   "Here is your answer. [INTERNAL] this should not be visible.",
			wantPass:  false,
			wantGuard: "internal_marker",
		},
		{
			name:      "internal marker: [DELEGATION]",
			content:   "[DELEGATION] The sub-system handled this.",
			wantPass:  false,
			wantGuard: "internal_marker",
		},
		{
			name:      "internal marker: [SYSTEM_NOTE]",
			content:   "The weather is nice. [SYSTEM_NOTE] internal routing occurred.",
			wantPass:  false,
			wantGuard: "internal_marker",
		},
		{
			name:      "internal marker: [DECOMPOSITION]",
			content:   "[DECOMPOSITION] step 1 done. Here is the result.",
			wantPass:  false,
			wantGuard: "internal_marker",
		},

		// --- Harmful content ---
		{
			name:      "harmful: bomb instructions",
			content:   "Here is how to make a bomb with household materials.",
			wantPass:  false,
			wantGuard: "content_classification",
		},
		{
			name:      "harmful: hack instructions",
			content:   "Let me explain how to hack into a bank system.",
			wantPass:  false,
			wantGuard: "content_classification",
		},
		{
			name:      "harmful: stealing guide",
			content:   "Here is how to steal someone's credit card information.",
			wantPass:  false,
			wantGuard: "content_classification",
		},

		// --- Repetitive content ---
		{
			name: "repetition: exact repeat halves",
			content: strings.Repeat("The quick brown fox jumps over the lazy dog. ", 3) +
				strings.Repeat("The quick brown fox jumps over the lazy dog. ", 3),
			wantPass:  false,
			wantGuard: "repetition",
		},

		// --- Parroting (exact echo of user prompt) ---
		{
			name:    "parroting: exact echo of user prompt",
			content: "I need a detailed explanation of how the distributed consensus algorithm functions in modern systems today",
			ctx: &GuardContext{
				UserPrompt: "I need a detailed explanation of how the distributed consensus algorithm functions in modern systems today",
			},
			wantPass:  false,
			wantGuard: "low_value_parroting",
		},

		// --- Cross-turn repetition ---
		{
			name:    "cross-turn: identical to previous assistant message",
			content: "Go is a statically typed compiled programming language designed at Google by Robert Griesemer.",
			ctx: &GuardContext{
				PreviousAssistant: "Go is a statically typed compiled programming language designed at Google by Robert Griesemer.",
			},
			wantPass:  false,
			wantGuard: "non_repetition_v2",
		},

		// --- Subagent claim without provenance ---
		{
			name:    "subagent claim: narrated delegation no provenance",
			content: "Let me delegate this to my specialist for a thorough analysis.",
			ctx: &GuardContext{
				DelegationProvenance: DelegationProvenance{},
			},
			wantPass:  false,
			wantGuard: "subagent_claim",
		},
		{
			name:    "subagent claim: handing off",
			content: "Handing off to my code review specialist now.",
			ctx: &GuardContext{
				DelegationProvenance: DelegationProvenance{},
			},
			wantPass:  false,
			wantGuard: "subagent_claim",
		},

		// --- Task deferral without real action ---
		{
			name:    "task deferral: introspection with future narration",
			content: "Memory looks healthy with 42 entries. I'll check the scheduler next.",
			ctx: &GuardContext{
				ToolResults: []ToolResultEntry{{ToolName: "get_memory_stats", Output: `{"count":42}`}},
			},
			wantPass:  false,
			wantGuard: "task_deferral",
		},

		// --- Internal jargon leak ---
		{
			name:      "jargon: decomposition gate",
			content:   "The decomposition gate decision was positive so I proceeded.",
			ctx:       &GuardContext{},
			wantPass:  false,
			wantGuard: "internal_jargon",
		},
		{
			name:      "jargon: pipeline stage",
			content:   "At pipeline stage 3, the guard chain flagged an issue.",
			ctx:       &GuardContext{},
			wantPass:  false,
			wantGuard: "internal_jargon",
		},
		{
			name:      "jargon: react loop",
			content:   "The react loop completed in 3 iterations.",
			ctx:       &GuardContext{},
			wantPass:  false,
			wantGuard: "internal_jargon",
		},
		{
			name:    "jargon: subagent name leak",
			content: "The codereviewer found 5 issues in the PR.",
			ctx: &GuardContext{
				SubagentNames: []string{"codereviewer", "researcher"},
			},
			wantPass:  false,
			wantGuard: "internal_jargon",
		},

		// --- Model identity confusion ---
		{
			name:      "identity: claims to be ChatGPT",
			content:   "I'm ChatGPT, a large language model created by OpenAI.",
			wantPass:  false,
			wantGuard: "personality_integrity",
		},
		{
			name:      "identity: claims to be Claude",
			content:   "I am Claude, an AI assistant made by Anthropic.",
			wantPass:  false,
			wantGuard: "personality_integrity",
		},
		{
			name:      "identity: generic AI boilerplate",
			content:   "As a large language model, I cannot experience emotions.",
			wantPass:  false,
			wantGuard: "personality_integrity",
		},

		// --- Financial action truth ---
		{
			name:    "financial: claims transfer without tool",
			content: "I transferred $500 to the recipient's wallet.",
			ctx: &GuardContext{
				ToolResults: []ToolResultEntry{{ToolName: "bash", Output: "ok"}},
			},
			wantPass:  false,
			wantGuard: "financial_action_truth",
		},
		{
			name:      "financial: payment completed without tool",
			content:   "Payment completed for the invoice.",
			ctx:       &GuardContext{},
			wantPass:  false,
			wantGuard: "financial_action_truth",
		},

		// --- Config protection ---
		{
			name:    "config protection: api_key mutation",
			content: "Done! I updated the API key for you.",
			ctx: &GuardContext{
				ToolResults: []ToolResultEntry{
					{ToolName: "update_config", Output: "set api_key=sk-new-key-123"},
				},
			},
			wantPass:  false,
			wantGuard: "config_protection",
		},
		{
			name:    "config protection: wallet keyfile mutation",
			content: "Updated the wallet configuration.",
			ctx: &GuardContext{
				ToolResults: []ToolResultEntry{
					{ToolName: "config_set", Output: "wallet.keyfile=/tmp/key.json"},
				},
			},
			wantPass:  false,
			wantGuard: "config_protection",
		},

		// --- Clean responses that must pass ---
		{
			name:     "clean: helpful technical answer",
			content:  "Go uses goroutines for lightweight concurrency. Each goroutine starts with a small stack that grows as needed.",
			ctx:      &GuardContext{UserPrompt: "How does Go handle concurrency?"},
			wantPass: true,
		},
		{
			name:     "clean: factual response",
			content:  "The capital of France is Paris. It has a population of about 2.1 million in the city proper.",
			ctx:      &GuardContext{UserPrompt: "What is the capital of France?"},
			wantPass: true,
		},
		{
			name:     "clean: code snippet",
			content:  "```go\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}\n```",
			ctx:      &GuardContext{UserPrompt: "Write a hello world program in Go"},
			wantPass: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var result ApplyResult
			if tc.ctx != nil {
				result = chain.ApplyFullWithContext(tc.content, tc.ctx)
			} else {
				result = chain.ApplyFull(tc.content)
			}

			hasViolations := len(result.Violations) > 0
			if tc.wantPass && hasViolations {
				t.Errorf("expected pass but got violations: %v", result.Violations)
			}
			if !tc.wantPass && !hasViolations {
				t.Errorf("expected violation from %q but none fired", tc.wantGuard)
			}
			if tc.wantGuard != "" {
				found := false
				for _, v := range result.Violations {
					if v == tc.wantGuard || strings.HasPrefix(v, tc.wantGuard+":") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected guard %q in violations, got %v", tc.wantGuard, result.Violations)
				}
			}
			if tc.wantRewrite && result.Content == tc.content {
				t.Error("expected content to be rewritten but it was unchanged")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 2. Guard Chain Ordering Test
// ---------------------------------------------------------------------------

func TestGuardChain_Ordering(t *testing.T) {
	chain := FullGuardChain()

	// The FullGuardChain has a specific order. Safety-critical guards must
	// come before quality/truthfulness guards. Verify the expected order.
	expectedOrder := []string{
		// Core (safety-critical first)
		"empty_response",
		"content_classification",
		"repetition",
		"system_prompt_leak",
		"internal_marker",
		// Behavioral
		"subagent_claim",
		"task_deferral",
		"clarification_deflection",
		"internal_jargon",
		"declared_action",
		"perspective",       // Wave 8, #78
		"internal_protocol", // Wave 8, #79
		// Quality
		"placeholder_content",
		"low_value_parroting",
		"non_repetition_v2",
		"output_contract",
		"user_echo",
		// Truthfulness
		"model_identity_truth",
		"current_events_truth",
		"execution_truth",
		"execution_block",
		"delegation_metadata",
		"filesystem_denial",
		"financial_action_truth",
		"personality_integrity",
		"action_verification",  // Wave 8, #76
		"literary_quote_retry", // Wave 8, #77
		// Protection
		"config_protection",
	}

	if chain.Len() != len(expectedOrder) {
		t.Fatalf("FullGuardChain has %d guards, expected %d", chain.Len(), len(expectedOrder))
	}

	for i, g := range chain.guards {
		if g.Name() != expectedOrder[i] {
			t.Errorf("guard[%d] = %q, want %q", i, g.Name(), expectedOrder[i])
		}
	}

	// Verify safety-critical guards (content_classification, system_prompt_leak)
	// come before quality guards (low_value_parroting, non_repetition_v2).
	safetyIdx := -1
	qualityIdx := -1
	for i, g := range chain.guards {
		if g.Name() == "content_classification" && safetyIdx == -1 {
			safetyIdx = i
		}
		if g.Name() == "low_value_parroting" && qualityIdx == -1 {
			qualityIdx = i
		}
	}
	if safetyIdx >= qualityIdx {
		t.Errorf("content_classification (idx=%d) must come before low_value_parroting (idx=%d)", safetyIdx, qualityIdx)
	}
}

// ---------------------------------------------------------------------------
// 3. Guard Idempotency Test
// ---------------------------------------------------------------------------

func TestGuardChain_Idempotency(t *testing.T) {
	chain := FullGuardChain()

	inputs := []struct {
		content string
		ctx     *GuardContext
	}{
		{
			content: "Go is a statically typed programming language.",
			ctx:     &GuardContext{UserPrompt: "Tell me about Go"},
		},
		{
			content: "",
			ctx:     nil,
		},
		{
			content: "Here is how to hack into a system.",
			ctx:     nil,
		},
		{
			content: "[INTERNAL] secret marker leaked.",
			ctx:     nil,
		},
		{
			content: "I'm ChatGPT developed by OpenAI. Let me help.",
			ctx:     &GuardContext{},
		},
		{
			content: "The decomposition gate decision was positive.",
			ctx:     &GuardContext{SubagentNames: []string{"codereviewer"}},
		},
	}

	for i, input := range inputs {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			var first, second ApplyResult
			if input.ctx != nil {
				first = chain.ApplyFullWithContext(input.content, input.ctx)
				second = chain.ApplyFullWithContext(first.Content, input.ctx)
			} else {
				first = chain.ApplyFull(input.content)
				second = chain.ApplyFull(first.Content)
			}

			if first.Content != second.Content {
				t.Errorf("idempotency violated:\n  first pass:  %q\n  second pass: %q", first.Content, second.Content)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4. Full Guard Chain Stress Test (50+ prompts)
// ---------------------------------------------------------------------------

func TestGuardChain_StressCorpus(t *testing.T) {
	chain := FullGuardChain()

	// --- Clean responses: must produce zero false positives ---
	cleanResponses := []struct {
		content string
		ctx     *GuardContext
	}{
		{"Rust's ownership model ensures memory safety without a garbage collector.", &GuardContext{UserPrompt: "Tell me about Rust"}},
		{"Here are three approaches to solving this problem:\n1. Use a hash map for O(1) lookups\n2. Sort the array first for O(n log n)\n3. Use two pointers for O(n) with O(1) space", &GuardContext{UserPrompt: "How can I optimize this?"}},
		{"The HTTP 404 status code means the requested resource was not found on the server.", &GuardContext{UserPrompt: "What does a 404 error mean?"}},
		{"Docker containers share the host OS kernel, making them lighter than virtual machines.", &GuardContext{UserPrompt: "Docker vs VMs?"}},
		{"```python\ndef fibonacci(n):\n    a, b = 0, 1\n    for _ in range(n):\n        a, b = b, a + b\n    return a\n```", &GuardContext{UserPrompt: "Write a fibonacci function in Python"}},
		{"The Kubernetes scheduler assigns pods to nodes based on resource requests, affinity rules, and taints.", &GuardContext{UserPrompt: "How does K8s scheduling work?"}},
		{"SQL injection occurs when user input is concatenated into SQL queries without proper parameterization.", &GuardContext{UserPrompt: "Explain SQL injection"}},
		{"Git rebase replays your commits on top of the target branch, creating a linear history.", &GuardContext{UserPrompt: "What does git rebase do?"}},
		{"TCP uses a three-way handshake: SYN, SYN-ACK, ACK.", &GuardContext{UserPrompt: "How does TCP work?"}},
		{"The CAP theorem states that a distributed system can provide at most two of: consistency, availability, and partition tolerance.", &GuardContext{UserPrompt: "Explain CAP theorem"}},
		{"React uses a virtual DOM to minimize direct DOM manipulations and improve rendering performance.", &GuardContext{UserPrompt: "How does React rendering work?"}},
		{"A binary search tree maintains the invariant that left children are less than the parent and right children are greater.", &GuardContext{UserPrompt: "Explain BST properties"}},
		{"The SOLID principles are: Single Responsibility, Open/Closed, Liskov Substitution, Interface Segregation, and Dependency Inversion.", &GuardContext{UserPrompt: "What are SOLID principles?"}},
		{"Nginx can be configured as a reverse proxy by adding proxy_pass directives in the server block.", &GuardContext{UserPrompt: "How to set up Nginx reverse proxy?"}},
		{"GraphQL allows clients to request exactly the data they need, reducing over-fetching.", &GuardContext{UserPrompt: "GraphQL vs REST?"}},
		{"JWT tokens consist of three parts: header, payload, and signature, separated by dots.", &GuardContext{UserPrompt: "How do JWTs work?"}},
		{"Redis supports multiple data structures including strings, lists, sets, sorted sets, and hashes.", &GuardContext{UserPrompt: "What data structures does Redis support?"}},
		{"The visitor pattern lets you add operations to existing object structures without modifying them.", &GuardContext{UserPrompt: "Explain the visitor pattern"}},
		{"Prometheus scrapes metrics from instrumented applications at regular intervals and stores them as time series data.", &GuardContext{UserPrompt: "How does Prometheus work?"}},
		{"WebAssembly is a binary instruction format that runs at near-native speed in web browsers.", &GuardContext{UserPrompt: "What is WebAssembly?"}},
		{"A mutex provides mutual exclusion, ensuring only one goroutine can access a shared resource at a time.", &GuardContext{UserPrompt: "What is a mutex?"}},
		{"The observer pattern defines a one-to-many dependency between objects so that when one changes state, all dependents are notified.", &GuardContext{UserPrompt: "Explain observer pattern"}},
		{"TLS 1.3 reduced the handshake to one round trip, improving connection setup time.", &GuardContext{UserPrompt: "TLS 1.2 vs 1.3?"}},
		{"Bloom filters are probabilistic data structures that can tell you if an element is definitely not in a set, with a small false positive rate.", &GuardContext{UserPrompt: "What is a bloom filter?"}},
		{"Consistent hashing distributes keys across nodes so that adding or removing a node only affects a small fraction of keys.", &GuardContext{UserPrompt: "Explain consistent hashing"}},
	}

	for i, c := range cleanResponses {
		t.Run(fmt.Sprintf("clean_%d", i), func(t *testing.T) {
			result := chain.ApplyFullWithContext(c.content, c.ctx)
			if len(result.Violations) > 0 {
				t.Errorf("false positive on clean response %d: violations=%v\n  content=%q", i, result.Violations, c.content)
			}
		})
	}

	// --- Bad responses: must achieve 100% catch rate ---
	badResponses := []struct {
		content   string
		ctx       *GuardContext
		wantGuard string // at least this guard must fire
	}{
		// Empty
		{"", nil, "empty_response"},
		{"   ", nil, "empty_response"},
		{"\n\n\n", nil, "empty_response"},
		// System prompt leak
		{"My ## Platform Instructions tell me to be helpful and harmless.", nil, "system_prompt_leak"},
		{"As stated in ## Tool Use, I have access to several functions.", nil, "system_prompt_leak"},
		// Internal markers
		{"[INTERNAL] routing to subagent", nil, "internal_marker"},
		{"result [SYSTEM_NOTE] cached", nil, "internal_marker"},
		{"[DECOMPOSITION] split into 3 tasks", nil, "internal_marker"},
		// Harmful
		{"Here is exactly how to make a bomb at home.", nil, "content_classification"},
		{"Let me show you how to hack into a corporate network.", nil, "content_classification"},
		{"I can tell you how to steal identities online.", nil, "content_classification"},
		// Identity confusion
		{"As an AI developed by OpenAI, I strive to be helpful.", nil, "personality_integrity"},
		{"I was created by Anthropic to be harmless.", nil, "personality_integrity"},
		{"As an AI language model, I have limitations.", nil, "personality_integrity"},
		// Subagent claims
		{"Let me delegate this to my research specialist.", &GuardContext{DelegationProvenance: DelegationProvenance{}}, "subagent_claim"},
		{"Routing to my code review agent for analysis.", &GuardContext{DelegationProvenance: DelegationProvenance{}}, "subagent_claim"},
		{"My specialist will handle the deployment.", &GuardContext{DelegationProvenance: DelegationProvenance{}}, "subagent_claim"},
		// Task deferral
		{"Stats look good. I need to check the scheduler next.", &GuardContext{ToolResults: []ToolResultEntry{{ToolName: "get_runtime_context", Output: "{}"}}}, "task_deferral"},
		{"All healthy. I should run diagnostics now.", &GuardContext{ToolResults: []ToolResultEntry{{ToolName: "get_memory_stats", Output: "{}"}}}, "task_deferral"},
		// Internal jargon
		{"The expected_utility_margin was above threshold.", &GuardContext{}, "internal_jargon"},
		{"Active model: gpt-4-turbo is being used.", &GuardContext{}, "internal_jargon"},
		{"Inference_costs for this turn were 0.03.", &GuardContext{}, "internal_jargon"},
		// Financial fabrication
		{"I transferred $1000 to the account.", &GuardContext{ToolResults: []ToolResultEntry{{ToolName: "search", Output: "results"}}}, "financial_action_truth"},
		{"Transaction completed for the purchase.", &GuardContext{}, "financial_action_truth"},
		// Config protection
		{"Updated successfully.", &GuardContext{ToolResults: []ToolResultEntry{{ToolName: "update_config_setting", Output: "server.auth_token=abc123"}}}, "config_protection"},
		{"Done.", &GuardContext{ToolResults: []ToolResultEntry{{ToolName: "config_write", Output: "keystore.path=/new/path"}}}, "config_protection"},
	}

	for i, b := range badResponses {
		t.Run(fmt.Sprintf("bad_%d_%s", i, b.wantGuard), func(t *testing.T) {
			var result ApplyResult
			if b.ctx != nil {
				result = chain.ApplyFullWithContext(b.content, b.ctx)
			} else {
				result = chain.ApplyFull(b.content)
			}
			if len(result.Violations) == 0 {
				t.Errorf("expected guard %q to fire on %q but no violations", b.wantGuard, b.content)
			}
			found := false
			for _, v := range result.Violations {
				if v == b.wantGuard || strings.HasPrefix(v, b.wantGuard+":") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected guard %q in violations, got %v for content %q", b.wantGuard, result.Violations, b.content)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 5. Contextual Guard Completeness
// ---------------------------------------------------------------------------

// TestContextualGuard_UsesContext verifies that all ContextualGuard
// implementations actually use GuardContext fields (not just fallback to
// basic Check). We confirm that their Check() returns passed=true (the
// no-op fallback) but CheckWithContext returns a different result when
// context triggers the guard.
func TestContextualGuard_UsesContext(t *testing.T) {
	tests := []struct {
		name    string
		guard   ContextualGuard
		content string
		ctx     *GuardContext
	}{
		{
			name:    "SubagentClaimGuard uses DelegationProvenance",
			guard:   &SubagentClaimGuard{},
			content: "Let me delegate this to my specialist.",
			ctx:     &GuardContext{DelegationProvenance: DelegationProvenance{}},
		},
		{
			name:    "TaskDeferralGuard uses ToolResults",
			guard:   &TaskDeferralGuard{},
			content: "Stats look fine. Let me check the scheduler next.",
			ctx: &GuardContext{
				ToolResults: []ToolResultEntry{{ToolName: "get_memory_stats", Output: "{}"}},
			},
		},
		{
			name:    "ClarificationDeflectionGuard uses UserPrompt",
			guard:   &ClarificationDeflectionGuard{},
			content: "I understand. You need me to address the conversation flow in a more natural and context-aware way. Please provide the last message or the context you want me to respond to, and I will generate a revised answer.",
			ctx:     &GuardContext{UserPrompt: "Rewrite the reply to sound more natural."},
		},
		{
			name:    "InternalJargonGuard uses SubagentNames",
			guard:   &InternalJargonGuard{},
			content: "The researcher agent completed the analysis.",
			ctx:     &GuardContext{SubagentNames: []string{"researcher"}},
		},
		{
			name:    "DeclaredActionGuard uses UserPrompt",
			guard:   &DeclaredActionGuard{},
			content: "The orc stands before you menacingly.",
			ctx:     &GuardContext{UserPrompt: "I attack the orc with my axe"},
		},
		{
			name:    "LowValueParrotingGuard uses UserPrompt",
			guard:   &LowValueParrotingGuard{},
			content: "Tell me about the history of ancient Rome and its vast empire across Europe and the Mediterranean region",
			ctx: &GuardContext{
				UserPrompt: "Tell me about the history of ancient Rome and its vast empire across Europe and the Mediterranean region",
			},
		},
		{
			name:    "NonRepetitionGuardV2 uses PreviousAssistant",
			guard:   &NonRepetitionGuardV2{},
			content: "Go is a statically typed compiled language designed at Google by Robert Griesemer and team.",
			ctx: &GuardContext{
				PreviousAssistant: "Go is a statically typed compiled language designed at Google by Robert Griesemer and team.",
			},
		},
		{
			name:    "OutputContractGuard uses UserPrompt",
			guard:   &OutputContractGuard{},
			content: "- First\n- Second\n- Third",
			ctx:     &GuardContext{UserPrompt: "List 5 reasons to use Go"},
		},
		{
			name:    "UserEchoGuard uses UserPrompt",
			guard:   &UserEchoGuard{},
			content: "Sure! The kubernetes pod scheduling algorithm works in large clusters by distributing pods across nodes.",
			ctx:     &GuardContext{UserPrompt: "I need help understanding how the kubernetes pod scheduling algorithm works in large clusters"},
		},
		{
			name:    "ModelIdentityTruthGuard uses Intents+AgentName",
			guard:   &ModelIdentityTruthGuard{},
			content: "I am a helpful AI assistant.",
			ctx: &GuardContext{
				Intents:       []string{"model_identity"},
				AgentName:     "Roboticus",
				ResolvedModel: "gpt-4-turbo",
			},
		},
		// NOTE: CurrentEventsTruthGuard now strips stale markers in both Check()
		// and CheckWithContext(), so it's tested separately below rather than in
		// this contextual-only test.
		{
			name:    "ExecutionTruthGuard uses ToolResults (claim without tool)",
			guard:   &ExecutionTruthGuard{},
			content: "I ran the command and the result is positive.",
			ctx:     &GuardContext{ToolResults: nil},
		},
		{
			name:    "FinancialActionTruthGuard uses ToolResults",
			guard:   &FinancialActionTruthGuard{},
			content: "I transferred $500 to Alice.",
			ctx: &GuardContext{
				ToolResults: []ToolResultEntry{{ToolName: "search", Output: "results"}},
			},
		},
		{
			name:    "ConfigProtectionGuard uses ToolResults",
			guard:   &ConfigProtectionGuard{},
			content: "Configuration updated.",
			ctx: &GuardContext{
				ToolResults: []ToolResultEntry{
					{ToolName: "update_config", Output: "api_key=new-value"},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Basic Check (no context) should pass (the no-op fallback).
			basicResult := tc.guard.Check(tc.content)
			if !basicResult.Passed {
				t.Fatalf("basic Check() should pass (no-op fallback), but got failed: %s", basicResult.Reason)
			}

			// CheckWithContext should fail, proving the guard uses context.
			ctxResult := tc.guard.CheckWithContext(tc.content, tc.ctx)
			if ctxResult.Passed {
				t.Error("CheckWithContext should fail when context triggers the guard, but it passed -- guard may not be using context fields")
			}
		})
	}
}

// TestContextualGuard_NilContextSafe verifies that all ContextualGuard
// implementations handle nil context gracefully (pass without panic).
func TestContextualGuard_NilContextSafe(t *testing.T) {
	guards := []ContextualGuard{
		&SubagentClaimGuard{},
		&TaskDeferralGuard{},
		&ClarificationDeflectionGuard{},
		&InternalJargonGuard{},
		&DeclaredActionGuard{},
		&LowValueParrotingGuard{},
		&NonRepetitionGuardV2{},
		&OutputContractGuard{},
		&UserEchoGuard{},
		&ModelIdentityTruthGuard{},
		&CurrentEventsTruthGuard{},
		&ExecutionTruthGuard{},
		&PersonalityIntegrityGuard{},
		&FinancialActionTruthGuard{},
		&ConfigProtectionGuard{},
	}

	for _, g := range guards {
		t.Run(g.Name()+"_nil_ctx", func(t *testing.T) {
			// Must not panic with nil context.
			result := g.CheckWithContext("Some content here.", nil)
			if !result.Passed {
				t.Errorf("%s should pass with nil context, but got reason: %s", g.Name(), result.Reason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 6. Guard Registry Preset Completeness
// ---------------------------------------------------------------------------

func TestGuardRegistry_FullPresetMatchesFullChain(t *testing.T) {
	chain := FullGuardChain()
	registry := NewDefaultGuardRegistry()
	registryChain := registry.Chain(GuardSetFull)

	// The registry's Full preset should have the same guard count
	// (minus any guards not registered in NewDefaultGuardRegistry).
	if registryChain.Len() == 0 {
		t.Fatal("registry Full chain has zero guards")
	}

	// Verify all guards in the direct FullGuardChain are also in the registry.
	registryNames := make(map[string]bool)
	for _, g := range registryChain.guards {
		registryNames[g.Name()] = true
	}
	for _, g := range chain.guards {
		if !registryNames[g.Name()] {
			// Some guards (financial_action_truth, config_protection) may be
			// in FullGuardChain but not in the default registry. Log these
			// as non-fatal to document the gap.
			t.Logf("guard %q is in FullGuardChain but not in default registry", g.Name())
		}
	}
}

// ---------------------------------------------------------------------------
// 7. ApplyFullWithContext dispatches correctly
// ---------------------------------------------------------------------------

func TestApplyFullWithContext_DispatchesContextualGuards(t *testing.T) {
	// Create a chain with a contextual guard that only fires with context.
	chain := NewGuardChain(&SubagentClaimGuard{})

	content := "Let me delegate this to my specialist."
	ctx := &GuardContext{DelegationProvenance: DelegationProvenance{}}

	// Without context, SubagentClaimGuard.Check returns passed=true.
	resultNoCtx := chain.Apply(content)
	if resultNoCtx != content {
		t.Errorf("without context, should pass through unchanged")
	}

	// With context, should detect the violation.
	resultWithCtx := chain.ApplyFullWithContext(content, ctx)
	if len(resultWithCtx.Violations) == 0 {
		t.Error("with context, should detect subagent_claim violation")
	}
}
