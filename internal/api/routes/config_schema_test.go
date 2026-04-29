package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"roboticus/internal/core"
)

func TestGetConfigSchema(t *testing.T) {
	cfg := core.DefaultConfig()
	handler := GetConfigSchema(&cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/config/schema", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result struct {
		Fields []SchemaField `json:"fields"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(result.Fields) == 0 {
		t.Fatal("expected non-empty fields list")
	}

	// Verify we have fields from multiple sections.
	sections := map[string]bool{}
	for _, f := range result.Fields {
		sections[f.Section] = true
	}
	expectedSections := []string{"agent", "server", "models", "memory", "cache", "security", "wallet", "session"}
	for _, s := range expectedSections {
		if !sections[s] {
			t.Errorf("expected section %q in schema fields", s)
		}
	}

	// Verify a specific field has correct metadata.
	var agentName *SchemaField
	for i, f := range result.Fields {
		if f.Name == "agent.name" {
			agentName = &result.Fields[i]
			break
		}
	}
	if agentName == nil {
		t.Fatal("expected agent.name field in schema")
	}
	if agentName.Type != "string" {
		t.Errorf("agent.name type = %q, want string", agentName.Type)
	}
	if agentName.Section != "agent" {
		t.Errorf("agent.name section = %q, want agent", agentName.Section)
	}
	if agentName.Default != "roboticus" {
		t.Errorf("agent.name default = %v, want roboticus", agentName.Default)
	}

	// Verify enum fields.
	var logLevel *SchemaField
	for i, f := range result.Fields {
		if f.Name == "agent.log_level" {
			logLevel = &result.Fields[i]
			break
		}
	}
	if logLevel == nil {
		t.Fatal("expected agent.log_level field in schema")
	}
	if len(logLevel.Enum) == 0 {
		t.Error("expected agent.log_level to have enum values")
	}

	// Verify immutable fields.
	var serverPort *SchemaField
	for i, f := range result.Fields {
		if f.Name == "server.port" {
			serverPort = &result.Fields[i]
			break
		}
	}
	if serverPort == nil {
		t.Fatal("expected server.port field in schema")
	}
	if !serverPort.Immutable {
		t.Error("expected server.port to be immutable")
	}
	if serverPort.Type != "integer" {
		t.Errorf("server.port type = %q, want integer", serverPort.Type)
	}

	// Verify nested struct fields use dot notation.
	var routingMode *SchemaField
	for i, f := range result.Fields {
		if f.Name == "models.routing.mode" {
			routingMode = &result.Fields[i]
			break
		}
	}
	if routingMode == nil {
		t.Fatal("expected models.routing.mode field in schema")
	}
	if routingMode.Section != "models" {
		t.Errorf("models.routing.mode section = %q, want models", routingMode.Section)
	}
	if len(routingMode.Enum) == 0 {
		t.Error("expected models.routing.mode to have enum values")
	}
	for _, value := range routingMode.Enum {
		if value == "round_robin" || value == "cost_aware" || value == "local_first" {
			t.Fatalf("models.routing.mode advertised invalid core mode %q", value)
		}
	}

	var modelPolicy *SchemaField
	for i, f := range result.Fields {
		if f.Name == "models.policy" {
			modelPolicy = &result.Fields[i]
			break
		}
	}
	if modelPolicy == nil {
		t.Fatal("expected models.policy object field in schema")
	}
	if len(modelPolicy.Properties["state"].Enum) == 0 {
		t.Fatal("expected models.policy.state to expose enum metadata")
	}
	if len(modelPolicy.Properties["primary_reason_code"].Enum) == 0 {
		t.Fatal("expected models.policy.primary_reason_code to expose enum metadata")
	}
	if len(modelPolicy.Properties["reason_codes"].ItemEnum) == 0 {
		t.Fatal("expected models.policy.reason_codes to expose item enum metadata")
	}
	if modelPolicy.Properties["source"].Description == "" {
		t.Fatal("expected models.policy.source to include explanatory metadata")
	}

	// Verify current == default when using DefaultConfig.
	if agentName.Current != agentName.Default {
		t.Errorf("agent.name current (%v) != default (%v) for DefaultConfig", agentName.Current, agentName.Default)
	}

	// Verify boolean type.
	var cacheEnabled *SchemaField
	for i, f := range result.Fields {
		if f.Name == "cache.enabled" {
			cacheEnabled = &result.Fields[i]
			break
		}
	}
	if cacheEnabled == nil {
		t.Fatal("expected cache.enabled field in schema")
	}
	if cacheEnabled.Type != "boolean" {
		t.Errorf("cache.enabled type = %q, want boolean", cacheEnabled.Type)
	}

	// Verify float type.
	var hybridWeight *SchemaField
	for i, f := range result.Fields {
		if f.Name == "memory.hybrid_weight_override" {
			hybridWeight = &result.Fields[i]
			break
		}
	}
	if hybridWeight == nil {
		t.Fatal("expected memory.hybrid_weight_override field in schema")
	}
	if hybridWeight.Type != "float" {
		t.Errorf("memory.hybrid_weight_override type = %q, want float", hybridWeight.Type)
	}

	// Verify descriptions exist for fields with known descriptions.
	if agentName.Description == "" {
		t.Error("expected agent.name to have a description")
	}

	t.Logf("schema returned %d fields across %d sections", len(result.Fields), len(sections))
}

func TestGetConfigSchema_CurrentDiffersFromDefault(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Agent.Name = "custom-agent"
	cfg.Server.Port = 9999

	handler := GetConfigSchema(&cfg)
	req := httptest.NewRequest(http.MethodGet, "/api/config/schema", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var result struct {
		Fields []SchemaField `json:"fields"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	for _, f := range result.Fields {
		if f.Name == "agent.name" {
			if f.Current != "custom-agent" {
				t.Errorf("agent.name current = %v, want custom-agent", f.Current)
			}
			if f.Default != "roboticus" {
				t.Errorf("agent.name default = %v, want roboticus", f.Default)
			}
		}
		if f.Name == "server.port" {
			// JSON numbers unmarshal as float64.
			if cur, ok := f.Current.(float64); !ok || int(cur) != 9999 {
				// Direct reflect gives int.
				if cur, ok := f.Current.(int); !ok || cur != 9999 {
					t.Errorf("server.port current = %v, want 9999", f.Current)
				}
			}
		}
	}
}
