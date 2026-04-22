package pipeline

import (
	"regexp"
	"strings"

	agenttools "roboticus/internal/agent/tools"
)

type ExpectedArtifactSpec struct {
	ArtifactKind string
	Path         string
	ExactContent string
}

type ArtifactContentMismatch struct {
	Path     string
	Expected string
	Actual   string
	Reason   string
}

type ArtifactConformance struct {
	Expected    []ExpectedArtifactSpec
	Missing     []ExpectedArtifactSpec
	Mismatched  []ArtifactContentMismatch
	MatchedPath []string
}

func (c ArtifactConformance) AllExactSatisfied() bool {
	return len(c.Expected) > 0 && len(c.Missing) == 0 && len(c.Mismatched) == 0
}

func (c ArtifactConformance) HasUnsatisfied() bool {
	return len(c.Missing) > 0 || len(c.Mismatched) > 0
}

type artifactTargetMatch struct {
	Path  string
	Start int
	End   int
}

var artifactTargetPattern = regexp.MustCompile(`(?i)\b(?:[a-z0-9_][a-z0-9_.-]*/)*[a-z0-9_][a-z0-9_.-]*\.(?:md|markdown|txt|json|yaml|yml|toml)\b`)
var artifactContainerDirPattern = regexp.MustCompile(`(?i)\b(?:in|under|within)\s+((?:[a-z0-9_][a-z0-9_.-]*/)+)`)

const artifactContentDirectivePattern = `(?:with\s+content|(?:should\s+)?contain(?:ing)?\s+exactly)`

var exactContentMarkerPattern = regexp.MustCompile(`(?i)` + artifactContentDirectivePattern + `:\s*`)
var inlineExactArtifactPattern = regexp.MustCompile(`(?is)(?:\bfile\s+\d+\s*:\s*)?\b((?:[a-z0-9_][a-z0-9_.-]*/)*[a-z0-9_][a-z0-9_.-]*\.(?:md|markdown|txt|json|yaml|yml|toml))\b\s+(?:that\s+)?` + artifactContentDirectivePattern + `:\s*`)
var ordinalExactContentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)\b(?:the\s+)?first\b.{0,120}?\b` + artifactContentDirectivePattern + `:\s*`),
	regexp.MustCompile(`(?is)\b(?:the\s+)?second\b.{0,120}?\b` + artifactContentDirectivePattern + `:\s*`),
	regexp.MustCompile(`(?is)\b(?:the\s+)?third\b.{0,120}?\b` + artifactContentDirectivePattern + `:\s*`),
}
var postArtifactDirectivePattern = regexp.MustCompile(`(?im)\n(?:after\s+(?:writing|creating)\b|when\s+done\b|once\s+(?:written|created)\b|then\s+tell\b|tell\s+me\b|do\s+not\s+claim\b).*`)
var trailingArtifactBulletPattern = regexp.MustCompile(`(?m)\n[-*•]\s*$`)

func ParseExpectedArtifactSpecs(prompt string) []ExpectedArtifactSpec {
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	baseDir := explicitArtifactContainerDir(prompt)
	targets := orderedArtifactTargets(prompt)
	inlineSpecs := parseInlineExpectedArtifactSpecs(prompt, baseDir)
	if len(inlineSpecs) > 0 && len(inlineSpecs) == len(targets) {
		return inlineSpecs
	}
	ordinalSpecs := parseOrdinalExpectedArtifactSpecs(prompt, baseDir)
	if len(ordinalSpecs) > len(inlineSpecs) {
		return ordinalSpecs
	}
	return inlineSpecs
}

func CompareArtifactConformance(expected []ExpectedArtifactSpec, proofs []agenttools.ArtifactProof) ArtifactConformance {
	conformance := ArtifactConformance{
		Expected: append([]ExpectedArtifactSpec(nil), expected...),
	}
	if len(expected) == 0 {
		return conformance
	}

	byPath := make(map[string]agenttools.ArtifactProof, len(proofs))
	for _, proof := range proofs {
		byPath[normalizeArtifactPath(proof.Path)] = proof
	}

	for _, spec := range expected {
		proof, ok := byPath[normalizeArtifactPath(spec.Path)]
		if !ok {
			conformance.Missing = append(conformance.Missing, spec)
			continue
		}
		if !proof.ExactContentIncluded {
			conformance.Mismatched = append(conformance.Mismatched, ArtifactContentMismatch{
				Path:     spec.Path,
				Expected: spec.ExactContent,
				Actual:   proof.ContentPreview,
				Reason:   "exact_content_unavailable",
			})
			continue
		}
		if normalizeArtifactContent(proof.Content) != normalizeArtifactContent(spec.ExactContent) {
			conformance.Mismatched = append(conformance.Mismatched, ArtifactContentMismatch{
				Path:     spec.Path,
				Expected: spec.ExactContent,
				Actual:   proof.Content,
				Reason:   "content_mismatch",
			})
			continue
		}
		conformance.MatchedPath = append(conformance.MatchedPath, spec.Path)
	}

	return conformance
}

func parseInlineExpectedArtifactSpecs(prompt, baseDir string) []ExpectedArtifactSpec {
	matches := inlineExactArtifactPattern.FindAllStringSubmatchIndex(prompt, -1)
	if len(matches) == 0 {
		return nil
	}

	var specs []ExpectedArtifactSpec
	for i, match := range matches {
		path := resolveExpectedArtifactPath(prompt[match[2]:match[3]], baseDir)
		contentStart := match[1]
		contentEnd := len(prompt)
		if i+1 < len(matches) {
			contentEnd = matches[i+1][0]
		}
		content := trimExpectedArtifactContent(prompt[contentStart:contentEnd])
		if content == "" {
			continue
		}
		specs = append(specs, ExpectedArtifactSpec{
			ArtifactKind: classifyArtifactKind(prompt, path),
			Path:         path,
			ExactContent: content,
		})
	}
	return specs
}

func parseOrdinalExpectedArtifactSpecs(prompt, baseDir string) []ExpectedArtifactSpec {
	targets := orderedArtifactTargets(prompt)
	if len(targets) == 0 {
		return nil
	}

	type ordinalMarker struct {
		Path  string
		Start int
		End   int
	}

	var markers []ordinalMarker
	for i, pattern := range ordinalExactContentPatterns {
		if i >= len(targets) {
			break
		}
		loc := pattern.FindStringIndex(prompt)
		if loc == nil {
			break
		}
		markers = append(markers, ordinalMarker{
			Path:  resolveExpectedArtifactPath(targets[i].Path, baseDir),
			Start: loc[0],
			End:   loc[1],
		})
	}
	if len(markers) == 0 {
		return nil
	}

	var specs []ExpectedArtifactSpec
	for i, marker := range markers {
		end := len(prompt)
		if i+1 < len(markers) {
			end = markers[i+1].Start
		}
		content := trimExpectedArtifactContent(prompt[marker.End:end])
		if content == "" {
			continue
		}
		specs = append(specs, ExpectedArtifactSpec{
			ArtifactKind: classifyArtifactKind(prompt, marker.Path),
			Path:         marker.Path,
			ExactContent: content,
		})
	}
	return specs
}

func orderedArtifactTargets(prompt string) []artifactTargetMatch {
	matches := artifactTargetPattern.FindAllStringIndex(prompt, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	var targets []artifactTargetMatch
	for _, idx := range matches {
		path := prompt[idx[0]:idx[1]]
		key := normalizeArtifactPath(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, artifactTargetMatch{
			Path:  path,
			Start: idx[0],
			End:   idx[1],
		})
	}
	return targets
}

func classifyArtifactKind(prompt, path string) string {
	lower := strings.ToLower(prompt)
	if strings.Contains(lower, "obsidian") || strings.Contains(lower, "vault") {
		return "obsidian_note"
	}
	return "workspace_file"
}

func explicitArtifactContainerDir(prompt string) string {
	match := artifactContainerDirPattern.FindStringSubmatch(prompt)
	if len(match) < 2 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(match[1]), "/")
}

func resolveExpectedArtifactPath(path, baseDir string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || baseDir == "" || strings.Contains(trimmed, "/") {
		return trimmed
	}
	return strings.Trim(baseDir, "/") + "/" + trimmed
}

func trimExpectedArtifactContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if loc := postArtifactDirectivePattern.FindStringIndex(trimmed); loc != nil {
		trimmed = strings.TrimSpace(trimmed[:loc[0]])
	}
	trimmed = trailingArtifactBulletPattern.ReplaceAllString(trimmed, "")
	return strings.TrimSpace(trimmed)
}

func normalizeArtifactPath(path string) string {
	return strings.ToLower(strings.TrimSpace(path))
}

func normalizeArtifactContent(content string) string {
	return strings.ReplaceAll(strings.TrimSpace(content), "\r\n", "\n")
}
