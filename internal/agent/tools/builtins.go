package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- Echo Tool ---

// EchoTool echoes input back (for testing/debug).
type EchoTool struct{}

func (t *EchoTool) Name() string { return "echo" }
func (t *EchoTool) Description() string {
	return "Echo the input message back as output. Useful for testing."
}
func (t *EchoTool) Risk() RiskLevel { return RiskSafe }
func (t *EchoTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"message":{"type":"string","description":"Message to echo"}},"required":["message"]}`)
}
func (t *EchoTool) Execute(_ context.Context, params string, _ *Context) (*Result, error) {
	var args struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	return &Result{Output: args.Message}, nil
}

// --- Read File Tool ---

// ReadFileTool reads a text file from the workspace.
type ReadFileTool struct{}

func (t *ReadFileTool) Name() string { return "read_file" }
func (t *ReadFileTool) Description() string {
	return "Read a UTF-8 text file from the workspace (max 1MB)."
}
func (t *ReadFileTool) Risk() RiskLevel { return RiskCaution }
func (t *ReadFileTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Relative path within workspace"}},"required":["path"]}`)
}
func (t *ReadFileTool) Execute(_ context.Context, params string, tctx *Context) (*Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	resolved, err := resolvePath(tctx.Workspace, args.Path, tctx.AllowedPaths)
	if err != nil {
		return nil, err
	}

	data, err := tctx.GetFS().ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	const maxBytes = 1 << 20 // 1MB
	if len(data) > maxBytes {
		return nil, fmt.Errorf("file exceeds 1MB limit (%d bytes)", len(data))
	}

	return &Result{Output: string(data)}, nil
}

// --- Write File Tool ---

// WriteFileTool writes content to a workspace file.
type WriteFileTool struct{}

func (t *WriteFileTool) Name() string { return "write_file" }
func (t *WriteFileTool) Description() string {
	return "Write text content to a file in the workspace. Creates parent directories if needed."
}
func (t *WriteFileTool) Risk() RiskLevel { return RiskCaution }
func (t *WriteFileTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Relative path within workspace"},"content":{"type":"string","description":"Content to write"},"append":{"type":"boolean","description":"Append instead of overwrite","default":false}},"required":["path","content"]}`)
}
func (t *WriteFileTool) Execute(_ context.Context, params string, tctx *Context) (*Result, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Append  bool   `json:"append"`
	}
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	resolved, err := resolvePath(tctx.Workspace, args.Path, tctx.AllowedPaths)
	if err != nil {
		return nil, err
	}

	if err := tctx.GetFS().MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directories: %w", err)
	}

	flag := os.O_WRONLY | os.O_CREATE
	if args.Append {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}

	f, err := tctx.GetFS().OpenFile(resolved, flag, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(args.Content); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return &Result{Output: fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path)}, nil
}

// --- Edit File Tool ---

// EditFileTool replaces text in an existing file.
type EditFileTool struct{}

func (t *EditFileTool) Name() string        { return "edit_file" }
func (t *EditFileTool) Description() string { return "Replace text in an existing workspace file." }
func (t *EditFileTool) Risk() RiskLevel     { return RiskCaution }
func (t *EditFileTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Relative path within workspace"},"old_text":{"type":"string","description":"Text to find"},"new_text":{"type":"string","description":"Replacement text"},"replace_all":{"type":"boolean","default":false}},"required":["path","old_text","new_text"]}`)
}
func (t *EditFileTool) Execute(_ context.Context, params string, tctx *Context) (*Result, error) {
	var args struct {
		Path       string `json:"path"`
		OldText    string `json:"old_text"`
		NewText    string `json:"new_text"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	resolved, err := resolvePath(tctx.Workspace, args.Path, tctx.AllowedPaths)
	if err != nil {
		return nil, err
	}

	data, err := tctx.GetFS().ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	content := string(data)
	if !strings.Contains(content, args.OldText) {
		return nil, fmt.Errorf("old_text not found in file")
	}

	var newContent string
	if args.ReplaceAll {
		newContent = strings.ReplaceAll(content, args.OldText, args.NewText)
	} else {
		newContent = strings.Replace(content, args.OldText, args.NewText, 1)
	}

	if err := tctx.GetFS().WriteFile(resolved, []byte(newContent), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return &Result{Output: "file edited successfully"}, nil
}

// --- List Directory Tool ---

// ListDirectoryTool lists files and folders.
type ListDirectoryTool struct{}

func (t *ListDirectoryTool) Name() string { return "list_directory" }
func (t *ListDirectoryTool) Description() string {
	return "List files and folders in a workspace directory."
}
func (t *ListDirectoryTool) Risk() RiskLevel { return RiskCaution }
func (t *ListDirectoryTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Relative path within workspace","default":"."}},"required":[]}`)
}
func (t *ListDirectoryTool) Execute(_ context.Context, params string, tctx *Context) (*Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	args.Path = "."
	if params != "" {
		_ = json.Unmarshal([]byte(params), &args)
	}

	resolved, err := resolvePath(tctx.Workspace, args.Path, tctx.AllowedPaths)
	if err != nil {
		return nil, err
	}

	entries, err := tctx.GetFS().ReadDir(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			b.WriteString(e.Name() + "/\n")
		} else {
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			fmt.Fprintf(&b, "%s (%d bytes)\n", e.Name(), size)
		}
	}
	return &Result{Output: b.String()}, nil
}

// --- Search Files Tool ---

// SearchFilesTool searches for text content across files.
type SearchFilesTool struct{}

func (t *SearchFilesTool) Name() string { return "search_files" }
func (t *SearchFilesTool) Description() string {
	return "Search for text content across workspace files with line number reporting."
}
func (t *SearchFilesTool) Risk() RiskLevel { return RiskCaution }
func (t *SearchFilesTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Text to search for"},"path":{"type":"string","description":"Directory to search in","default":"."},"limit":{"type":"integer","description":"Max results","default":20},"case_sensitive":{"type":"boolean","default":false}},"required":["query"]}`)
}
func (t *SearchFilesTool) Execute(_ context.Context, params string, tctx *Context) (*Result, error) {
	var args struct {
		Query         string `json:"query"`
		Path          string `json:"path"`
		Limit         int    `json:"limit"`
		CaseSensitive bool   `json:"case_sensitive"`
	}
	args.Path = "."
	args.Limit = 20
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if args.Limit > 100 {
		args.Limit = 100
	}

	resolved, err := resolvePath(tctx.Workspace, args.Path, tctx.AllowedPaths)
	if err != nil {
		return nil, err
	}

	query := args.Query
	if !args.CaseSensitive {
		query = strings.ToLower(query)
	}

	var results []string
	count := 0
	maxWalk := 5000

	_ = tctx.GetFS().Walk(resolved, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Size() > 1<<20 {
			return nil
		}
		maxWalk--
		if maxWalk <= 0 {
			return filepath.SkipAll
		}
		if count >= args.Limit {
			return filepath.SkipAll
		}

		// Skip binary files (simple heuristic).
		ext := strings.ToLower(filepath.Ext(path))
		binaryExts := map[string]bool{".exe": true, ".dll": true, ".so": true, ".bin": true, ".png": true, ".jpg": true, ".gif": true, ".pdf": true, ".zip": true, ".gz": true}
		if binaryExts[ext] {
			return nil
		}

		data, err := tctx.GetFS().ReadFile(path)
		if err != nil {
			return nil
		}

		content := string(data)
		searchContent := content
		if !args.CaseSensitive {
			searchContent = strings.ToLower(content)
		}

		lines := strings.Split(content, "\n")
		searchLines := strings.Split(searchContent, "\n")

		relPath, _ := filepath.Rel(tctx.Workspace, path)
		if relPath == "" {
			relPath = path
		}

		for i, line := range searchLines {
			if strings.Contains(line, query) {
				results = append(results, fmt.Sprintf("%s:%d: %s", relPath, i+1, strings.TrimSpace(lines[i])))
				count++
				if count >= args.Limit {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})

	if len(results) == 0 {
		return &Result{Output: "no matches found"}, nil
	}
	return &Result{Output: strings.Join(results, "\n")}, nil
}

// --- Glob Files Tool ---

// GlobFilesTool finds files matching a pattern.
type GlobFilesTool struct{}

func (t *GlobFilesTool) Name() string { return "glob_files" }
func (t *GlobFilesTool) Description() string {
	return "Find files matching a wildcard pattern under the workspace."
}
func (t *GlobFilesTool) Risk() RiskLevel { return RiskCaution }
func (t *GlobFilesTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Glob pattern (e.g., **/*.go)"},"path":{"type":"string","default":"."},"limit":{"type":"integer","default":50}},"required":["pattern"]}`)
}
func (t *GlobFilesTool) Execute(_ context.Context, params string, tctx *Context) (*Result, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Limit   int    `json:"limit"`
	}
	args.Path = "."
	args.Limit = 50
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if args.Limit > 500 {
		args.Limit = 500
	}

	resolved, err := resolvePath(tctx.Workspace, args.Path, tctx.AllowedPaths)
	if err != nil {
		return nil, err
	}

	pattern := filepath.Join(resolved, args.Pattern)
	matches, err := tctx.GetFS().Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern: %w", err)
	}

	if len(matches) > args.Limit {
		matches = matches[:args.Limit]
	}

	// Convert to relative paths.
	var results []string
	for _, m := range matches {
		rel, _ := filepath.Rel(tctx.Workspace, m)
		if rel == "" {
			rel = m
		}
		results = append(results, rel)
	}

	if len(results) == 0 {
		return &Result{Output: "no files matched"}, nil
	}
	return &Result{Output: strings.Join(results, "\n")}, nil
}

// --- Bash Tool ---

// BashTool executes shell commands.
type BashTool struct{}

func (t *BashTool) Name() string { return "bash" }
func (t *BashTool) Description() string {
	return "Execute a shell command in the workspace. Use with caution."
}
func (t *BashTool) Risk() RiskLevel { return RiskDangerous }
func (t *BashTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"},"cwd":{"type":"string","default":"."},"timeout_seconds":{"type":"integer","default":20,"minimum":1,"maximum":120}},"required":["command"]}`)
}
func (t *BashTool) Execute(ctx context.Context, params string, tctx *Context) (*Result, error) {
	var args struct {
		Command        string `json:"command"`
		Cwd            string `json:"cwd"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	args.Cwd = "."
	args.TimeoutSeconds = 20
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if args.TimeoutSeconds < 1 {
		args.TimeoutSeconds = 1
	}
	if args.TimeoutSeconds > 120 {
		args.TimeoutSeconds = 120
	}

	resolved, err := resolvePath(tctx.Workspace, args.Cwd, tctx.AllowedPaths)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(args.TimeoutSeconds) * time.Second
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	shellName, shellArgs := shellCommand(args.Command)
	stdout, stderr, err := tctx.GetRunner().Run(execCtx, shellName, shellArgs, resolved, nil)
	combined := string(stdout) + string(stderr)
	if err != nil {
		return &Result{Output: fmt.Sprintf("error: %v\n%s", err, combined)}, nil
	}

	return &Result{Output: combined}, nil
}

// --- Runtime Context Tool ---

// RuntimeContextTool reports agent runtime information.
type RuntimeContextTool struct{}

func (t *RuntimeContextTool) Name() string { return "get_runtime_context" }
func (t *RuntimeContextTool) Description() string {
	return "Report runtime context (agent id, session, workspace, allowed paths)."
}
func (t *RuntimeContextTool) Risk() RiskLevel { return RiskSafe }
func (t *RuntimeContextTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *RuntimeContextTool) Execute(_ context.Context, _ string, tctx *Context) (*Result, error) {
	info := fmt.Sprintf(`Agent: %s
Session: %s
Workspace: %s
Channel: %s
Allowed Paths: %s`,
		tctx.AgentID,
		tctx.SessionID,
		tctx.Workspace,
		tctx.Channel,
		strings.Join(tctx.AllowedPaths, ", "),
	)
	return &Result{Output: info}, nil
}

// --- Helpers ---

// resolvePath safely resolves a path within the workspace.
func resolvePath(workspace, path string, allowedPaths []string) (string, error) {
	return ResolvePath(path, workspace, &ToolSandboxSnapshot{AllowedPaths: allowedPaths})
}
