package tools

import (
	"encoding/json"
	"strings"
)

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
	OperationArtifactRead        OperationClass = "artifact_read"
	OperationWorkspaceInspect    OperationClass = "workspace_inspect"
	OperationCapabilityInventory OperationClass = "capability_inventory"
	OperationTaskInspection      OperationClass = "task_inspection"
	OperationArtifactWrite       OperationClass = "artifact_write"
	OperationAuthorityWrite      OperationClass = "authority_write"
	OperationScheduling          OperationClass = "scheduling"
	OperationExecution           OperationClass = "execution"
	OperationMemoryRead          OperationClass = "memory_read"
	OperationDelegation          OperationClass = "delegation"
	// OperationDataRead covers reads against the hippocampus / structured
	// data store (e.g. query_table). Treated as read-only exploration for
	// replay and admission purposes.
	OperationDataRead OperationClass = "data_read"
	// OperationDataWrite covers schema or row mutations against the
	// hippocampus (create_table, insert_row, alter_table, drop_table).
	// Replay-protected; counts as execution progress.
	OperationDataWrite OperationClass = "data_write"
	// OperationWebRead covers external HTTP reads (web_search, http_fetch).
	// Treated as read-only exploration; admitted in inspection and
	// analysis-authoring profiles where external context is useful.
	OperationWebRead OperationClass = "web_read"

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
	case "read_file":
		return OperationArtifactRead
	case "ingest_policy":
		return OperationAuthorityWrite
	case "cron":
		return OperationScheduling
	case "get_runtime_context":
		return OperationRuntimeContextRead
	case "glob_files", "list_directory", "search_files", "inventory_projects":
		return OperationWorkspaceInspect
	case "get_channel_health":
		return OperationInspection
	case "task-status", "list-open-tasks", "get_subagent_status":
		return OperationTaskInspection
	case "list-subagent-roster", "list-available-skills",
		"introspect", "introspection",
		"compose-skill":
		return OperationCapabilityInventory
	case "recall_memory", "search_memories", "get_memory_stats",
		"query_knowledge_graph", "find_workflow":
		return OperationMemoryRead
	case "compose-subagent", "orchestrate-subagents", "retry-task":
		return OperationDelegation
	case "bash", "echo":
		return OperationExecution
	case "query_table":
		return OperationDataRead
	case "create_table", "insert_row", "alter_table", "drop_table":
		return OperationDataWrite
	case "web_search", "http_fetch", "ghola",
		"browser_navigate", "browser_snapshot", "browser_take_screenshot",
		"browser_wait_for", "browser_tabs", "browser_console_messages",
		"browser_network_requests":
		return OperationWebRead
	case "browser_click", "browser_type", "browser_fill_form", "browser_press_key",
		"browser_select_option", "browser_hover", "browser_evaluate",
		"browser_run_code", "browser_drag", "browser_drop", "browser_file_upload":
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
		OperationArtifactRead,
		OperationWorkspaceInspect,
		OperationCapabilityInventory,
		OperationTaskInspection,
		OperationMemoryRead,
		OperationDataRead,
		OperationWebRead:
		return ReplaySafe
	case OperationArtifactWrite,
		OperationAuthorityWrite,
		OperationScheduling,
		OperationExecution,
		OperationDelegation,
		OperationDataWrite:
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

type ReplayFingerprint struct {
	Key      string
	Resource string
}

// ReplayFingerprintForCall resolves the protected resource/effect identity for
// a tool call. The loop uses this instead of raw argument equality when
// deciding whether a successful side effect may be replayed inside the same
// turn.
func ReplayFingerprintForCall(name, normalizedArgs string) ReplayFingerprint {
	key, resource := replayFingerprintKeyForOperation(OperationClassForName(name), normalizedArgs, nil)
	if key == "" {
		key = strings.TrimSpace(strings.ToLower(name)) + ":" + canonicalArgumentShape(normalizedArgs)
	}
	return ReplayFingerprint{Key: key, Resource: resource}
}

// ReplayFingerprintForResult resolves the authoritative protected
// resource/effect identity after a successful tool execution. Typed artifact
// proof, when present, outranks call arguments.
func ReplayFingerprintForResult(name, normalizedArgs string, metadata json.RawMessage) ReplayFingerprint {
	if proof, ok := ParseArtifactProof(metadata); ok {
		path := strings.TrimSpace(strings.ToLower(proof.Path))
		if path != "" {
			key := string(OperationArtifactWrite) + ":" + path
			return ReplayFingerprint{Key: key, Resource: proof.Path}
		}
	}
	return ReplayFingerprintForCall(name, normalizedArgs)
}

func IsReadOnlyExploration(name string) bool {
	switch OperationClassForName(name) {
	case OperationInspection,
		OperationRuntimeContextRead,
		OperationArtifactRead,
		OperationWorkspaceInspect,
		OperationCapabilityInventory,
		OperationTaskInspection,
		OperationMemoryRead,
		OperationDataRead,
		OperationWebRead:
		return true
	default:
		return false
	}
}

func MakesExecutionProgress(name string) bool {
	switch OperationClassForName(name) {
	case OperationArtifactWrite,
		OperationAuthorityWrite,
		OperationScheduling,
		OperationExecution,
		OperationDelegation,
		OperationDataWrite:
		return true
	default:
		return false
	}
}

func replayFingerprintKeyForOperation(op OperationClass, rawArgs string, metadata json.RawMessage) (string, string) {
	switch op {
	case OperationArtifactWrite:
		if proof, ok := ParseArtifactProof(metadata); ok {
			path := strings.TrimSpace(strings.ToLower(proof.Path))
			if path != "" {
				return string(op) + ":" + path, proof.Path
			}
		}
		if path := firstSemanticResource(rawArgs, "path", "file", "file_path", "target", "target_path", "note_path"); path != "" {
			return string(op) + ":" + path, path
		}
	case OperationAuthorityWrite:
		if resource := firstSemanticResource(rawArgs, "key", "name", "target", "target_id", "id"); resource != "" {
			return string(op) + ":" + resource, resource
		}
	}
	return "", ""
}

func firstSemanticResource(rawArgs string, keys ...string) string {
	parsed := parseJSONMap(rawArgs)
	if len(parsed) == 0 {
		return ""
	}
	for _, key := range keys {
		value, ok := parsed[key]
		if !ok {
			continue
		}
		if text := strings.TrimSpace(toStringValue(value)); text != "" {
			return strings.ToLower(text)
		}
	}
	return ""
}

func parseJSONMap(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}
	return decoded
}

func toStringValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	default:
		buf, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return string(buf)
	}
}

func canonicalArgumentShape(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return strings.ToLower(raw)
	}
	buf, err := json.Marshal(decoded)
	if err != nil {
		return strings.ToLower(raw)
	}
	return strings.ToLower(string(buf))
}
