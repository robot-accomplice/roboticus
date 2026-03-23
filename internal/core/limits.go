package core

// System-wide size and rate limits.
const (
	// MaxUserMessageBytes is the maximum size of a user message (100 KB).
	MaxUserMessageBytes = 100 * 1024

	// MaxFileReadBytes is the maximum file read size for tools (10 MB).
	MaxFileReadBytes = 10 * 1024 * 1024

	// MaxObsidianNoteBytes is the maximum Obsidian note size (5 MB).
	MaxObsidianNoteBytes = 5 * 1024 * 1024

	// MaxDeliveryItemBytes is the maximum delivery queue item size (100 KB).
	MaxDeliveryItemBytes = 100 * 1024

	// MaxInterviewTurns caps interview sessions to prevent unbounded growth.
	MaxInterviewTurns = 200

	// RateLimitPerIPCap is the maximum number of per-IP rate limit entries.
	RateLimitPerIPCap = 10_000

	// RateLimitPerActorCap is the maximum number of per-actor rate limit entries.
	RateLimitPerActorCap = 5_000

	// DefaultServerPort is the default HTTP server port.
	DefaultServerPort = 18789

	// DefaultServerBind is the default HTTP server bind address.
	DefaultServerBind = "127.0.0.1"
)
