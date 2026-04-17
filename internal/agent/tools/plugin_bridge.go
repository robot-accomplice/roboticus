package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog/log"

	"roboticus/internal/plugin"
)

// PluginBridgeTool exposes a plugin-backed tool through the agent Tool registry.
type PluginBridgeTool struct {
	name        string
	description string
	schema      json.RawMessage
	risk        RiskLevel
	pluginName  string
	registry    *plugin.Registry
}

func (t *PluginBridgeTool) Name() string                  { return t.name }
func (t *PluginBridgeTool) Description() string           { return t.description }
func (t *PluginBridgeTool) Risk() RiskLevel               { return t.risk }
func (t *PluginBridgeTool) ParameterSchema() json.RawMessage { return t.schema }

func (t *PluginBridgeTool) Execute(ctx context.Context, params string, _ *Context) (*Result, error) {
	result, err := t.registry.ExecuteTool(ctx, t.name, json.RawMessage(params))
	if err != nil {
		return nil, fmt.Errorf("plugin tool %s: %w", t.name, err)
	}
	return &Result{
		Output:   result.Output,
		Metadata: result.Metadata,
		Source:   "plugin:" + t.pluginName,
	}, nil
}

// RegisterPluginTools synchronizes active plugin tools into the agent registry.
// Builtin/MCP tools keep precedence on name conflicts; colliding plugin tools are skipped.
func RegisterPluginTools(registry *Registry, pluginReg *plugin.Registry) int {
	if registry == nil || pluginReg == nil {
		return 0
	}

	registered := 0
	activePlugins := pluginReg.List()
	activeNames := make(map[string]bool, len(activePlugins))
	activeToolNames := make(map[string]bool)

	for _, info := range activePlugins {
		if info.Status != plugin.StatusActive {
			continue
		}
		activeNames[info.Name] = true
		for _, td := range pluginReg.PluginTools(info.Name) {
			activeToolNames[td.Name] = true
			if existing := registry.Get(td.Name); existing != nil {
				if _, ok := existing.(*PluginBridgeTool); !ok {
					log.Warn().Str("tool", td.Name).Str("plugin", info.Name).
						Msg("skipping plugin tool registration due to existing non-plugin tool")
					continue
				}
			}
			registry.Register(&PluginBridgeTool{
				name:        td.Name,
				description: td.Description,
				schema:      td.Parameters,
				risk:        pluginRisk(td.RiskLevel),
				pluginName:  info.Name,
				registry:    pluginReg,
			})
			registered++
		}
	}

	for _, toolName := range registry.Names() {
		existing := registry.Get(toolName)
		bridge, ok := existing.(*PluginBridgeTool)
		if !ok {
			continue
		}
		if !activeNames[bridge.pluginName] || !activeToolNames[toolName] {
			registry.Unregister(toolName)
		}
	}

	return registered
}

func pluginRisk(level string) RiskLevel {
	switch level {
	case plugin.RiskLevelHigh:
		return RiskDangerous
	case plugin.RiskLevelCaution:
		return RiskCaution
	default:
		return RiskSafe
	}
}
