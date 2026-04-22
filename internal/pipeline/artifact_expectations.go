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

type ArtifactPromptContract struct {
	ExpectedOutputs []ExpectedArtifactSpec
	SourceInputs    []string
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
	Unexpected  []string
	MatchedPath []string
}

func (c ArtifactConformance) AllExactSatisfied() bool {
	return len(c.Expected) > 0 && len(c.Missing) == 0 && len(c.Mismatched) == 0
}

func (c ArtifactConformance) HasUnsatisfied() bool {
	return len(c.Missing) > 0 || len(c.Mismatched) > 0 || len(c.Unexpected) > 0
}

type ArtifactClaimConformance struct {
	Claimed          []string
	UnsupportedClaim []string
}

func (c ArtifactClaimConformance) HasUnsupported() bool {
	return len(c.UnsupportedClaim) > 0
}

type SourceArtifactConformance struct {
	ExpectedSources []string
	Unread          []string
	ReadPaths       []string
}

func (c SourceArtifactConformance) HasUnread() bool {
	return len(c.Unread) > 0
}

type artifactTargetMatch struct {
	Path  string
	Start int
	End   int
}

var artifactTargetPattern = regexp.MustCompile(`(?i)\b(?:[a-z0-9_][a-z0-9_.-]*/)*[a-z0-9_][a-z0-9_.-]*\.(?:md|markdown|txt|json|yaml|yml|toml)\b`)
var artifactContainerDirPattern = regexp.MustCompile(`(?i)\b(?:in|under|within)\s+((?:[a-z0-9_][a-z0-9_.-]*/)+)`)
var sourceArtifactRefPattern = regexp.MustCompile(`(?i)\b(?:read|inspect|open|parse|use|using|from)\s+((?:[a-z0-9_][a-z0-9_.-]*/)*[a-z0-9_][a-z0-9_.-]*\.(?:md|markdown|txt|json|yaml|yml|toml))\b`)

const artifactContentDirectivePattern = `(?:with\s+content|(?:should\s+)?contain(?:ing)?\s+exactly)`

var inlineExactArtifactPattern = regexp.MustCompile(`(?is)(?:\bfile\s+\d+\s*:\s*)?\b((?:[a-z0-9_][a-z0-9_.-]*/)*[a-z0-9_][a-z0-9_.-]*\.(?:md|markdown|txt|json|yaml|yml|toml))\b\s+(?:that\s+)?` + artifactContentDirectivePattern + `\s*:?\s*`)
var ordinalExactContentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)\b(?:the\s+)?first\b.{0,120}?\b` + artifactContentDirectivePattern + `\s*:?\s*`),
	regexp.MustCompile(`(?is)\b(?:the\s+)?second\b.{0,120}?\b` + artifactContentDirectivePattern + `\s*:?\s*`),
	regexp.MustCompile(`(?is)\b(?:the\s+)?third\b.{0,120}?\b` + artifactContentDirectivePattern + `\s*:?\s*`),
}
var postArtifactDirectivePattern = regexp.MustCompile(`(?im)\n(?:after\s+(?:writing|creating)\b|when\s+done\b|once\s+(?:written|created)\b|then\s+tell\b|tell\s+me\b|do\s+not\s+claim\b).*`)
var trailingArtifactBulletPattern = regexp.MustCompile(`(?m)\n[-*•]\s*$`)
var intraArtifactConnectorPattern = regexp.MustCompile(`(?is)\s+(?:and|then)\s+(?:create|write)\s*$`)

func ParseExpectedArtifactSpecs(prompt string) []ExpectedArtifactSpec {
	return ParseArtifactPromptContract(prompt).ExpectedOutputs
}

func ParseArtifactPromptContract(prompt string) ArtifactPromptContract {
	if strings.TrimSpace(prompt) == "" {
		return ArtifactPromptContract{}
	}
	baseDir := explicitArtifactContainerDir(prompt)
	targets := orderedArtifactTargets(prompt)
	inlineSpecs := parseInlineExpectedArtifactSpecs(prompt, baseDir)
	expected := inlineSpecs
	if len(inlineSpecs) > 0 && len(inlineSpecs) == len(targets) {
		expected = inlineSpecs
		return ArtifactPromptContract{
			ExpectedOutputs: expected,
			SourceInputs:    classifySourceArtifactRefs(prompt, expected),
		}
	}
	ordinalSpecs := parseOrdinalExpectedArtifactSpecs(prompt, baseDir)
	if len(ordinalSpecs) > len(inlineSpecs) {
		expected = ordinalSpecs
	}
	return ArtifactPromptContract{
		ExpectedOutputs: expected,
		SourceInputs:    classifySourceArtifactRefs(prompt, expected),
	}
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
	matchedProofs := make(map[string]struct{}, len(proofs))

	for _, spec := range expected {
		key := normalizeArtifactPath(spec.Path)
		proof, ok := byPath[key]
		if !ok {
			conformance.Missing = append(conformance.Missing, spec)
			continue
		}
		matchedProofs[key] = struct{}{}
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

	expectedSet := make(map[string]struct{}, len(expected))
	expectedBaseSet := make(map[string]struct{}, len(expected))
	for _, spec := range expected {
		recordArtifactSupport(spec.Path, expectedSet, expectedBaseSet)
	}
	for _, proof := range proofs {
		key := normalizeArtifactPath(proof.Path)
		if _, ok := matchedProofs[key]; ok {
			continue
		}
		base := artifactBaseName(key)
		if _, ok := expectedSet[key]; ok {
			continue
		}
		if base != "" {
			if _, ok := expectedBaseSet[base]; ok {
				continue
			}
		}
		conformance.Unexpected = append(conformance.Unexpected, proof.Path)
	}

	return conformance
}

func CompareArtifactClaims(content string, expected []ExpectedArtifactSpec, sourceInputs []string, proofs []agenttools.ArtifactProof, inspection []agenttools.InspectionProof, prompt string) ArtifactClaimConformance {
	conformance := ArtifactClaimConformance{}
	if shouldSkipArtifactClaimChecks(expected, proofs, inspection, prompt) {
		return conformance
	}
	claims := extractClaimedArtifactPaths(content)
	conformance.Claimed = append([]string(nil), claims...)
	if len(claims) == 0 {
		return conformance
	}

	supported := make(map[string]struct{}, len(expected)+len(proofs))
	supportedBase := make(map[string]struct{}, len(expected)+len(proofs))
	for _, spec := range expected {
		recordArtifactSupport(spec.Path, supported, supportedBase)
	}
	for _, source := range sourceInputs {
		recordArtifactSupport(source, supported, supportedBase)
	}
	for _, proof := range proofs {
		recordArtifactSupport(proof.Path, supported, supportedBase)
	}

	for _, claim := range claims {
		key := normalizeArtifactPath(claim)
		base := artifactBaseName(key)
		if _, ok := supported[key]; ok {
			continue
		}
		if base != "" {
			if _, ok := supportedBase[base]; ok {
				continue
			}
		}
		conformance.UnsupportedClaim = append(conformance.UnsupportedClaim, claim)
	}

	return conformance
}

func shouldSkipArtifactClaimChecks(expected []ExpectedArtifactSpec, proofs []agenttools.ArtifactProof, inspection []agenttools.InspectionProof, prompt string) bool {
	if len(expected) > 0 {
		return false
	}
	if len(proofs) > 0 {
		return false
	}
	if len(sourceArtifactRefPattern.FindAllStringSubmatch(prompt, -1)) > 0 {
		return false
	}
	if len(inspection) > 0 {
		return true
	}
	return true
}

func classifySourceArtifactRefs(prompt string, expected []ExpectedArtifactSpec) []string {
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	expectedSet := make(map[string]struct{}, len(expected))
	expectedBaseSet := make(map[string]struct{}, len(expected))
	for _, spec := range expected {
		recordArtifactSupport(spec.Path, expectedSet, expectedBaseSet)
	}
	var refs []string
	seen := make(map[string]struct{})
	for _, match := range sourceArtifactRefPattern.FindAllStringSubmatch(prompt, -1) {
		if len(match) < 2 {
			continue
		}
		path := strings.TrimSpace(match[1])
		key := normalizeArtifactPath(path)
		if key == "" {
			continue
		}
		base := artifactBaseName(key)
		if _, ok := expectedSet[key]; ok {
			continue
		}
		if base != "" {
			if _, ok := expectedBaseSet[base]; ok {
				continue
			}
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, path)
	}
	return refs
}

func CompareSourceArtifactConformance(sourceInputs []string, reads []agenttools.ArtifactReadProof) SourceArtifactConformance {
	conformance := SourceArtifactConformance{
		ExpectedSources: append([]string(nil), sourceInputs...),
	}
	if len(sourceInputs) == 0 {
		return conformance
	}
	readSet := make(map[string]struct{}, len(reads))
	readBaseSet := make(map[string]struct{}, len(reads))
	for _, read := range reads {
		recordArtifactSupport(read.Path, readSet, readBaseSet)
	}
	for _, source := range sourceInputs {
		key := normalizeArtifactPath(source)
		base := artifactBaseName(key)
		if _, ok := readSet[key]; ok {
			conformance.ReadPaths = append(conformance.ReadPaths, source)
			continue
		}
		if base != "" {
			if _, ok := readBaseSet[base]; ok {
				conformance.ReadPaths = append(conformance.ReadPaths, source)
				continue
			}
		}
		conformance.Unread = append(conformance.Unread, source)
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

func extractClaimedArtifactPaths(content string) []string {
	matches := artifactTargetPattern.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	var claims []string
	for _, idx := range matches {
		path := strings.TrimSpace(content[idx[0]:idx[1]])
		key := normalizeArtifactPath(path)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		claims = append(claims, path)
	}
	return claims
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
	trimmed = intraArtifactConnectorPattern.ReplaceAllString(trimmed, "")
	return strings.TrimSpace(trimmed)
}

func normalizeArtifactPath(path string) string {
	return strings.ToLower(strings.TrimSpace(path))
}

func artifactBaseName(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 && idx+1 < len(trimmed) {
		return trimmed[idx+1:]
	}
	return trimmed
}

func recordArtifactSupport(path string, supported, supportedBase map[string]struct{}) {
	key := normalizeArtifactPath(path)
	if key == "" {
		return
	}
	supported[key] = struct{}{}
	base := artifactBaseName(key)
	if base != "" {
		supportedBase[base] = struct{}{}
	}
}

func normalizeArtifactContent(content string) string {
	return strings.ReplaceAll(strings.TrimSpace(content), "\r\n", "\n")
}
