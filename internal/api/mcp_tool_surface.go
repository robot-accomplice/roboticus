package api

import (
	"context"

	"roboticus/internal/agent/tools"
	"roboticus/internal/llm"
	"roboticus/internal/mcp"
)

type mcpToolSurface struct {
	registry   *tools.Registry
	embeddings *llm.EmbeddingClient
}

func newMCPToolSurface(registry *tools.Registry, embeddings *llm.EmbeddingClient) *mcpToolSurface {
	if registry == nil {
		return nil
	}
	return &mcpToolSurface{
		registry:   registry,
		embeddings: embeddings,
	}
}

func (s *mcpToolSurface) SyncMCPToolSurface(ctx context.Context, mgr *mcp.ConnectionManager) {
	if s == nil || s.registry == nil || mgr == nil {
		return
	}
	tools.SyncMCPTools(s.registry, mgr)
	if s.embeddings != nil {
		_ = s.registry.EmbedDescriptors(ctx, s.embeddings)
	}
}
