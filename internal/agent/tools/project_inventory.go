package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ProjectInventoryTool struct{}

type projectInventoryArgs struct {
	Root  string `json:"root"`
	Limit int    `json:"limit,omitempty"`
}

type ProjectInventoryRow struct {
	Path            string   `json:"path"`
	Name            string   `json:"name"`
	Languages       []string `json:"languages"`
	FirstEditDate   string   `json:"first_edit_date,omitempty"`
	LastEditDate    string   `json:"last_edit_date,omitempty"`
	RemoteDirection string   `json:"remote_direction"`
}

type ProjectInventoryResult struct {
	Root         string                `json:"root"`
	ProjectCount int                   `json:"project_count"`
	Projects     []ProjectInventoryRow `json:"projects"`
}

func (t *ProjectInventoryTool) Name() string { return "inventory_projects" }
func (t *ProjectInventoryTool) Description() string {
	return "Inspect immediate child project directories under a workspace-relative or absolute allowed root and return project-level metadata such as name, languages, first/last edit dates, and git remote direction."
}
func (t *ProjectInventoryTool) Risk() RiskLevel { return RiskCaution }
func (t *ProjectInventoryTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"root":{"type":"string","description":"Workspace-relative path or absolute allowed directory containing project folders"},"limit":{"type":"integer","description":"Maximum number of project directories to inspect","default":200}},"required":["root"]}`)
}

func (t *ProjectInventoryTool) Execute(ctx context.Context, params string, tctx *Context) (*Result, error) {
	var args projectInventoryArgs
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if strings.TrimSpace(args.Root) == "" {
		return nil, fmt.Errorf("root is required")
	}
	if args.Limit <= 0 || args.Limit > 500 {
		args.Limit = 200
	}

	root, err := tctx.ResolveReadPath(args.Root)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("failed to read project root: %w", err)
	}

	projects := make([]ProjectInventoryRow, 0, len(entries))
	for _, entry := range entries {
		if len(projects) >= args.Limit {
			break
		}
		if !entry.IsDir() || shouldSkipProjectDir(entry.Name()) {
			continue
		}
		projectPath := filepath.Join(root, entry.Name())
		projects = append(projects, inspectProjectDirectory(ctx, projectPath, tctx.GetRunner()))
	}

	sort.SliceStable(projects, func(i, j int) bool {
		return projectSortKey(projects[i]).After(projectSortKey(projects[j]))
	})

	payload := ProjectInventoryResult{
		Root:         root,
		ProjectCount: len(projects),
		Projects:     projects,
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode project inventory: %w", err)
	}

	proof := NewInspectionProof("project_inventory", "inventory_projects", root, len(projects))
	return &Result{Output: string(body), Metadata: proof.Metadata()}, nil
}

func inspectProjectDirectory(ctx context.Context, projectPath string, runner interface {
	Run(ctx context.Context, name string, args []string, dir string, env []string) ([]byte, []byte, error)
}) ProjectInventoryRow {
	row := ProjectInventoryRow{
		Path:            projectPath,
		Name:            filepath.Base(projectPath),
		RemoteDirection: "unknown",
	}

	languages, first, last := scanProjectFiles(projectPath)
	row.Languages = languages
	if !first.IsZero() {
		row.FirstEditDate = first.Format("2006-01-02")
	}
	if !last.IsZero() {
		row.LastEditDate = last.Format("2006-01-02")
	}
	row.RemoteDirection = gitRemoteDirection(ctx, runner, projectPath)
	return row
}

func scanProjectFiles(root string) ([]string, time.Time, time.Time) {
	const maxFiles = 4000
	fileCount := 0
	langCounts := make(map[string]int)
	var first, last time.Time

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && shouldSkipProjectDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if fileCount >= maxFiles {
			return fs.SkipAll
		}
		fileCount++

		info, statErr := d.Info()
		if statErr == nil {
			mod := info.ModTime()
			if first.IsZero() || mod.Before(first) {
				first = mod
			}
			if last.IsZero() || mod.After(last) {
				last = mod
			}
		}

		if lang := languageForPath(path); lang != "" {
			langCounts[lang]++
		}
		return nil
	})

	type kv struct {
		Lang  string
		Count int
	}
	pairs := make([]kv, 0, len(langCounts))
	for lang, count := range langCounts {
		pairs = append(pairs, kv{Lang: lang, Count: count})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].Count == pairs[j].Count {
			return pairs[i].Lang < pairs[j].Lang
		}
		return pairs[i].Count > pairs[j].Count
	})
	languages := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		languages = append(languages, pair.Lang)
		if len(languages) >= 5 {
			break
		}
	}
	return languages, first, last
}

func shouldSkipProjectDir(name string) bool {
	if name == "" {
		return true
	}
	lower := strings.ToLower(name)
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch lower {
	case "node_modules", "vendor", "dist", "build", "target", "out", "coverage", ".next", "tmp":
		return true
	default:
		return false
	}
}

func languageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "Go"
	case ".py":
		return "Python"
	case ".js":
		return "JavaScript"
	case ".ts", ".tsx":
		return "TypeScript"
	case ".jsx":
		return "JavaScript"
	case ".rs":
		return "Rust"
	case ".sol":
		return "Solidity"
	case ".rb":
		return "Ruby"
	case ".java":
		return "Java"
	case ".c", ".h":
		return "C"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "C++"
	case ".swift":
		return "Swift"
	case ".kt":
		return "Kotlin"
	case ".php":
		return "PHP"
	case ".sh":
		return "Shell"
	case ".html":
		return "HTML"
	case ".css", ".scss":
		return "CSS"
	default:
		return ""
	}
}

func gitRemoteDirection(ctx context.Context, runner interface {
	Run(ctx context.Context, name string, args []string, dir string, env []string) ([]byte, []byte, error)
}, projectPath string) string {
	if runner == nil {
		return "unknown"
	}

	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	stdout, _, err := runner.Run(checkCtx, "git", []string{"rev-parse", "--is-inside-work-tree"}, projectPath, nil)
	if err != nil || strings.TrimSpace(string(stdout)) != "true" {
		return "not_git"
	}

	branchCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	branchOut, _, err := runner.Run(branchCtx, "git", []string{"rev-parse", "--abbrev-ref", "HEAD"}, projectPath, nil)
	if err != nil {
		return "unknown"
	}
	branch := strings.TrimSpace(string(branchOut))
	if branch == "" || branch == "HEAD" {
		return "unknown"
	}

	compare := []string{"rev-list", "--left-right", "--count", "origin/" + branch + "...HEAD"}
	compareCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	countOut, _, err := runner.Run(compareCtx, "git", compare, projectPath, nil)
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(string(countOut))
	if len(fields) < 2 {
		return "unknown"
	}
	behind, ahead := fields[0], fields[1]
	switch {
	case behind == "0" && ahead == "0":
		return "up_to_date"
	case behind != "0" && ahead == "0":
		return "behind"
	case behind == "0" && ahead != "0":
		return "ahead"
	default:
		return "diverged"
	}
}

func projectSortKey(row ProjectInventoryRow) time.Time {
	if strings.TrimSpace(row.LastEditDate) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse("2006-01-02", row.LastEditDate)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
