package channel

import "strings"

// MessageFilter decides whether an inbound message should be processed.
type MessageFilter interface {
	ShouldProcess(msg *InboundMessage) bool
}

// FilterChain composes multiple filters — all must pass for a message to proceed.
type FilterChain struct {
	filters []MessageFilter
}

// NewFilterChain creates a filter chain from the given filters.
func NewFilterChain(filters ...MessageFilter) *FilterChain {
	return &FilterChain{filters: filters}
}

// ShouldProcess returns true only if every filter passes.
func (fc *FilterChain) ShouldProcess(msg *InboundMessage) bool {
	for _, f := range fc.filters {
		if !f.ShouldProcess(msg) {
			return false
		}
	}
	return true
}

// MentionFilter passes messages that mention the bot by name (case-insensitive).
type MentionFilter struct {
	BotName string
}

func (f *MentionFilter) ShouldProcess(msg *InboundMessage) bool {
	return strings.Contains(strings.ToLower(msg.Content), strings.ToLower(f.BotName))
}

// ReplyFilter passes messages that are replies to the bot.
// Checks the metadata "reply_to_bot" flag set by channel adapters.
type ReplyFilter struct{}

func (f *ReplyFilter) ShouldProcess(msg *InboundMessage) bool {
	if msg.Metadata == nil {
		return false
	}
	v, ok := msg.Metadata["reply_to_bot"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// ConversationFilter passes all direct messages (non-group) unconditionally.
// Group messages are only passed if they match another filter in the chain.
type ConversationFilter struct{}

func (f *ConversationFilter) ShouldProcess(msg *InboundMessage) bool {
	if msg.Metadata == nil {
		return true // assume DM if no metadata
	}
	isGroup, _ := msg.Metadata["is_group"].(bool)
	return !isGroup
}

// OrFilter passes if ANY of its sub-filters pass.
type OrFilter struct {
	Filters []MessageFilter
}

func (f *OrFilter) ShouldProcess(msg *InboundMessage) bool {
	for _, sub := range f.Filters {
		if sub.ShouldProcess(msg) {
			return true
		}
	}
	return false
}

// DefaultAddressabilityChain returns the standard filter for group chats:
// pass if DM, OR if mentioned, OR if reply to bot.
func DefaultAddressabilityChain(botName string) *FilterChain {
	return NewFilterChain(
		&OrFilter{
			Filters: []MessageFilter{
				&ConversationFilter{},
				&MentionFilter{BotName: botName},
				&ReplyFilter{},
			},
		},
	)
}
