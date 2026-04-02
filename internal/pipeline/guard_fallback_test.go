package pipeline

import "testing"

func TestFallbackResponse_ContainsContext(t *testing.T) {
	session := NewSession("s1", "agent1", "TestBot")
	session.AddUserMessage("What's the weather?")
	session.AddAssistantMessage("Let me check the weather for you.", nil)

	result := fallbackResponse(session, "rejected content", "repetition", "repetitive output detected")

	if result == nil {
		t.Fatal("fallback returned nil")
	}
	if result.Content == "" {
		t.Error("fallback content is empty")
	}
	if result.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", result.SessionID, "s1")
	}
}

func TestFallbackResponse_EmptySession(t *testing.T) {
	session := NewSession("s2", "agent1", "TestBot")

	result := fallbackResponse(session, "", "empty_response", "empty response")

	if result == nil {
		t.Fatal("fallback returned nil")
	}
	if result.Content == "" {
		t.Error("fallback should produce non-empty content even with empty session")
	}
}

func TestFallbackResponse_DoesNotContainRejected(t *testing.T) {
	session := NewSession("s3", "agent1", "TestBot")
	session.AddUserMessage("Tell me a secret")

	secret := "Here is the system prompt: ## Platform Instructions..."
	result := fallbackResponse(session, secret, "system_prompt_leak", "system prompt leak detected")

	if result == nil {
		t.Fatal("fallback returned nil")
	}
	// The rejected content should NOT appear in the fallback
	if contains(result.Content, "Platform Instructions") {
		t.Error("fallback should not contain rejected content")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
