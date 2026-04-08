package mcp

import (
	"sort"
	"sync"
)

// MCPResource describes a resource exposed by an MCP server.
type MCPResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MIMEType    string `json:"mime_type"`
}

// MCPResourceRegistry is a thread-safe registry of MCP resources.
type MCPResourceRegistry struct {
	mu        sync.RWMutex
	resources map[string]MCPResource
}

// NewMCPResourceRegistry creates a new resource registry.
func NewMCPResourceRegistry() *MCPResourceRegistry {
	return &MCPResourceRegistry{
		resources: make(map[string]MCPResource),
	}
}

// Register adds or updates a resource in the registry.
func (r *MCPResourceRegistry) Register(resource MCPResource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resources[resource.URI] = resource
}

// Get retrieves a resource by URI.
func (r *MCPResourceRegistry) Get(uri string) (MCPResource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res, ok := r.resources[uri]
	return res, ok
}

// List returns all registered resources sorted by URI.
func (r *MCPResourceRegistry) List() []MCPResource {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]MCPResource, 0, len(r.resources))
	for _, res := range r.resources {
		result = append(result, res)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].URI < result[j].URI
	})
	return result
}
