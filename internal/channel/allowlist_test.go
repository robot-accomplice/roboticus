package channel

import "testing"

func TestCheckAllowlist_EmptyAllowAll(t *testing.T) {
	if !CheckAllowlist("anyone", nil) {
		t.Error("empty allowlist should allow all")
	}
}

func TestCheckAllowlist_ExactMatch(t *testing.T) {
	list := []string{"alice", "bob"}
	if !CheckAllowlist("alice", list) {
		t.Error("should match exact")
	}
	if CheckAllowlist("charlie", list) {
		t.Error("should not match non-member")
	}
}

func TestCheckAllowlist_CaseInsensitive(t *testing.T) {
	if !CheckAllowlist("Alice", []string{"alice"}) {
		t.Error("should be case-insensitive")
	}
}

func TestCheckAllowlist_PhoneSuffix(t *testing.T) {
	list := []string{"+15551234567"}
	if !CheckAllowlist("5551234567", list) {
		t.Error("bare number should match E.164")
	}
	if !CheckAllowlist("+15551234567", list) {
		t.Error("exact E.164 should match")
	}
	if CheckAllowlist("5559999999", list) {
		t.Error("different number should not match")
	}
}

func TestCheckAllowlist_PhoneSuffixReverse(t *testing.T) {
	list := []string{"5551234567"}
	if !CheckAllowlist("+15551234567", list) {
		t.Error("E.164 sender should match bare allowlist entry")
	}
}
