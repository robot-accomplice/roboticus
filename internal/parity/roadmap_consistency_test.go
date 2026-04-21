package parity

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestParityRoadmap_CoversReopenedSystemFindings(t *testing.T) {
	roadmapPath := repoPath("docs", "parity-forensics", "v1.0.7-roadmap.md")
	roadmap := mustRead(t, roadmapPath)

	reopened := reopenedSystemFindings(t)
	for _, findingID := range reopened {
		if !strings.Contains(roadmap, findingID) {
			t.Fatalf("roadmap missing reopened system finding %s", findingID)
		}
	}
}

func TestParityRoadmap_CoversArchitectureOnlyRemainingGaps(t *testing.T) {
	roadmapPath := repoPath("docs", "parity-forensics", "v1.0.7-roadmap.md")
	roadmap := mustRead(t, roadmapPath)

	requiredPhrases := []string{
		"Fusion Layer",
		"LLM-based Reranking",
		"Verifier contradiction resolution",
		"Proof-style evidence audit depth",
		"Semantic read-path cleanup",
	}
	for _, phrase := range requiredPhrases {
		if !strings.Contains(roadmap, phrase) {
			t.Fatalf("roadmap missing architecture-led parity item %q", phrase)
		}
	}
}

func TestParityRoadmap_HasUniqueEntriesAndClosureFields(t *testing.T) {
	roadmapPath := repoPath("docs", "parity-forensics", "v1.0.7-roadmap.md")
	roadmap := mustRead(t, roadmapPath)

	rowRe := regexp.MustCompile(`(?m)^\| (PAR-\d{3}) \| ([^|]+) \| ([^|]+) \| ([^|]+) \| ([^|]+) \| ([^|]+) \| ([^|]+) \| ([^|]+) \|$`)
	matches := rowRe.FindAllStringSubmatch(roadmap, -1)
	if len(matches) == 0 {
		t.Fatal("no parity roadmap entries found")
	}

	seen := map[string]struct{}{}
	allowedStatuses := map[string]struct{}{
		"not started":                  {},
		"in progress":                  {},
		"blocked":                      {},
		"closed":                       {},
		"explicitly deferred by operator": {},
	}

	for _, match := range matches {
		id := strings.TrimSpace(match[1])
		status := strings.TrimSpace(match[4])
		remediation := strings.TrimSpace(match[7])
		proof := strings.TrimSpace(match[8])

		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate roadmap entry %s", id)
		}
		seen[id] = struct{}{}

		if _, ok := allowedStatuses[status]; !ok {
			t.Fatalf("roadmap entry %s has invalid status %q", id, status)
		}
		if remediation == "" {
			t.Fatalf("roadmap entry %s is missing required remediation", id)
		}
		if proof == "" {
			t.Fatalf("roadmap entry %s is missing required proof of closure", id)
		}
	}
}

func reopenedSystemFindings(t *testing.T) []string {
	t.Helper()

	systemDir := repoPath("docs", "parity-forensics", "systems")
	entries, err := os.ReadDir(systemDir)
	if err != nil {
		t.Fatalf("read system dir: %v", err)
	}

	findingRe := regexp.MustCompile(`SYS-\d{2}-\d{3}`)
	var reopened []string

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		content := mustRead(t, filepath.Join(systemDir, entry.Name()))
		for _, line := range strings.Split(content, "\n") {
			if !strings.Contains(line, "SYS-") {
				continue
			}
			if !isReopenedParityLine(line) {
				continue
			}
			if finding := findingRe.FindString(line); finding != "" {
				reopened = append(reopened, finding)
			}
		}
	}

	slices.Sort(reopened)
	return slices.Compact(reopened)
}

func isReopenedParityLine(line string) bool {
	return strings.Contains(line, "Improved, not closed") ||
		strings.Contains(line, "Open, narrower seam") ||
		strings.Contains(line, "Deferred with rationale") ||
		strings.Contains(line, "Deferred with negative evidence") ||
		strings.Contains(line, "reopened via `PAR-")
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func repoPath(parts ...string) string {
	all := append([]string{"..", ".."}, parts...)
	return filepath.Join(all...)
}
