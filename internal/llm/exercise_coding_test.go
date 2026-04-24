package llm

import "testing"

// TestExerciseMatrix_HasCodingPrompts pins the v1.0.6 CODING intent
// class: five prompts, one per complexity level. If a future edit
// accidentally drops one, this test flags it before baseline runs
// start producing unevenly-sampled coding data.
func TestExerciseMatrix_HasCodingPrompts(t *testing.T) {
	count := 0
	seenComplexity := make(map[ComplexityLevel]bool)
	for _, p := range ExerciseMatrix {
		if p.Intent == IntentCoding {
			count++
			seenComplexity[p.Complexity] = true
		}
	}
	if count != 5 {
		t.Errorf("ExerciseMatrix has %d CODING prompts, want 5 (one per complexity)", count)
	}
	for _, lvl := range []ComplexityLevel{ComplexityTrivial, ComplexitySimple, ComplexityModerate, ComplexityComplex, ComplexityExpert} {
		if !seenComplexity[lvl] {
			t.Errorf("CODING intent class is missing a prompt for complexity=%s", lvl.String())
		}
	}
}

// TestScoreCoding_RewardsCodeSubstance pins the scoring contract for
// the CODING intent: responses with actual code structure + coding
// concepts should score meaningfully higher than generic prose that
// mentions programming but doesn't engage with code.
func TestScoreCoding_RewardsCodeSubstance(t *testing.T) {
	// Complex prompt (LRU cache review) — matches ComplexityComplex
	// length thresholds so we're comparing on quality-markers, not
	// length-alone adequacy.
	prompt := ExercisePrompt{Intent: IntentCoding, Complexity: ComplexityComplex}

	// Generic prose — mentions coding concepts casually but shows
	// no engagement with actual code.
	prose := "Programming is about writing code that works correctly. A good function should do one thing and do it well. Think about the problem before you start typing. Tests are important for catching bugs. Documentation helps future maintainers understand your intent. Code review catches issues early."

	// Substantive code review — identifies the actual race condition,
	// proposes a fix using concurrency primitives, and surfaces the
	// specific invariant.
	substantive := "The hazard is a race condition: the map read and the counter increment are not atomic, and if multiple goroutines call Get concurrently the data map can race with any concurrent write. Fix with a sync.RWMutex: take RLock for map reads, Lock for the counter increment, OR switch to atomic.AddUint64 for the counter and sync.Map for data. The nil check on c.data must also happen under the lock to avoid panics. Expected complexity stays O(1) on Get."

	proseScore := ScoreExerciseResponse(prompt, prose)
	substantiveScore := ScoreExerciseResponse(prompt, substantive)

	if substantiveScore <= proseScore {
		t.Fatalf("substantive code review (%.2f) should score higher than generic prose (%.2f)", substantiveScore, proseScore)
	}
	if substantiveScore < 0.55 {
		t.Fatalf("substantive code review scored %.2f; expected >= 0.55 on a ComplexityComplex prompt", substantiveScore)
	}
}

// TestScoreCoding_PenalizesShortTrivia confirms that a trivially-short
// answer to a coding question gets a meaningfully LOWER score than a
// substantive answer that engages with the same prompt. Absolute
// thresholds are brittle against scoring-heuristic tuning; a relative
// inequality pins the semantic intent without constraining the
// numeric scale.
func TestScoreCoding_PenalizesShortTrivia(t *testing.T) {
	prompt := ExercisePrompt{Intent: IntentCoding, Complexity: ComplexityModerate}
	short := "nil pointer."
	full := "The function dereferences a nil pointer when x is nil, which panics at runtime. Fix by adding a nil check: if x == nil { return } before *x++, or change the signature to accept a value instead of a pointer and return the incremented value. The value form avoids the hazard entirely at the cost of the mutation-in-place behavior. Either fix should include a test case for the nil input to catch regression."

	shortScore := ScoreExerciseResponse(prompt, short)
	fullScore := ScoreExerciseResponse(prompt, full)

	if shortScore >= fullScore {
		t.Fatalf("short trivia (%.2f) must score lower than a substantive coding answer (%.2f) for the same prompt", shortScore, fullScore)
	}
	// Extra guard: the gap must be meaningful (>= 0.15), not marginal.
	// A marginal gap means the scoring can't actually distinguish
	// quality from trivia — which defeats the point of per-intent
	// baselining.
	if fullScore-shortScore < 0.15 {
		t.Fatalf("score gap between short(%.2f) and full(%.2f) is only %.2f; need >= 0.15 to meaningfully differentiate coding quality", shortScore, fullScore, fullScore-shortScore)
	}
}

func TestScoreCoding_ArtifactBearingPromptPrefersRunnableArtifact(t *testing.T) {
	prompt := ResolveExercisePrompt(
		"Write a function in any language that reverses a string in-place and explain one edge case to watch for.",
		IntentCoding,
		ComplexitySimple,
	)

	proseOnly := "Use a loop to swap characters from both ends toward the center. Watch out for Unicode."
	goArtifact := "```go\nfunc reverseString(s string) string {\n\trunes := []rune(s)\n\tfor i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {\n\t\trunes[i], runes[j] = runes[j], runes[i]\n\t}\n\treturn string(runes)\n}\n```\nEdge case: Unicode combining characters."

	proseScore := ScoreExerciseResponse(prompt, proseOnly)
	artifactScore := ScoreExerciseResponse(prompt, goArtifact)
	if artifactScore <= proseScore {
		t.Fatalf("artifact-bearing answer (%.2f) should outscore prose-only answer (%.2f)", artifactScore, proseScore)
	}
	if artifactScore < 0.7 {
		t.Fatalf("artifact-bearing answer scored %.2f; want >= 0.70", artifactScore)
	}
}

func TestScoreCoding_ArtifactBearingPromptPenalizesBrokenGoSyntax(t *testing.T) {
	prompt := ResolveExercisePrompt(
		"Design a memory-bounded LRU cache in Go with O(1) Get and Put, eviction callbacks, and safe concurrent access. Describe the data structures you'd use and write the core Put method.",
		IntentCoding,
		ComplexityExpert,
	)

	broken := "```go\nfunc (c *Cache) Put(key string, value string) {\n\tif c.items[key] == nil {\n\t\tc.items[key] = value\n```\nUse a map and linked list."
	valid := "```go\nfunc (c *Cache) Put(key string, value string) {\n\tif elem, ok := c.items[key]; ok {\n\t\tent := elem.Value.(*entry)\n\t\tent.value = value\n\t\tc.order.MoveToFront(elem)\n\t\treturn\n\t}\n\tent := &entry{key: key, value: value}\n\telem := c.order.PushFront(ent)\n\tc.items[key] = elem\n}\n```\nUse a map plus doubly linked list."

	brokenScore := ScoreExerciseResponse(prompt, broken)
	validScore := ScoreExerciseResponse(prompt, valid)
	if validScore <= brokenScore {
		t.Fatalf("valid Go artifact (%.2f) should outscore broken artifact (%.2f)", validScore, brokenScore)
	}
}
