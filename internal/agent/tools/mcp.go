package tools

import (
	"encoding/json"
	"sync"
)

// McpToolDescriptor represents a tool exposed via the Model Context Protocol.
type McpToolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// McpResource represents a resource exposed via MCP.
type McpResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mime_type,omitempty"`
}

// McpServerRegistry manages MCP-exposed tools and resources.
type McpServerRegistry struct {
	mu        sync.RWMutex
	tools     map[string]*McpToolDescriptor
	resources map[string]*McpResource
}

// NewMcpServerRegistry creates an empty MCP registry.
func NewMcpServerRegistry() *McpServerRegistry {
	return &McpServerRegistry{
		tools:     make(map[string]*McpToolDescriptor),
		resources: make(map[string]*McpResource),
	}
}

// RegisterTool adds an MCP tool descriptor.
func (r *McpServerRegistry) RegisterTool(d *McpToolDescriptor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[d.Name] = d
}

// RegisterResource adds an MCP resource descriptor.
func (r *McpServerRegistry) RegisterResource(res *McpResource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resources[res.URI] = res
}

// ListTools returns all registered MCP tools.
func (r *McpServerRegistry) ListTools() []*McpToolDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*McpToolDescriptor, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// GetTool retrieves a tool by name.
func (r *McpServerRegistry) GetTool(name string) *McpToolDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// ListResources returns all registered MCP resources.
func (r *McpServerRegistry) ListResources() []*McpResource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*McpResource, 0, len(r.resources))
	for _, res := range r.resources {
		out = append(out, res)
	}
	return out
}

// GetResource retrieves a resource by URI.
func (r *McpServerRegistry) GetResource(uri string) *McpResource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resources[uri]
}

// ToolCount returns the number of registered tools.
func (r *McpServerRegistry) ToolCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// ResourceCount returns the number of registered resources.
func (r *McpServerRegistry) ResourceCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.resources)
}

// ExportToMcp creates an MCP registry from the standard tool registry.
func (reg *Registry) ExportToMcp() *McpServerRegistry {
	mcp := NewMcpServerRegistry()
	for _, t := range reg.List() {
		mcp.RegisterTool(&McpToolDescriptor{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.ParameterSchema(),
		})
	}
	return mcp
}
