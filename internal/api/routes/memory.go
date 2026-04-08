package routes

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"roboticus/internal/db"
	"roboticus/internal/pipeline"
)

// GetWorkingMemory returns all working memory entries.
func GetWorkingMemory(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.NewRouteQueries(store).ListWorkingMemory(r.Context(), "", 100)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query working memory")
			return
		}
		defer func() { _ = rows.Close() }()

		var entries []map[string]any
		for rows.Next() {
			var id, sessionID, entryType, content, createdAt string
			var importance int
			if err := rows.Scan(&id, &sessionID, &entryType, &content, &importance, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read working memory row")
				return
			}
			entries = append(entries, map[string]any{
				"id": id, "session_id": sessionID, "entry_type": entryType,
				"content": content, "importance": importance, "created_at": createdAt,
			})
		}
		if entries == nil {
			entries = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	}
}

// GetSessionWorkingMemory returns working memory for a specific session.
func GetSessionWorkingMemory(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "session_id")
		rows, err := db.NewRouteQueries(store).ListWorkingMemory(r.Context(), sessionID, 100)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query session working memory")
			return
		}
		defer func() { _ = rows.Close() }()

		var entries []map[string]any
		for rows.Next() {
			var id, sid, entryType, content, createdAt string
			var importance int
			if err := rows.Scan(&id, &sid, &entryType, &content, &importance, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read session working memory row")
				return
			}
			entries = append(entries, map[string]any{
				"id": id, "session_id": sid, "entry_type": entryType,
				"content": content, "importance": importance, "created_at": createdAt,
			})
		}
		if entries == nil {
			entries = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	}
}

// GetEpisodicMemory returns episodic memory entries.
func GetEpisodicMemory(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.NewRouteQueries(store).ListEpisodicMemory(r.Context(), 100)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query episodic memory")
			return
		}
		defer func() { _ = rows.Close() }()

		var entries []map[string]any
		for rows.Next() {
			var id, classification, content, createdAt string
			var importance int
			if err := rows.Scan(&id, &classification, &content, &importance, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read episodic memory row")
				return
			}
			entries = append(entries, map[string]any{
				"id": id, "classification": classification,
				"content": content, "importance": importance, "created_at": createdAt,
			})
		}
		if entries == nil {
			entries = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	}
}

// GetSemanticMemory returns all semantic memory.
func GetSemanticMemory(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.NewRouteQueries(store).ListSemanticMemory(r.Context(), "", 200)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query semantic memory")
			return
		}
		defer func() { _ = rows.Close() }()

		var entries []map[string]any
		for rows.Next() {
			var id, category, key, value, createdAt string
			var confidence float64
			if err := rows.Scan(&id, &category, &key, &value, &confidence, &createdAt); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read semantic memory row")
				return
			}
			entries = append(entries, map[string]any{
				"id": id, "category": category, "key": key, "value": value,
				"confidence": confidence, "created_at": createdAt,
			})
		}
		if entries == nil {
			entries = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	}
}

// GetSemanticMemoryByCategory returns semantic memory filtered by category.
func GetSemanticMemoryByCategory(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		category := chi.URLParam(r, "category")
		limit := parseIntParam(r, "limit", 100)
		rows, err := db.NewRouteQueries(store).ListSemanticMemory(r.Context(), category, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query semantic memory by category")
			return
		}
		defer func() { _ = rows.Close() }()

		var entries []map[string]any
		for rows.Next() {
			var id, cat, key, value string
			var confidence float64
			if err := rows.Scan(&id, &cat, &key, &value, &confidence); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to read semantic memory row")
				return
			}
			entries = append(entries, map[string]any{
				"id": id, "category": cat, "key": key, "value": value,
				"confidence": confidence,
			})
		}
		if entries == nil {
			entries = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	}
}

// IngestKnowledge inserts a knowledge entry into semantic memory.
func IngestKnowledge(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Content  string `json:"content"`
			Category string `json:"category"`
			Key      string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Content == "" || req.Category == "" || req.Key == "" {
			writeError(w, http.StatusBadRequest, "content, category, and key are required")
			return
		}
		id := db.NewID()
		memRepo := db.NewMemoryRepository(store)
		if err := memRepo.StoreSemantic(r.Context(), id, req.Category, req.Key, req.Content, 1.0); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "ingested"})
	}
}

// SearchMemory searches across memory tiers using FTS5 or LIKE fallback.
func SearchMemory(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
			return
		}

		var results []map[string]any
		pattern := "%" + query + "%"

		// Search working memory.
		rows, err := db.NewRouteQueries(store).SearchWorkingMemory(r.Context(), pattern, 20)
		if err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var id, tier, entryType, content, createdAt string
				if rows.Scan(&id, &tier, &entryType, &content, &createdAt) == nil {
					results = append(results, map[string]any{
						"id": id, "tier": tier, "entry_type": entryType,
						"content": content, "created_at": createdAt,
					})
				}
			}
		}

		// Search episodic memory.
		rows2, err := db.NewRouteQueries(store).SearchEpisodicMemory(r.Context(), pattern, 20)
		if err == nil {
			defer func() { _ = rows2.Close() }()
			for rows2.Next() {
				var id, tier, classification, content, createdAt string
				if rows2.Scan(&id, &tier, &classification, &content, &createdAt) == nil {
					results = append(results, map[string]any{
						"id": id, "tier": tier, "entry_type": classification,
						"content": content, "created_at": createdAt,
					})
				}
			}
		}

		// Search semantic memory.
		rows3, err := db.NewRouteQueries(store).SearchSemanticMemory(r.Context(), pattern, 20)
		if err == nil {
			defer func() { _ = rows3.Close() }()
			for rows3.Next() {
				var id, tier, category, value, createdAt string
				if rows3.Scan(&id, &tier, &category, &value, &createdAt) == nil {
					results = append(results, map[string]any{
						"id": id, "tier": tier, "entry_type": category,
						"content": value, "created_at": createdAt,
					})
				}
			}
		}

		if results == nil {
			results = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	}
}

// TriggerConsolidation runs the memory consolidation pipeline on demand.
func TriggerConsolidation(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		force, _ := strconv.ParseBool(r.URL.Query().Get("force"))
		report := pipeline.RunMemoryConsolidation(r.Context(), store, force)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"report": map[string]any{
				"indexed":            report.Indexed,
				"deduped":            report.Deduped,
				"promoted":           report.Promoted,
				"confidence_decayed": report.ConfidenceDecayed,
				"importance_decayed": report.ImportanceDecayed,
				"pruned":             report.Pruned,
				"orphaned":           report.Orphaned,
			},
		})
	}
}

// TriggerReindex rebuilds the ANN memory index from persisted embeddings.
func TriggerReindex(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report, err := pipeline.RebuildMemoryIndex(r.Context(), store)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to rebuild memory index")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":          true,
			"built":       report.IndexBuilt,
			"entry_count": report.EntryCount,
		})
	}
}
