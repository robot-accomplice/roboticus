// Package agent — context_compress.go ports Rust's
// `compress_context` from `crates/roboticus-agent/src/context.rs`.
//
// This is the authoritative Go prompt-compression wrapper. It operates
// on an already-assembled []llm.Message, but intentionally compresses a
// NARROWER surface than the historical Rust behavior:
//
//   - the last user message is preserved verbatim
//   - all system messages are preserved verbatim
//   - only older assistant history is eligible for llm.SmartCompress
//
// That narrowing is deliberate. In Go, the system layer carries richer
// memory, hippocampus, and checkpoint-style ambient context than the
// older Rust baseline. Rewriting those system messages through a lossy
// compressor would undermine the novel memory architecture we are trying
// to preserve.
//
// Scope boundary:
//   - This function ONLY does ratio-based content compression of
//     individual messages. It does NOT drop messages, reorder them, or
//     split them.
//   - Message-level budget enforcement (trimming history to fit a
//     token budget) is owned by ContextBuilder.compact / BuildRequest,
//     not by this function. Compression runs before that trim so the
//     trim operates on the already-compressed content.
//   - Topic-block summarization of off-topic history is owned by
//     agent.PartitionByTopic / SummarizeTopicBlock, not by this
//     function.
//
// SYS-01-007 tracks the ownership split. Before v1.0.6 P2 Go also
// carried llm.PromptCompressor + llm.CompressWithTopicAwareness; those
// were dead code and are deleted with this commit.

package agent

import (
	"roboticus/internal/llm"
)

// minCompressibleContentLen is the per-message content length below
// which compression is skipped. Matches Rust's 200-char floor — below
// ~50 tokens compression rarely helps and can destroy small-but-
// important messages. Keep this exported-looking by keeping the name
// stable across the ports; consumers should NOT override it per call.
const minCompressibleContentLen = 200

// CompressContextMessages rewrites only compressible assistant HISTORY messages
// whose content is at least minCompressibleContentLen into their
// SmartCompress output at the given targetRatio. Mutates the messages
// slice in place.
//
// Rust parity: roboticus-agent/src/context.rs::compress_context.
//
// Invariants the caller relies on:
//   - User messages are returned unchanged. Operator-authored turns may contain
//     active session constraints, corrections, and output rules; they are not
//     elastic compression material.
//   - System messages are returned unchanged. This preserves the system
//     prompt, memory block, memory index, hippocampus summaries, and
//     any future checkpoint/system-note injections exactly as assembled.
//   - Tool / structured payload messages are returned unchanged. Prompt
//     compression is an assistant-history quality valve, not a transport
//     rewrite.
//   - No message is dropped or reordered. Only Content fields change.
//   - When targetRatio is outside [0.1, 1.0], SmartCompress clamps it;
//     this function does not clamp separately so there is a single
//     authoritative clamp site.
//   - A targetRatio of 1.0 (or above) leaves every message unchanged
//     (SmartCompress returns the input verbatim at full ratio). So the
//     caller can use ratio==1.0 as a no-op marker without needing a
//     separate gate.
//
// Safe to call on empty input.
func CompressContextMessages(messages []llm.Message, targetRatio float64) {
	if len(messages) == 0 {
		return
	}

	for i := range messages {
		if !isCompressibleHistoryMessage(messages[i]) {
			continue
		}
		if len(messages[i].Content) < minCompressibleContentLen {
			continue
		}
		messages[i].Content = llm.SmartCompress(messages[i].Content, targetRatio)
	}
}

func isCompressibleHistoryMessage(msg llm.Message) bool {
	return msg.Role == "assistant"
}
