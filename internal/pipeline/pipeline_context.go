package pipeline

import (
	"time"

	"roboticus/internal/core"
)

// pipelineContext carries mutable state threaded through pipeline stages.
// This replaces the ~20 local variables that were scattered across Run().
type pipelineContext struct {
	// Immutable inputs.
	cfg   Config
	input Input
	start time.Time

	// Trace recorder — spans are opened and closed by each stage.
	tr *TraceRecorder
	dr *TurnDiagnosticsRecorder

	// State accumulated across stages.
	session          *Session
	content          string // execution content; may be expanded for short-followup context
	storedContent    string // operator-authored transcript content after safety sanitization
	msgID            string
	turnID           string
	taskID           string
	dedupFP          string // dedup fingerprint — Release is deferred in Run()
	isBotCommand     bool
	threatCaution    bool
	correctionTurn   bool
	decomp           *DecompositionResult
	synthesis        TaskSynthesis
	policy           TurnEnvelopePolicy
	memoryBlock      string
	delegationResult string
	cacheFingerprint string
	secClaim         core.SecurityClaim
}
