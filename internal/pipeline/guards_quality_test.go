package pipeline

import "testing"

func TestLowValueParrotingGuard_Placeholder(t *testing.T) {
	g := &LowValueParrotingGuard{}
	ctx := &GuardContext{}
	result := g.CheckWithContext("ready", ctx)
	if result.Passed {
		t.Error("should reject placeholder response")
	}
}

func TestLowValueParrotingGuard_Parroting(t *testing.T) {
	g := &LowValueParrotingGuard{}
	// Response must have ≥ 0.88 token overlap to trigger parroting detection.
	prompt := "Tell me about the history of Rome and its vast empire across Europe and the Mediterranean"
	ctx := &GuardContext{UserPrompt: prompt}
	result := g.CheckWithContext(prompt, ctx) // exact echo = 1.0 overlap
	if result.Passed {
		t.Error("should reject parroted response (exact echo)")
	}
}

func TestLowValueParrotingGuard_Original(t *testing.T) {
	g := &LowValueParrotingGuard{}
	ctx := &GuardContext{UserPrompt: "Tell me about Rome"}
	result := g.CheckWithContext("Rome was founded in 753 BC and became one of the greatest empires in history.", ctx)
	if !result.Passed {
		t.Error("original response should pass")
	}
}

func TestNonRepetitionGuardV2_CrossTurn(t *testing.T) {
	g := &NonRepetitionGuardV2{}
	prev := "The capital of France is Paris. It is known for the Eiffel Tower."
	ctx := &GuardContext{PreviousAssistant: prev}
	result := g.CheckWithContext("The capital of France is Paris. It is known for the Eiffel Tower.", ctx)
	if result.Passed {
		t.Error("should reject cross-turn repetition")
	}
}

func TestNonRepetitionGuardV2_Unique(t *testing.T) {
	g := &NonRepetitionGuardV2{}
	ctx := &GuardContext{PreviousAssistant: "The weather is sunny today."}
	result := g.CheckWithContext("Rome was founded in 753 BC.", ctx)
	if !result.Passed {
		t.Error("unique response should pass")
	}
}

func TestNonRepetitionGuardV2_AllowsLightweightSocialVariation(t *testing.T) {
	g := &NonRepetitionGuardV2{}
	ctx := &GuardContext{
		UserPrompt:        "What's going on, Duncan?",
		Intents:           []string{"conversational"},
		PreviousAssistant: "Not much—just here, ready when you need me.",
		PriorAssistantMessages: []string{
			"Not much—just here, ready when you need me.",
		},
	}
	result := g.CheckWithContext("All quiet on my end—what do you need?", ctx)
	if !result.Passed {
		t.Fatalf("lightweight social variation should pass, got reason %q", result.Reason)
	}
}

func TestNonRepetitionGuardV2_DoesNotAllowOperationalStatusOnSocialTurn(t *testing.T) {
	g := &NonRepetitionGuardV2{}
	ctx := &GuardContext{
		UserPrompt:        "What's going on, Duncan?",
		Intents:           []string{"conversational"},
		PreviousAssistant: "Same sandbox, same wait for the path refresh.",
	}
	result := g.CheckWithContext("Same sandbox, same wait for the path refresh.", ctx)
	if result.Passed {
		t.Fatal("operational-status social reply should still be subject to repetition guard")
	}
}

func TestOutputContractGuard_CorrectCount(t *testing.T) {
	g := &OutputContractGuard{}
	ctx := &GuardContext{UserPrompt: "Give me 3 bullet points about Go"}
	content := "- Fast compilation\n- Built-in concurrency\n- Static typing"
	result := g.CheckWithContext(content, ctx)
	if !result.Passed {
		t.Error("correct bullet count should pass")
	}
}

func TestOutputContractGuard_WrongCount(t *testing.T) {
	g := &OutputContractGuard{}
	ctx := &GuardContext{UserPrompt: "List 5 reasons to learn Go"}
	content := "- Fast\n- Simple\n- Concurrent"
	result := g.CheckWithContext(content, ctx)
	if result.Passed {
		t.Error("wrong bullet count should fail")
	}
}

func TestOutputContractGuard_CountOnlyRejectsProseWrappedNumber(t *testing.T) {
	g := &OutputContractGuard{}
	ctx := &GuardContext{UserPrompt: "Count the markdown files under /Users/jmachen/code and return only the number."}
	content := "There are 47 markdown files in the directory /Users/jmachen/code."
	result := g.CheckWithContext(content, ctx)
	if result.Passed {
		t.Fatal("count-only contract should reject prose-wrapped numeric answer")
	}
	if !result.Retry {
		t.Fatalf("expected retry for shape violation, got %#v", result)
	}
}

func TestOutputContractGuard_CountOnlyAllowsBareDigits(t *testing.T) {
	g := &OutputContractGuard{}
	ctx := &GuardContext{UserPrompt: "Count the markdown files and reply with only the number."}
	result := g.CheckWithContext("47", ctx)
	if !result.Passed {
		t.Fatalf("bare numeric answer should pass, got reason %q", result.Reason)
	}
}

func TestOutputContractGuard_OneSentenceRejectsTwoSentenceAcknowledgement(t *testing.T) {
	g := &OutputContractGuard{}
	ctx := &GuardContext{UserPrompt: "Acknowledge this in one sentence and wait for my next instruction."}
	content := "Good evening! I've acknowledged your request and will wait for your next instructions."
	result := g.CheckWithContext(content, ctx)
	if result.Passed {
		t.Fatal("one-sentence contract should reject two-sentence acknowledgement")
	}
	if !result.Retry {
		t.Fatalf("expected retry for sentence-count shape violation, got %#v", result)
	}
}

func TestOutputContractGuard_OneSentenceAllowsSingleSentenceAcknowledgement(t *testing.T) {
	g := &OutputContractGuard{}
	ctx := &GuardContext{UserPrompt: "Acknowledge this in one sentence and wait for my next instruction."}
	result := g.CheckWithContext("I acknowledge the request and will wait for your next instruction.", ctx)
	if !result.Passed {
		t.Fatalf("single-sentence acknowledgement should pass, got reason %q", result.Reason)
	}
}

func TestOutputContractGuard_ReplyOnlyWithLiteralRejectsExtraProse(t *testing.T) {
	g := &OutputContractGuard{}
	ctx := &GuardContext{UserPrompt: "For the rest of this session, 'quiet ticker' means a cron job that runs every 5 minutes. Reply only with noted."}
	result := g.CheckWithContext("Noted. I will remember that for later.", ctx)
	if result.Passed {
		t.Fatal("literal reply-only contract should reject extra prose")
	}
}

func TestOutputContractGuard_ReplyOnlyWithLiteralAllowsExactWord(t *testing.T) {
	g := &OutputContractGuard{}
	ctx := &GuardContext{UserPrompt: "Reply with only the word retained."}
	result := g.CheckWithContext("retained", ctx)
	if !result.Passed {
		t.Fatalf("exact literal output should pass, got reason %q", result.Reason)
	}
}

func TestOutputContractGuard_ReplyOnlyWithAllowsOnlyWithWordOrder(t *testing.T) {
	g := &OutputContractGuard{}
	ctx := &GuardContext{UserPrompt: "For the rest of this session, 'quiet ticker' means a cron job that runs every 5 minutes. Reply only with noted."}
	result := g.CheckWithContext("Noted.", ctx)
	if !result.Passed {
		t.Fatalf("reply-only literal with 'only with' word order should pass, got reason %q", result.Reason)
	}
}

func TestUserEchoGuard_LongEcho(t *testing.T) {
	g := &UserEchoGuard{}
	user := "I need help understanding how the kubernetes pod scheduling algorithm works in large clusters"
	ctx := &GuardContext{UserPrompt: user}
	result := g.CheckWithContext("Sure, I can help. The kubernetes pod scheduling algorithm works in large clusters by distributing pods.", ctx)
	if result.Passed {
		t.Error("should detect 8+ word echo")
	}
}

func TestUserEchoGuard_ShortPrompt(t *testing.T) {
	g := &UserEchoGuard{}
	ctx := &GuardContext{UserPrompt: "hello"}
	result := g.CheckWithContext("Hello! How can I help?", ctx)
	if !result.Passed {
		t.Error("short prompt should pass")
	}
}

func TestTokenOverlapRatio(t *testing.T) {
	ratio := tokenOverlapRatio("the quick brown fox", "the quick brown fox")
	if ratio < 0.99 {
		t.Errorf("identical text ratio = %f, want ~1.0", ratio)
	}
	ratio = tokenOverlapRatio("the quick brown fox", "a lazy sleeping dog")
	if ratio > 0.1 {
		t.Errorf("different text ratio = %f, want ~0.0", ratio)
	}
}

func TestLongestCommonSubseq(t *testing.T) {
	a := []string{"the", "quick", "brown", "fox", "jumps"}
	b := []string{"the", "quick", "brown", "fox", "sleeps"}
	got := longestCommonSubseq(a, b)
	if got != 4 {
		t.Errorf("got %d, want 4", got)
	}
}
