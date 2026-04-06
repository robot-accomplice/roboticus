package channel

import "testing"

func TestMentionFilter(t *testing.T) {
	f := &MentionFilter{BotName: "Roboticus"}
	if !f.ShouldProcess(&InboundMessage{Content: "hey @Roboticus help"}) {
		t.Error("should match mention")
	}
	if !f.ShouldProcess(&InboundMessage{Content: "roboticus please"}) {
		t.Error("should be case-insensitive")
	}
	if f.ShouldProcess(&InboundMessage{Content: "hello world"}) {
		t.Error("should not match without mention")
	}
}

func TestReplyFilter(t *testing.T) {
	f := &ReplyFilter{}
	if f.ShouldProcess(&InboundMessage{Content: "hi"}) {
		t.Error("should not match without metadata")
	}
	if f.ShouldProcess(&InboundMessage{Content: "hi", Metadata: map[string]any{"reply_to_bot": false}}) {
		t.Error("should not match when false")
	}
	if !f.ShouldProcess(&InboundMessage{Content: "hi", Metadata: map[string]any{"reply_to_bot": true}}) {
		t.Error("should match when true")
	}
}

func TestConversationFilter(t *testing.T) {
	f := &ConversationFilter{}
	if !f.ShouldProcess(&InboundMessage{Content: "hi"}) {
		t.Error("should pass DM (no metadata)")
	}
	if !f.ShouldProcess(&InboundMessage{Content: "hi", Metadata: map[string]any{"is_group": false}}) {
		t.Error("should pass non-group")
	}
	if f.ShouldProcess(&InboundMessage{Content: "hi", Metadata: map[string]any{"is_group": true}}) {
		t.Error("should block group message")
	}
}

func TestFilterChain_AllMustPass(t *testing.T) {
	chain := NewFilterChain(
		&MentionFilter{BotName: "bot"},
		&ConversationFilter{},
	)
	// DM with mention: both pass.
	if !chain.ShouldProcess(&InboundMessage{Content: "bot help"}) {
		t.Error("should pass when all filters match")
	}
	// DM without mention: mention filter fails.
	if chain.ShouldProcess(&InboundMessage{Content: "help"}) {
		t.Error("should fail when any filter fails")
	}
}

func TestDefaultAddressabilityChain(t *testing.T) {
	chain := DefaultAddressabilityChain("Roboticus")

	// DM passes.
	if !chain.ShouldProcess(&InboundMessage{Content: "hello"}) {
		t.Error("DM should pass")
	}
	// Group with mention passes.
	msg := &InboundMessage{
		Content:  "hey roboticus",
		Metadata: map[string]any{"is_group": true},
	}
	if !chain.ShouldProcess(msg) {
		t.Error("group with mention should pass")
	}
	// Group without mention or reply fails.
	msg = &InboundMessage{
		Content:  "random chat",
		Metadata: map[string]any{"is_group": true},
	}
	if chain.ShouldProcess(msg) {
		t.Error("unaddressed group message should fail")
	}
}
