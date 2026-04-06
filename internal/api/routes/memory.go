package routes

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"goboticus/internal/db"
)

// GetWorkingMemory returns all working memory entries.
func GetWorkingMemory(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, session_id, entry_type, content, importance, created_at
			 FROM working_memory ORDER BY created_at DESC LIMIT 100`)
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
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, session_id, entry_type, content, importance, created_at
			 FROM working_memory WHERE session_id = ? ORDER BY created_at DESC LIMIT 100`, sessionID)
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
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, classification, content, importance, created_at
			 FROM episodic_memory ORDER BY created_at DESC LIMIT 100`)
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
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, category, key, value, confidence, created_at
			 FROM semantic_memory ORDER BY category, key LIMIT 200`)
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
		_, err := store.ExecContext(r.Context(),
			`INSERT INTO semantic_memory (id, category, key, value, confidence)
			 VALUES (?, ?, ?, ?, 1.0)`,
			id, req.Category, req.Key, req.Content)
		if err != nil {
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
		rows, err := store.QueryContext(r.Context(),
			`SELECT id, 'working' as tier, entry_type, content, created_at
			 FROM working_memory WHERE content LIKE ? LIMIT 20`, pattern)
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
		rows2, err := store.QueryContext(r.Context(),
			`SELECT id, 'episodic' as tier, classification, content, created_at
			 FROM episodic_memory WHERE content LIKE ? LIMIT 20`, pattern)
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
		rows3, err := store.QueryContext(r.Context(),
			`SELECT id, 'semantic' as tier, category, value, created_at
			 FROM semantic_memory WHERE value LIKE ? OR key LIKE ? LIMIT 20`, pattern, pattern)
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
