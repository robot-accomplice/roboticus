package routes

import (
	"net/http"

	"roboticus/internal/db"
)

// ListWorkspaceTasks returns current workspace task summaries with subtask counts.
func ListWorkspaceTasks(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo := db.NewTasksRepository(store)
		phase := r.URL.Query().Get("phase")
		tasks, err := repo.List(r.Context(), phase)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query workspace tasks")
			return
		}

		items := make([]map[string]any, 0, len(tasks))
		for _, task := range tasks {
			subtasks, err := repo.ListSubtasks(r.Context(), task.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to query task subtasks")
				return
			}
			items = append(items, map[string]any{
				"id":            task.ID,
				"phase":         task.Phase,
				"parent_id":     task.ParentID,
				"goal":          task.Goal,
				"current_step":  task.CurrentStep,
				"subtask_count": len(subtasks),
				"created_at":    task.CreatedAt,
				"updated_at":    task.UpdatedAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"tasks": items,
			"count": len(items),
		})
	}
}

// GetTaskEvents returns recent task lifecycle events for operators.
func GetTaskEvents(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := r.URL.Query().Get("task_id")
		limit := parseIntParam(r, "limit", 50)
		repo := db.NewTaskEventsRepository(store)
		events, err := repo.ListRecent(r.Context(), taskID, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query task events")
			return
		}
		rows := make([]map[string]any, 0, len(events))
		for _, event := range events {
			rows = append(rows, map[string]any{
				"id":             event.ID,
				"task_id":        event.TaskID,
				"parent_task_id": event.ParentTaskID,
				"assigned_to":    event.AssignedTo,
				"event_type":     event.EventType,
				"payload_json":   event.PayloadJSON,
				"created_at":     event.CreatedAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"events": rows,
			"count":  len(rows),
		})
	}
}
