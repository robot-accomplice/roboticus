package api

import (
	"os"
	"strings"
	"testing"
)

// TestDashboard_EveryNavPageHasRenderer verifies that every page listed in the
// nav and pages array has a corresponding renderXxx function. This catches
// pages that were added to the nav but never implemented.
func TestDashboard_EveryNavPageHasRenderer(t *testing.T) {
	data, err := os.ReadFile("dashboard_spa.html")
	if err != nil {
		t.Skipf("dashboard_spa.html not found: %v", err)
	}
	content := string(data)

	// Extract pages from the pages array.
	pagesStart := strings.Index(content, "var pages = [")
	if pagesStart < 0 {
		t.Fatal("pages array not found in dashboard_spa.html")
	}
	pagesEnd := strings.Index(content[pagesStart:], "]")
	if pagesEnd < 0 {
		t.Fatal("pages array not closed")
	}
	pagesStr := content[pagesStart : pagesStart+pagesEnd+1]

	// Extract page names.
	pages := extractQuotedStrings(pagesStr)
	if len(pages) < 10 {
		t.Fatalf("expected at least 10 pages, got %d: %v", len(pages), pages)
	}

	// For each page, verify a renderXxx function exists.
	for _, page := range pages {
		if page == "recommendations" {
			continue // Recommendations may be a sub-tab, not a top-level renderer.
		}
		renderName := "render" + strings.ToUpper(page[:1]) + page[1:]
		if !strings.Contains(content, renderName+":") && !strings.Contains(content, renderName+" ") {
			t.Errorf("page %q listed in nav but no %s function found in dashboard_spa.html", page, renderName)
		}
	}
}

// TestDashboard_EveryNavItemHasDataPage verifies nav items have data-page attributes.
func TestDashboard_EveryNavItemHasDataPage(t *testing.T) {
	data, err := os.ReadFile("dashboard_spa.html")
	if err != nil {
		t.Skipf("dashboard_spa.html not found: %v", err)
	}
	content := string(data)

	// Count data-page attributes in nav links.
	navCount := strings.Count(content, `data-page="`)
	if navCount < 20 { // Desktop + mobile nav = 2x pages.
		t.Errorf("expected at least 20 data-page nav items, got %d", navCount)
	}
}

// TestDashboard_PageCount verifies the dashboard has the expected number of pages.
func TestDashboard_PageCount(t *testing.T) {
	data, err := os.ReadFile("dashboard_spa.html")
	if err != nil {
		t.Skipf("dashboard_spa.html not found: %v", err)
	}
	content := string(data)

	pages := extractQuotedStrings(content[strings.Index(content, "var pages = ["):])
	// Must have at least 14 pages (matching Rust parity).
	if len(pages) < 14 {
		t.Errorf("dashboard has %d pages, want at least 14", len(pages))
	}
}

func TestDashboard_ObservabilityNarrativeContract(t *testing.T) {
	data, err := os.ReadFile("dashboard_spa.html")
	if err != nil {
		t.Skipf("dashboard_spa.html not found: %v", err)
	}
	content := string(data)

	required := []string{
		"Macro View",
		"Show Detail",
		"trace-only fallback",
		"Canonical diagnostics were not persisted for turn",
		"Conclusion",
		"Health",
		"Learning",
		"Host Resources",
		"Decision Flow",
		"Hover a flow block for quick evidence",
		"Chronological Timeline",
		"Retried same route",
		"Post-attempt guard",
		"Attempt sequence",
		"Routing passes",
		"post-tool follow-up",
		"Copy turn ID",
		"Request-eligible",
		"Ignored as unproven",
		"Recommendation",
		"Replay suppressions",
		"Replay protection blocked",
		"↻",
		"The first attempt succeeded, but",
		"health-poor",
		"fair aggregate",
		"Aggregate of task, envelope, routing, execution, recovery, learning, host resources, and outcome.",
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Errorf("dashboard_spa.html missing observability narrative contract string %q", needle)
		}
	}
}

func TestDashboard_TableLegibilityContract(t *testing.T) {
	data, err := os.ReadFile("dashboard_spa.html")
	if err != nil {
		t.Skipf("dashboard_spa.html not found: %v", err)
	}
	content := string(data)

	required := []string{
		".table-wrap { overflow-x: auto; border-radius: var(--radius); border: 1px solid var(--border-ghost); background: var(--surface-2); }",
		"tbody td { background: var(--surface-2); }",
		"tbody tr:nth-child(even) td { background: var(--surface); }",
		"tbody tr:hover td { background: var(--surface-3); }",
		".table-wrap table { background: transparent; }",
		`.card:not(.table-wrap):not(table):not(thead):not(tbody):not(tr):not(th):not(td), `,
		`[data-theme="`,
		`:not(.table-wrap):not(table):not(thead):not(tbody):not(tr):not(th):not(td)`,
		`.table-wrap td { background-image: none; }`,
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Errorf("dashboard_spa.html missing table legibility contract string %q", needle)
		}
	}
}

func extractQuotedStrings(s string) []string {
	var result []string
	for {
		start := strings.Index(s, "'")
		if start < 0 {
			break
		}
		end := strings.Index(s[start+1:], "'")
		if end < 0 {
			break
		}
		result = append(result, s[start+1:start+1+end])
		s = s[start+1+end+1:]
	}
	return result
}
