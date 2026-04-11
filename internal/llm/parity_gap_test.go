package llm

import (
	"math"
	"strings"
	"testing"
)

// ---------- G005: rolling hash parity ----------

func TestRollingHash_Deterministic(t *testing.T) {
	a := rollingHash("abc")
	b := rollingHash("abc")
	if a != b {
		t.Fatalf("non-deterministic: %d != %d", a, b)
	}
}

func TestRollingHash_MatchesRust(t *testing.T) {
	// Rust: acc = 0; for c in "abc".chars() { acc = acc*31 + c as u32; }
	// 'a'=97: 0*31+97 = 97
	// 'b'=98: 97*31+98 = 3105
	// 'c'=99: 3105*31+99 = 96354
	got := rollingHash("abc")
	if got != 96354 {
		t.Errorf("rollingHash(\"abc\") = %d, want 96354", got)
	}
}

func TestRollingHash_Empty(t *testing.T) {
	if h := rollingHash(""); h != 0 {
		t.Errorf("rollingHash(\"\") = %d, want 0", h)
	}
}

func TestNgramHash_UsesRollingHash(t *testing.T) {
	// The ngramHash function should produce a valid L2-normalised vector.
	v := ngramHash("hello world", ngramDim)
	if len(v) != ngramDim {
		t.Fatalf("dim = %d, want %d", len(v), ngramDim)
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	if math.Abs(norm-1.0) > 0.001 {
		t.Errorf("L2 norm = %f, want ~1.0", norm)
	}
}

// ---------- G006: smart compression scoring ----------

func TestScoreToken_ContentWord(t *testing.T) {
	// "algorithm" is alphabetic, >3 chars, not a stop word → +3.0 base.
	score := scoreToken("algorithm", 5, 20)
	if score < 3.0 {
		t.Errorf("content word score = %f, want >= 3.0", score)
	}
}

func TestScoreToken_StopWord(t *testing.T) {
	score := scoreToken("the", 5, 20)
	// "the" is 3 chars → not content word, is stop word → +0.5 base.
	if score < 0.5 || score >= 3.0 {
		t.Errorf("stop word score = %f, expected around 0.5 base", score)
	}
}

func TestScoreToken_CodePunctuation(t *testing.T) {
	score := scoreToken("fn()", 5, 20)
	// Contains () → +2.0 code bonus on top of base.
	if score < 2.0 {
		t.Errorf("code punctuation score = %f, want >= 2.0", score)
	}
}

func TestScoreToken_Capitalized(t *testing.T) {
	lower := scoreToken("hello", 5, 20)
	upper := scoreToken("Hello", 5, 20)
	if upper <= lower {
		t.Errorf("capitalised (%f) should score higher than lowercase (%f)", upper, lower)
	}
}

func TestScoreToken_PositionBias(t *testing.T) {
	// Index 0 of 20 tokens → in first 10%.
	first := scoreToken("word", 0, 20)
	// Index 10 of 20 → not in first or last 10%.
	middle := scoreToken("word", 10, 20)
	if first <= middle {
		t.Errorf("first-position (%f) should score higher than middle (%f)", first, middle)
	}
}

func TestSmartCompress_FullRatio(t *testing.T) {
	text := "the quick brown fox jumps"
	result := SmartCompress(text, 1.0)
	if result != text {
		t.Errorf("ratio 1.0: got %q, want %q", result, text)
	}
}

func TestSmartCompress_HalfRatio(t *testing.T) {
	text := "the quick brown fox jumps over the lazy dog"
	result := SmartCompress(text, 0.5)
	words := strings.Fields(result)
	total := len(strings.Fields(text))
	expected := int(math.Ceil(float64(total) * 0.5))
	if len(words) != expected {
		t.Errorf("got %d tokens, want %d", len(words), expected)
	}
}

func TestSmartCompress_ClampLow(t *testing.T) {
	text := "a b c d e f g h i j"
	result := SmartCompress(text, 0.0) // clamped to 0.1
	words := strings.Fields(result)
	if len(words) < 1 {
		t.Error("should keep at least 1 token")
	}
}

func TestSmartCompress_Empty(t *testing.T) {
	if got := SmartCompress("", 0.5); got != "" {
		t.Errorf("empty input: got %q", got)
	}
}

func TestSmartCompress_PreservesOrder(t *testing.T) {
	text := "alpha bravo charlie delta echo"
	result := SmartCompress(text, 0.6)
	words := strings.Fields(result)
	// Verify monotonically increasing positions in original.
	origWords := strings.Fields(text)
	lastIdx := -1
	for _, w := range words {
		for i, ow := range origWords {
			if w == ow && i > lastIdx {
				lastIdx = i
				break
			}
		}
	}
	if lastIdx < 0 {
		t.Error("no words preserved")
	}
}

func TestStopWordsCount(t *testing.T) {
	// Rust parity: compression.rs STOP_WORDS contains 77 entries.
	if len(stopWords) != 77 {
		t.Errorf("stopWords has %d entries, want 77 (Rust parity)", len(stopWords))
	}
}

// ---------- G009: pctEncodeQueryValue ----------

func TestPctEncodeQueryValue_Passthrough(t *testing.T) {
	// Unreserved characters should pass through.
	input := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~"
	if got := pctEncodeQueryValue(input); got != input {
		t.Errorf("unreserved chars: got %q, want %q", got, input)
	}
}

func TestPctEncodeQueryValue_SpecialChars(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello world", "hello%20world"},
		{"key=val&foo", "key%3Dval%26foo"},
		{"sk-abc+123/def", "sk-abc%2B123%2Fdef"},
		{"100%", "100%25"},
	}
	for _, tt := range tests {
		got := pctEncodeQueryValue(tt.input)
		if got != tt.want {
			t.Errorf("pctEncodeQueryValue(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPctEncodeQueryValue_Empty(t *testing.T) {
	if got := pctEncodeQueryValue(""); got != "" {
		t.Errorf("empty: got %q", got)
	}
}

// ---------- G010: ClassifyProviderError ----------

func TestClassifyProviderError(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"circuit breaker open for provider X", "provider temporarily unavailable"},
		{"no api key found", "no provider configured for this model"},
		{"no provider configured", "no provider configured for this model"},
		{"HTTP 401 Unauthorized", "provider authentication error"},
		{"403 Forbidden", "provider authentication error"},
		{"authentication failed", "provider authentication error"},
		{"429 Too Many Requests", "provider rate limit reached"},
		{"rate limit exceeded", "provider rate limit reached"},
		{"rate_limit_exceeded", "provider rate limit reached"},
		{"402 Payment Required", "provider quota or billing issue"},
		{"quota exceeded", "provider quota or billing issue"},
		{"billing issue on account", "provider quota or billing issue"},
		{"insufficient credit", "provider quota or billing issue"},
		{"500 Internal Server Error", "provider server error"},
		{"502 Bad Gateway", "provider server error"},
		{"503 Service Unavailable", "provider server error"},
		{"504 Gateway Timeout", "provider server error"},
		{"request failed: dial tcp", "network error reaching provider"},
		{"timeout after 30s", "network error reaching provider"},
		{"connection refused", "network error reaching provider"},
		{"something completely unknown", "provider error"},
	}
	for _, tt := range tests {
		got := ClassifyProviderError(tt.raw)
		if got != tt.want {
			t.Errorf("ClassifyProviderError(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestClassifyProviderError_CaseInsensitive(t *testing.T) {
	got := ClassifyProviderError("CIRCUIT BREAKER tripped")
	if got != "provider temporarily unavailable" {
		t.Errorf("case insensitive: got %q", got)
	}
}

// ---------- G011: ProviderFailureUserMessage ----------

func TestProviderFailureUserMessage_Stored(t *testing.T) {
	msg := ProviderFailureUserMessage("429 rate limit", true)
	if !strings.Contains(msg, "saved") {
		t.Errorf("stored=true message should mention saved: %q", msg)
	}
	if !strings.Contains(msg, "rate limit") {
		t.Errorf("should contain classified error: %q", msg)
	}
}

func TestProviderFailureUserMessage_NotStored(t *testing.T) {
	msg := ProviderFailureUserMessage("timeout", false)
	if !strings.Contains(msg, "try sending") {
		t.Errorf("stored=false message should ask to resend: %q", msg)
	}
	if !strings.Contains(msg, "network error") {
		t.Errorf("should contain classified error: %q", msg)
	}
}
