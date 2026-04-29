package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type InspectionTargetResolution struct {
	ResolvedPaths         []string
	PromptSummary         string
	ClarificationRequired bool
}

type FilesystemDestinationResolution struct {
	ResolvedRoot          string
	PromptSummary         string
	UseConfiguredVault    bool
	ClarificationRequired bool
}

type SourceCodeTargetResolution struct {
	ResolvedRoot  string
	PromptSummary string
}

// looksLikeFocusedInspectionTurn detects bounded workspace/filesystem
// inspection regardless of whether the operator phrased it imperatively
// ("count the files") or interrogatively ("what's in the vault").
func looksLikeFocusedInspectionTurn(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return false
	}
	explicitInspectionPath := len(extractInspectionPathCandidates(content)) > 0

	inspectionVerbs := []string{
		"count", "list", "find", "scan", "inspect", "look in", "look at", "show", "show me",
		"summary", "summarize",
	}
	inspectionQuestions := []string{
		"what are the",
		"what s in", "what is in", "whats in",
		"what s inside", "what is inside", "whats inside",
		"what s under", "what is under", "whats under",
		"what about",
	}
	inspectionTargets := []string{
		"file", "files", "directory", "directories", "folder", "folders",
		"workspace", "vault", "note", "notes", "contents", "content",
		"path", "paths", "inside", "project", "projects", "repo", "repos", "repository", "repositories",
	}
	pathMarkers := []string{"/users/", "/tmp/", "/var/", "/opt/", "/srv/", "~/", "home folder", "home directory", "my home"}

	if !containsAnyMarker(lower, inspectionTargets) {
		if !containsAnyMarker(lower, pathMarkers) {
			return false
		}
	}

	if explicitInspectionPath && containsAnyMarker(lower, inspectionTargets) {
		return true
	}

	return containsAnyMarker(lower, inspectionVerbs) || containsAnyMarker(lower, inspectionQuestions)
}

func ResolveInspectionTarget(content, workspace string, allowedPaths []string) InspectionTargetResolution {
	if !looksLikeFocusedInspectionTurn(content) {
		return InspectionTargetResolution{}
	}

	resolved := uniqueInspectionPaths(resolveExplicitInspectionPaths(content, workspace, allowedPaths))
	if len(resolved) == 0 {
		resolved = uniqueInspectionPaths(resolveAliasedInspectionPaths(content, workspace, allowedPaths))
	}

	switch len(resolved) {
	case 0:
		return InspectionTargetResolution{
			ClarificationRequired: true,
			PromptSummary:         "This turn is a filesystem inspection request, but the target path is still ambiguous. Ask one precise clarifying question requesting the exact path or folder name before answering.",
		}
	case 1:
		return InspectionTargetResolution{
			ResolvedPaths: resolved,
			PromptSummary: fmt.Sprintf("This turn is a filesystem inspection request. Resolved target path: %s. Inspect this target directly with list_directory, glob_files, or read_file before answering. Do not claim inability unless a tool call against this target fails.", resolved[0]),
		}
	default:
		return InspectionTargetResolution{
			ResolvedPaths:         resolved,
			ClarificationRequired: true,
			PromptSummary:         fmt.Sprintf("This turn is a filesystem inspection request. The target may refer to one of these paths: %s. Ask one precise clarifying question choosing among those paths before answering.", strings.Join(resolved, ", ")),
		}
	}
}

func resolveExplicitInspectionPaths(content, workspace string, allowedPaths []string) []string {
	raw := extractInspectionPathCandidates(content)
	if len(raw) == 0 {
		return nil
	}
	var resolved []string
	for _, candidate := range raw {
		if path, ok := qualifyInspectionPath(candidate, workspace, allowedPaths); ok {
			resolved = append(resolved, path)
		}
	}
	return resolved
}

func resolveAliasedInspectionPaths(content, workspace string, allowedPaths []string) []string {
	lower := strings.ToLower(strings.TrimSpace(content))
	var resolved []string

	if strings.Contains(lower, "desktop") && strings.Contains(lower, "vault") {
		for _, path := range allowedPaths {
			pathLower := strings.ToLower(path)
			baseLower := strings.ToLower(filepath.Base(path))
			if strings.Contains(pathLower, "/desktop/") && strings.Contains(baseLower, "vault") {
				resolved = append(resolved, filepath.Clean(path))
			}
		}
	}

	if strings.Contains(lower, "workspace") && strings.Contains(lower, "vault") {
		workspaceVault := filepath.Clean(filepath.Join(workspace, "Vault"))
		if _, ok := qualifyInspectionPath(workspaceVault, workspace, allowedPaths); ok {
			resolved = append(resolved, workspaceVault)
		}
		for _, path := range allowedPaths {
			pathLower := strings.ToLower(path)
			if strings.Contains(pathLower, "/workspace/vault") {
				resolved = append(resolved, filepath.Clean(path))
			}
		}
	}

	if strings.Contains(lower, "workspace") && (strings.Contains(lower, "projects") || strings.Contains(lower, "files") || strings.Contains(lower, "contents")) {
		if workspace != "" {
			resolved = append(resolved, filepath.Clean(workspace))
		}
	}
	if strings.Contains(lower, "code folder") || strings.Contains(lower, "code directory") || strings.Contains(lower, "code repo") || strings.Contains(lower, "code repos") {
		for _, path := range allowedPaths {
			pathLower := strings.ToLower(path)
			if strings.HasSuffix(pathLower, string(filepath.Separator)+"code") || strings.EqualFold(filepath.Base(path), "code") {
				resolved = append(resolved, filepath.Clean(path))
			}
		}
	}

	if strings.Contains(lower, "home folder") || strings.Contains(lower, "home directory") || strings.Contains(lower, "my home") {
		if home := currentUserHomeDir(); home != "" {
			if path, ok := qualifyInspectionPath(home, workspace, allowedPaths); ok {
				resolved = append(resolved, path)
			}
		}
	}

	commonHomeAliases := map[string][]string{
		"Downloads": []string{"downloads", "download folder", "downloads folder"},
		"Desktop":   []string{"desktop", "desktop folder"},
		"Documents": []string{"documents", "documents folder"},
	}
	for dir, markers := range commonHomeAliases {
		if !containsAnyMarker(lower, markers) {
			continue
		}
		if path := resolveHomeChildAlias(dir, workspace, allowedPaths); path != "" {
			resolved = append(resolved, path)
		}
	}

	return resolved
}

func resolveHomeChildAlias(dir, workspace string, allowedPaths []string) string {
	home := currentUserHomeDir()
	if home == "" {
		return ""
	}
	candidate := filepath.Join(home, dir)
	if path, ok := qualifyInspectionPath(candidate, workspace, allowedPaths); ok {
		return path
	}
	for _, allowed := range allowedPaths {
		if strings.EqualFold(filepath.Base(allowed), dir) {
			return filepath.Clean(allowed)
		}
	}
	return ""
}

var absolutePathCandidateRE = regexp.MustCompile(`/[^\n\r\t,;]+`)
var tildePathCandidateRE = regexp.MustCompile(`(?:^|\s)(~(?:/[^\n\r\t,;]+)?)`)

func extractInspectionPathCandidates(content string) []string {
	matches := absolutePathCandidateRE.FindAllString(content, -1)
	tildeMatches := tildePathCandidateRE.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 && len(tildeMatches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches)+len(tildeMatches))
	for _, match := range matches {
		candidate := cleanInspectionPathCandidate(match)
		if strings.Contains(candidate, "/Desktop ") && !strings.Contains(candidate, "/Desktop/") {
			candidate = strings.Replace(candidate, "/Desktop ", "/Desktop/", 1)
		}
		if candidate != "" {
			out = append(out, candidate)
		}
	}
	for _, match := range tildeMatches {
		if len(match) < 2 {
			continue
		}
		candidate := cleanInspectionPathCandidate(match[1])
		if candidate != "" {
			out = append(out, candidate)
		}
	}
	return out
}

func cleanInspectionPathCandidate(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	candidate = strings.Trim(candidate, "\"'`()[]{}<>")
	candidate = strings.TrimRight(candidate, ".!?")
	lower := strings.ToLower(candidate)
	for _, marker := range []string{
		" and ", " then ", " when ", " where ", " which ", " that ",
		" so ", " but ", " before ", " after ",
	} {
		if idx := strings.Index(lower, marker); idx >= 0 {
			candidate = strings.TrimSpace(candidate[:idx])
			break
		}
	}
	return strings.TrimRight(candidate, ".!?")
}

func qualifyInspectionPath(candidate, workspace string, allowedPaths []string) (string, bool) {
	clean := filepath.Clean(strings.TrimSpace(candidate))
	if strings.HasPrefix(clean, "~") {
		home := currentUserHomeDir()
		if home == "" {
			return "", false
		}
		if clean == "~" {
			clean = home
		} else {
			clean = filepath.Join(home, strings.TrimPrefix(clean, "~/"))
		}
	}
	if clean == "." || clean == "" || !filepath.IsAbs(clean) {
		return "", false
	}
	if workspace != "" && pathWithin(clean, workspace) {
		return clean, true
	}
	for _, allowed := range allowedPaths {
		if pathWithin(clean, allowed) {
			return clean, true
		}
	}
	return "", false
}

func currentUserHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Clean(home)
}

func pathWithin(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == "" || root == "" {
		return false
	}
	if strings.EqualFold(path, root) {
		return true
	}
	rootLower := strings.ToLower(root)
	pathLower := strings.ToLower(path)
	return strings.HasPrefix(pathLower, rootLower+string(filepath.Separator))
}

func uniqueInspectionPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func ResolveFilesystemDestination(content, workspace string, allowedPaths []string, configuredVault string) FilesystemDestinationResolution {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return FilesystemDestinationResolution{}
	}
	if !looksLikeFilesystemAuthoringTurn(lower) {
		return FilesystemDestinationResolution{}
	}

	if strings.Contains(lower, "workspace") && strings.Contains(lower, "vault") {
		root := filepath.Clean(configuredVault)
		if root == "." || root == "" {
			root = filepath.Join(workspace, "Vault")
		}
		if path, ok := qualifyInspectionPath(root, workspace, allowedPaths); ok {
			return FilesystemDestinationResolution{
				ResolvedRoot:       path,
				UseConfiguredVault: samePath(path, configuredVault),
				PromptSummary:      buildDestinationSummary(path, samePath(path, configuredVault)),
			}
		}
	}

	if strings.Contains(lower, "desktop") && strings.Contains(lower, "vault") {
		for _, path := range allowedPaths {
			pathLower := strings.ToLower(path)
			baseLower := strings.ToLower(filepath.Base(path))
			if strings.Contains(pathLower, "/desktop/") && strings.Contains(baseLower, "vault") {
				clean := filepath.Clean(path)
				return FilesystemDestinationResolution{
					ResolvedRoot:       clean,
					UseConfiguredVault: samePath(clean, configuredVault),
					PromptSummary:      buildDestinationSummary(clean, samePath(clean, configuredVault)),
				}
			}
		}
		return FilesystemDestinationResolution{
			ClarificationRequired: true,
			PromptSummary:         "This turn requests authoring into a Desktop vault, but the destination root is still ambiguous. Ask one precise clarifying question requesting the exact destination path before writing.",
		}
	}

	for _, candidate := range extractInspectionPathCandidates(content) {
		if path, ok := qualifyInspectionPath(candidate, workspace, allowedPaths); ok {
			return FilesystemDestinationResolution{
				ResolvedRoot:       path,
				UseConfiguredVault: samePath(path, configuredVault),
				PromptSummary:      buildDestinationSummary(path, samePath(path, configuredVault)),
			}
		}
	}

	return FilesystemDestinationResolution{}
}

func looksLikeFilesystemAuthoringTurn(lower string) bool {
	if lower == "" {
		return false
	}
	actionMarkers := []string{
		"write", "create", "generate", "save", "draft",
	}
	targetMarkers := []string{
		"document", "report", "note", "file", "markdown",
		"vault", "folder", "directory",
	}
	return containsAnyMarker(lower, actionMarkers) && containsAnyMarker(lower, targetMarkers)
}

func looksLikeInspectionBackedArtifactAuthoring(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" || !looksLikeFilesystemAuthoringTurn(lower) {
		return false
	}

	reportMarkers := []string{
		"report", "inventory", "summary", "table", "fields",
		"ordered by", "sorted by", "order them",
	}
	sourceMarkers := []string{
		"project", "projects", "repo", "repos", "repository", "repositories",
		"code folder", "code directory", "workspace", "vault", "folder", "directory",
		"remote origin", "origin repo", "git",
	}
	targetBacked := looksLikeFocusedInspectionTurn(content) ||
		containsAnyMarker(lower, []string{"code folder", "code directory", "workspace", "desktop vault", "desktop"}) ||
		len(extractInspectionPathCandidates(content)) > 0
	return targetBacked && containsAnyMarker(lower, reportMarkers) && containsAnyMarker(lower, sourceMarkers)
}

func looksLikeSourceBackedCodeTask(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return false
	}
	actionMarkers := []string{
		"refactor", "fix", "debug", "modify", "update", "extend",
		"rewrite", "migrate", "optimize", "support hot-reload",
		"add support", "rollback on failure", "emit structured change events",
	}
	targetMarkers := []string{
		"parser", "implementation", "codebase", "repository", "repo",
		"module", "function", "service", "handler", "pipeline",
		"config", "configuration", "current repository", "current codebase",
	}
	return containsAnyMarker(lower, actionMarkers) && containsAnyMarker(lower, targetMarkers)
}

func ResolveSourceCodeTarget(content, currentRoot string, allowedPaths []string) SourceCodeTargetResolution {
	if !looksLikeSourceBackedCodeTask(content) {
		return SourceCodeTargetResolution{}
	}
	if currentRoot == "" {
		return SourceCodeTargetResolution{}
	}
	if _, ok := qualifyInspectionPath(currentRoot, "", allowedPaths); !ok {
		return SourceCodeTargetResolution{}
	}
	root := filepath.Clean(currentRoot)
	return SourceCodeTargetResolution{
		ResolvedRoot:  root,
		PromptSummary: fmt.Sprintf("This turn requests source-backed code work in the current repository root %s. Start there. Use list_directory, glob_files, and read_file to locate authoritative source files before proposing or applying a refactor. Prefer the current repository over broad parent-directory inventory unless direct inspection of this root proves insufficient.", root),
	}
}

func buildDestinationSummary(root string, configuredVault bool) string {
	if configuredVault {
		return fmt.Sprintf("This turn requests authoring into the configured Obsidian vault at %s. Use obsidian_write for vault-relative notes, or write_file with an absolute path under this root when the operator requests a specific absolute destination.", root)
	}
	return fmt.Sprintf("This turn requests authoring into the allowlisted destination root %s. This path is writable. Because it is not the configured default Obsidian vault, use write_file or edit_file with an absolute path under this root. If the operator asked for a new document without naming a file, choose a concise descriptive markdown filename under this root instead of claiming incapacity.", root)
}

func samePath(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
