package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"roboticus/internal/db"
)

// CronTool lets the agent manage scheduled cron jobs.
type CronTool struct{}

func (t *CronTool) Name() string        { return "cron" }
func (t *CronTool) Description() string { return "Manage scheduled cron jobs (create, list, delete)." }
func (t *CronTool) Risk() RiskLevel     { return RiskCaution }
func (t *CronTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action":   {"type": "string", "enum": ["create", "list", "delete"], "description": "Action to perform"},
			"name":     {"type": "string", "description": "Job name (for create; derived from task if omitted)"},
			"schedule": {"type": "string", "description": "Cron expression (for create)"},
			"task":     {"type": "string", "description": "Task description / payload (for create)"},
			"id":       {"type": "string", "description": "Job ID or name (for delete)"}
		},
		"required": ["action"]
	}`)
}

func (t *CronTool) Execute(ctx context.Context, params string, tctx *Context) (*Result, error) {
	var args struct {
		Action   string `json:"action"`
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Task     string `json:"task"`
		ID       string `json:"id"`
	}
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if tctx.Store == nil {
		return nil, fmt.Errorf("database store not available")
	}

	switch args.Action {
	case "list":
		return t.list(ctx, tctx)
	case "create":
		return t.create(ctx, args.Name, args.Schedule, args.Task, tctx)
	case "delete":
		return t.delete(ctx, args.ID, tctx)
	default:
		return nil, fmt.Errorf("unknown action %q; use create, list, or delete", args.Action)
	}
}

func (t *CronTool) list(ctx context.Context, tctx *Context) (*Result, error) {
	rows, err := tctx.Store.QueryContext(ctx,
		`SELECT id, name, schedule_expr, enabled FROM cron_jobs ORDER BY id DESC LIMIT 20`)
	if err != nil {
		return nil, fmt.Errorf("query cron_jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type job struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Enabled  bool   `json:"enabled"`
	}

	var jobs []job
	for rows.Next() {
		var j job
		var schedExpr *string
		var enabled int
		if err := rows.Scan(&j.ID, &j.Name, &schedExpr, &enabled); err != nil {
			return nil, fmt.Errorf("scan cron_jobs row: %w", err)
		}
		if schedExpr != nil {
			j.Schedule = *schedExpr
		}
		j.Enabled = enabled != 0
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cron_jobs: %w", err)
	}

	if len(jobs) == 0 {
		return &Result{Output: "No cron jobs found."}, nil
	}

	data, err := json.Marshal(jobs)
	if err != nil {
		return nil, fmt.Errorf("marshal jobs: %w", err)
	}
	return &Result{Output: string(data)}, nil
}

func (t *CronTool) create(ctx context.Context, name, schedule, task string, tctx *Context) (*Result, error) {
	name = strings.TrimSpace(name)
	task = strings.TrimSpace(task)
	if name == "" {
		name = deriveCronJobName(task)
	}
	if strings.TrimSpace(schedule) == "" {
		return nil, fmt.Errorf("schedule is required for create")
	}
	if task == "" {
		return nil, fmt.Errorf("task is required for create")
	}

	id := db.NewID()
	payload, err := json.Marshal(map[string]string{"task": task})
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	// Default delivery to the current session's channel so cron output reaches the user.
	deliveryMode := "none"
	deliveryChannel := ""
	if tctx.Channel != "" {
		deliveryMode = "push"
		deliveryChannel = tctx.Channel
	}

	_, err = tctx.Store.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, name, schedule_kind, schedule_expr, agent_id, payload_json, delivery_mode, delivery_channel)
		 VALUES (?, ?, 'cron', ?, ?, ?, ?, ?)`,
		id, name, schedule, tctx.AgentID, string(payload), deliveryMode, deliveryChannel)
	if err != nil {
		return nil, fmt.Errorf("insert cron_job: %w", err)
	}

	return &Result{Output: fmt.Sprintf("Created cron job %q (id=%s, schedule=%s, delivery=%s/%s)", name, id, schedule, deliveryMode, deliveryChannel)}, nil
}

func deriveCronJobName(task string) string {
	words := strings.Fields(task)
	if len(words) == 0 {
		return ""
	}
	parts := make([]string, 0, 4)
	for _, word := range words {
		word = strings.Map(func(r rune) rune {
			switch {
			case unicode.IsLetter(r), unicode.IsDigit(r):
				return unicode.ToLower(r)
			case r == '-' || r == '_':
				return r
			default:
				return -1
			}
		}, word)
		if word == "" {
			continue
		}
		parts = append(parts, word)
		if len(parts) == 4 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "-")
}

func (t *CronTool) delete(ctx context.Context, idOrName string, tctx *Context) (*Result, error) {
	if strings.TrimSpace(idOrName) == "" {
		return nil, fmt.Errorf("id is required for delete")
	}

	res, err := tctx.Store.ExecContext(ctx,
		`DELETE FROM cron_jobs WHERE id = ? OR name = ?`, idOrName, idOrName)
	if err != nil {
		return nil, fmt.Errorf("delete cron_job: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return &Result{Output: fmt.Sprintf("No cron job found with id or name %q", idOrName)}, nil
	}
	return &Result{Output: fmt.Sprintf("Deleted %d cron job(s) matching %q", affected, idOrName)}, nil
}
