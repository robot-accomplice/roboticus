package tools

import "strings"

// OperationClass captures the kind of real-world effect a tool can have.
// The pipeline uses this to keep tool selection and proof-of-work rules
// aligned to one authoritative tool-semantics map instead of scattering
// ad hoc name checks across routing, guards, and UI.
type OperationClass string

type ReplayClass string

const (
	OperationUnknown             OperationClass = "unknown"
	OperationInspection          OperationClass = "inspection"
	OperationRuntimeContextRead  OperationClass = "runtime_context_read"
	OperationWorkspaceInspect    OperationClass = "workspace_inspect"
	OperationCapabilityInventory OperationClass = "capability_inventory"
	OperationTaskInspection      OperationClass = "task_inspection"
	OperationArtifactWrite       OperationClass = "artifact_write"
	OperationAuthorityWrite      OperationClass = "authority_write"
	OperationExecution           OperationClass = "execution"
	OperationMemoryRead          OperationClass = "memory_read"
	OperationDelegation          OperationClass = "delegation"

	ReplayUnknown   ReplayClass = "unknown"
	ReplaySafe      ReplayClass = "safe"
	ReplayProtected ReplayClass = "protected"
)

// OperationClassForName returns the central semantic classification for a tool.
// Unknown names intentionally degrade to OperationUnknown rather than being
// guessed from prompt text at the point of use.
func OperationClassForName(name string) OperationClass {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "obsidian_write", "write_file", "edit_file":
		return OperationArtifactWrite
	case "ingest_policy":
		return OperationAuthorityWrite
	case "get_runtime_context":
		return OperationRuntimeContextRead
	case "list_directory":
		return OperationWorkspaceInspect
	case "task-status", "list-open-tasks", "get_subagent_status":
		return OperationTaskInspection
	case "list-subagent-roster", "list-available-skills":
		return OperationCapabilityInventory
	case "recall_memory", "search_memories", "get_memory_stats":
		return OperationMemoryRead
	case "compose-subagent", "orchestrate-subagents", "retry-task":
		return OperationDelegation
	case "bash", "echo":
		return OperationExecution
	default:
		return OperationUnknown
	}
}

// ReplayClassForName returns whether a tool can be safely replayed inside the
// same turn after a prior success. This keeps replay protection centralized in
// the same semantics map that already owns tool-shaping policy.
func ReplayClassForName(name string) ReplayClass {
	switch OperationClassForName(name) {
	case OperationInspection,
		OperationRuntimeContextRead,
		OperationWorkspaceInspect,
		OperationCapabilityInventory,
		OperationTaskInspection,
		OperationMemoryRead:
		return ReplaySafe
	case OperationArtifactWrite,
		OperationAuthorityWrite,
		OperationExecution,
		OperationDelegation:
		return ReplayProtected
	default:
		return ReplayUnknown
	}
}

func WritesPersistentArtifact(name string) bool {
	return OperationClassForName(name) == OperationArtifactWrite
}

func MutatesAuthorityLayer(name string) bool {
	return OperationClassForName(name) == OperationAuthorityWrite
}

func ReadsRuntimeContext(name string) bool {
	return OperationClassForName(name) == OperationRuntimeContextRead
}

func RequiresReplayProtection(name string) bool {
	return ReplayClassForName(name) == ReplayProtected
}

func IsReadOnlyExploration(name string) bool {
	switch OperationClassForName(name) {
	case OperationInspection,
		OperationRuntimeContextRead,
		OperationWorkspaceInspect,
		OperationCapabilityInventory,
		OperationTaskInspection,
		OperationMemoryRead:
		return true
	default:
		return false
	}
}

func MakesExecutionProgress(name string) bool {
	switch OperationClassForName(name) {
	case OperationArtifactWrite,
		OperationAuthorityWrite,
		OperationExecution,
		OperationDelegation:
		return true
	default:
		return false
	}
}
