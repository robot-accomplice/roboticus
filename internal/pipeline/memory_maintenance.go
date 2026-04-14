package pipeline

import (
	"context"
	"strings"

	agentmemory "roboticus/internal/agent/memory"
	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// MemoryMaintenanceReport captures the outcome of operator-triggered memory maintenance.
type MemoryMaintenanceReport struct {
	Consolidation agentmemory.ConsolidationReport `json:"consolidation"`
	IndexBuilt    bool                            `json:"index_built"`
	EntryCount    int                             `json:"entry_count"`
}

// ConsolidationOpts configures optional consolidation dependencies.
type ConsolidationOpts struct {
	EmbedClient *llm.EmbeddingClient
	LLMService  *llm.Service
}

// RunMemoryConsolidation executes the production consolidation pipeline from the
// canonical pipeline layer so API connectors stay thin.
func RunMemoryConsolidation(ctx context.Context, store *db.Store, force bool, opts ...ConsolidationOpts) agentmemory.ConsolidationReport {
	pipe := agentmemory.NewConsolidationPipeline()
	if force {
		pipe.MinInterval = 0
	}
	if len(opts) > 0 {
		o := opts[0]
		if o.EmbedClient != nil {
			pipe.EmbedClient = o.EmbedClient
		}
		if o.LLMService != nil {
			pipe.Distiller = &agentmemory.ServiceDistiller{LLMSvc: o.LLMService}
		}
	}
	return pipe.Run(ctx, store)
}

// RebuildMemoryIndex rebuilds the ANN index from persisted embeddings.
func RebuildMemoryIndex(ctx context.Context, store *db.Store) (MemoryMaintenanceReport, error) {
	if err := ensureEmbeddingJSONColumn(ctx, store); err != nil {
		return MemoryMaintenanceReport{}, err
	}

	idx := db.NewBruteForceIndex(db.VectorIndexConfig{})
	if err := idx.BuildFromStore(store); err != nil {
		return MemoryMaintenanceReport{}, err
	}

	return MemoryMaintenanceReport{
		IndexBuilt: idx.IsBuilt(),
		EntryCount: idx.EntryCount(),
	}, nil
}

func ensureEmbeddingJSONColumn(ctx context.Context, store *db.Store) error {
	_, err := store.ExecContext(ctx, `ALTER TABLE embeddings ADD COLUMN embedding_json TEXT`)
	if err == nil {
		return nil
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "duplicate column") || strings.Contains(lower, "duplicate column name") {
		return nil
	}
	return err
}
