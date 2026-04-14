package routes

import (
	"net/http"
	"reflect"
	"strings"

	"roboticus/internal/core"
)

// SchemaField describes a single config field for the settings UI.
type SchemaField struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Default     any      `json:"default"`
	Current     any      `json:"current"`
	Section     string   `json:"section"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Immutable   bool     `json:"immutable,omitempty"`
}

// immutableSections lists top-level config sections that require a restart.
var immutableSections = map[string]bool{
	"server":   true,
	"a2a":      true,
	"wallet":   true,
	"database": true,
}

// knownEnums maps dotted field paths to valid enum values.
var knownEnums = map[string][]string{
	"agent.log_level":                 {"trace", "debug", "info", "warn", "error"},
	"agent.composition_policy":        {"propose", "sequential", "parallel", "adaptive"},
	"agent.skill_creation_rigor":      {"generate", "validate", "full"},
	"agent.output_validation_policy":  {"strict", "sample", "off"},
	"models.routing.mode":             {"primary", "fallback", "auto", "metascore", "round_robin", "cost_aware", "local_first"},
	"session.scope_mode":              {"agent", "peer", "group"},
	"security.threat_caution_ceiling": {"low", "medium", "high", "critical"},
	"security.allowlist_authority":    {"None", "Peer", "Operator", "Creator"},
	"security.trusted_authority":      {"None", "Peer", "Operator", "Creator"},
	"security.api_authority":          {"None", "Peer", "Operator", "Creator"},
	"context_budget.channel_minimum":  {"L0", "L1", "L2", "L3"},
	"yield.protocol":                  {"aave", "compound"},
	"yield.chain":                     {"base", "ethereum", "polygon"},
}

// fieldDescriptions maps dotted field paths to human-readable descriptions.
// Falls back to the fieldTooltips map in the dashboard for anything not covered here.
var fieldDescriptions = map[string]string{
	"agent.name":                                "The agent's display name.",
	"agent.id":                                  "Unique identifier for this agent instance.",
	"agent.workspace":                           "Root directory for agent file operations.",
	"agent.log_level":                           "Logging verbosity: trace, debug, info, warn, error.",
	"agent.autonomy_max_react_turns":            "Max consecutive ReAct turns the agent can take autonomously.",
	"agent.autonomy_max_turn_duration_seconds":  "Max seconds per autonomous turn before timeout.",
	"agent.delegation_enabled":                  "Allow this agent to delegate sub-tasks to specialists.",
	"agent.delegation_min_complexity":            "Minimum task complexity score (0-1) for delegation.",
	"agent.delegation_min_utility_margin":        "Minimum expected utility gain to justify delegation.",
	"agent.specialist_creation_requires_approval": "Require human approval before spawning a specialist.",
	"agent.composition_policy":                  "Multi-agent orchestration: propose, sequential, parallel, adaptive.",
	"agent.skill_creation_rigor":                "Rigor for auto-generated skills: generate, validate, full.",
	"agent.output_validation_policy":            "Output quality checking: strict, sample, or off.",
	"agent.output_validation_sample_rate":        "Fraction of outputs to validate when policy is sample.",
	"agent.max_output_retries":                  "Maximum retries when output validation fails.",
	"agent.retirement_success_threshold":         "Success rate below which a specialist is retired.",
	"agent.retirement_min_delegations":           "Minimum delegations before retirement evaluation applies.",
	"server.port":                               "HTTP server port. Requires restart.",
	"server.bind":                               "Network interface to bind. Requires restart.",
	"server.api_key":                            "API key for authenticating dashboard and API requests.",
	"server.log_dir":                            "Directory for server log files.",
	"server.cron_max_concurrency":               "Max concurrent cron jobs.",
	"server.log_max_days":                       "Days to retain log files before rotation.",
	"models.primary":                            "Primary LLM model (e.g. openai/gpt-4o).",
	"models.stream_by_default":                  "Stream LLM responses by default.",
	"models.routing.mode":                       "Routing strategy: primary, fallback, auto, metascore, etc.",
	"models.routing.confidence_threshold":       "Minimum confidence to accept a model's response.",
	"models.routing.local_first":                "Prefer locally-hosted models when available.",
	"models.routing.cost_aware":                 "Factor inference cost into routing decisions.",
	"models.routing.per_provider_timeout_seconds": "Max seconds to wait for a single provider.",
	"models.routing.max_total_inference_seconds": "Hard ceiling on total inference time.",
	"models.routing.max_fallback_attempts":      "Max fallback models to try before giving up.",
	"memory.working_budget":                     "Percentage of memory budget for working memory.",
	"memory.episodic_budget":                    "Percentage for episodic (conversation) memory.",
	"memory.semantic_budget":                    "Percentage for semantic (knowledge) memory.",
	"memory.procedural_budget":                  "Percentage for procedural (skill) memory.",
	"memory.relationship_budget":                "Percentage for relationship memory.",
	"memory.hybrid_weight_override":              "Manual override for hybrid search weight (0=adaptive, >0=fixed blend). Default: 0 (adaptive based on corpus size).",
	"memory.decay_half_life_days":               "Half-life in days for memory recency decay.",
	"session.scope_mode":                        "Session scoping: agent, peer, or group.",
	"session.ttl_seconds":                       "Session time-to-live in seconds. 0 = no expiry.",
	"security.workspace_only":                   "Restrict all file operations to the workspace.",
	"security.deny_on_empty_allowlist":           "Deny all when allowlist is empty (fail-closed).",
	"security.sandbox_required":                 "Require sandboxed execution for all tools.",
	"wallet.chain_id":                           "Blockchain chain ID. Requires restart.",
	"wallet.rpc_url":                            "RPC endpoint URL for blockchain. Requires restart.",
	"wallet.balance_poll_seconds":               "Seconds between wallet balance checks.",
	"cache.enabled":                             "Enable semantic cache.",
	"cache.ttl_seconds":                         "Cache entry time-to-live in seconds.",
	"cache.similarity_threshold":                "Minimum similarity for cache hits.",
	"cache.max_entries":                          "Max cached entries before eviction.",
	"treasury.daily_cap":                        "Max total spend per day.",
	"treasury.per_payment_cap":                  "Max amount for a single payment.",
	"treasury.daily_inference_budget":            "Max daily spend on LLM inference.",
}

// GetConfigSchema returns a JSON schema derived from the Config struct.
func GetConfigSchema(cfg *core.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defaults := core.DefaultConfig()
		fields := walkStruct(reflect.ValueOf(defaults), reflect.ValueOf(*cfg), "", "")
		writeJSON(w, http.StatusOK, map[string]any{
			"fields": fields,
		})
	}
}

// walkStruct recursively walks a struct and produces SchemaField entries.
func walkStruct(defaultVal, currentVal reflect.Value, prefix, section string) []SchemaField {
	var fields []SchemaField

	defaultType := defaultVal.Type()
	for i := 0; i < defaultType.NumField(); i++ {
		field := defaultType.Field(i)
		if !field.IsExported() {
			continue
		}

		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		// Strip omitempty and other options.
		jsonName := strings.Split(jsonTag, ",")[0]
		if jsonName == "" {
			continue
		}

		dottedPath := jsonName
		if prefix != "" {
			dottedPath = prefix + "." + jsonName
		}

		fieldSection := section
		if fieldSection == "" {
			fieldSection = jsonName
		}

		defField := defaultVal.Field(i)
		curField := currentVal.Field(i)

		fieldType := field.Type
		// Dereference pointers.
		if fieldType.Kind() == reflect.Ptr {
			// For pointer fields, unwrap if non-nil, otherwise use zero value.
			if !defField.IsNil() {
				defField = defField.Elem()
			} else {
				defField = reflect.Zero(fieldType.Elem())
			}
			if !curField.IsNil() {
				curField = curField.Elem()
			} else {
				curField = reflect.Zero(fieldType.Elem())
			}
			fieldType = fieldType.Elem()
		}

		// Skip maps (providers is a map — handled separately by existing UI).
		if fieldType.Kind() == reflect.Map {
			continue
		}

		// Recurse into structs.
		if fieldType.Kind() == reflect.Struct {
			nested := walkStruct(defField, curField, dottedPath, fieldSection)
			fields = append(fields, nested...)
			continue
		}

		// Handle slices of structs (like MCP servers, knowledge sources) — skip.
		if fieldType.Kind() == reflect.Slice && fieldType.Elem().Kind() == reflect.Struct {
			continue
		}

		sf := SchemaField{
			Name:    dottedPath,
			Type:    goTypeToSchemaType(fieldType),
			Default: valueToInterface(defField),
			Current: valueToInterface(curField),
			Section: fieldSection,
		}

		if desc, ok := fieldDescriptions[dottedPath]; ok {
			sf.Description = desc
		}
		if enums, ok := knownEnums[dottedPath]; ok {
			sf.Enum = enums
		}

		// Mark immutable if the top-level section requires restart.
		topSection := fieldSection
		if idx := strings.IndexByte(topSection, '.'); idx > 0 {
			topSection = topSection[:idx]
		}
		if immutableSections[topSection] {
			sf.Immutable = true
		}

		fields = append(fields, sf)
	}

	return fields
}

// goTypeToSchemaType maps Go reflect.Kind to a schema type string.
func goTypeToSchemaType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.Slice:
		return "array"
	default:
		return "string"
	}
}

// valueToInterface converts a reflect.Value to a Go interface for JSON marshaling.
func valueToInterface(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return valueToInterface(v.Elem())
	case reflect.Slice:
		if v.IsNil() {
			return []any{}
		}
		result := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			result[i] = valueToInterface(v.Index(i))
		}
		return result
	case reflect.Map:
		if v.IsNil() {
			return map[string]any{}
		}
		result := make(map[string]any)
		for _, k := range v.MapKeys() {
			result[k.String()] = valueToInterface(v.MapIndex(k))
		}
		return result
	default:
		return v.Interface()
	}
}
