package routes

import (
	"net/http"
	"sort"

	"roboticus/internal/db"
)

// ListWorkspaceTasks returns workspace tasks grouped by status with a summary object.
func ListWorkspaceTasks(store *db.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo := db.NewTasksRepository(store)
		phase := r.URL.Query().Get("phase")
		tasks, err := repo.List(r.Context(), phase)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to query workspace tasks")
			return
		}

		// Build items and track status counts.
		var activeCount, completedCount, failedCount int
		items := make([]map[string]any, 0, len(tasks))
		for _, task := range tasks {
			subtasks, err := repo.ListSubtasks(r.Context(), task.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to query task subtasks")
				return
			}

			status := classifyTaskPhase(task.Phase)
			switch status {
			case "active":
				activeCount++
			case "completed":
				completedCount++
			case "failed":
				failedCount++
			}

			item := map[string]any{
				"id":            task.ID,
				"phase":         task.Phase,
				"status":        status,
				"parent_id":     task.ParentID,
				"goal":          task.Goal,
				"current_step":  task.CurrentStep,
				"subtask_count": len(subtasks),
				"created_at":    task.CreatedAt,
				"updated_at":    task.UpdatedAt,
			}

			// For active tasks, include a progress indicator if current_step > 0.
			if status == "active" {
				item["latest_event_at"] = task.UpdatedAt
				if task.CurrentStep > 0 {
					item["progress"] = map[string]any{
						"current_step": task.CurrentStep,
					}
				}
			}

			items = append(items, item)
		}

		// Sort: active tasks first, then by most recent updated_at descending.
		sort.SliceStable(items, func(i, j int) bool {
			si := items[i]["status"].(string)
			sj := items[j]["status"].(string)
			if si != sj {
				return statusOrder(si) < statusOrder(sj)
			}
			ui := items[i]["updated_at"].(string)
			uj := items[j]["updated_at"].(string)
			return ui > uj // descending
		})

		total := activeCount + completedCount + failedCount
		writeJSON(w, http.StatusOK, map[string]any{
			"tasks": items,
			"count": len(items),
			"summary": map[string]any{
				"active":    activeCount,
				"completed": completedCount,
				"failed":    failedCount,
				"total":     total,
			},
		})
	}
}

// GetTaskEvents returns task events grouped by task_id with computed summaries.
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

		// Build rows and group events by task_id.
		rows := make([]map[string]any, 0, len(events))
		type taskInfo struct {
			currentState string
			eventCount   int
			latestAt     string
			assignedTo   string
		}
		taskMap := make(map[string]*taskInfo)

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

			info, ok := taskMap[event.TaskID]
			if !ok {
				info = &taskInfo{}
				taskMap[event.TaskID] = info
			}
			info.eventCount++
			// Events are ordered by created_at DESC, so the first event per task
			// is the most recent.
			if info.latestAt == "" || event.CreatedAt > info.latestAt {
				info.latestAt = event.CreatedAt
				info.currentState = event.EventType
			}
			if event.AssignedTo != "" {
				info.assignedTo = event.AssignedTo
			}
		}

		// Build task_summaries array.
		summaries := make([]map[string]any, 0, len(taskMap))
		for tid, info := range taskMap {
			summaries = append(summaries, map[string]any{
				"task_id":        tid,
				"current_state":  info.currentState,
				"event_count":    info.eventCount,
				"latest_event_at": info.latestAt,
				"assigned_to":    info.assignedTo,
			})
		}

		// Sort summaries by latest_event_at descending for deterministic output.
		sort.Slice(summaries, func(i, j int) bool {
			li := summaries[i]["latest_event_at"].(string)
			lj := summaries[j]["latest_event_at"].(string)
			return li > lj
		})

		writeJSON(w, http.StatusOK, map[string]any{
			"events":         rows,
			"count":          len(rows),
			"task_summaries": summaries,
		})
	}
}

// classifyTaskPhase maps a task phase string to a canonical status bucket.
func classifyTaskPhase(phase string) string {
	switch phase {
	case "completed", "done", "finished":
		return "completed"
	case "failed", "error", "cancelled":
		return "failed"
	default:
		return "active"
	}
}

// statusOrder returns a sort key so active < completed < failed.
func statusOrder(status string) int {
	switch status {
	case "active":
		return 0
	case "completed":
		return 1
	case "failed":
		return 2
	default:
		return 3
	}
}
