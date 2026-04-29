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

func TestDashboard_PromptPerformanceTuningContract(t *testing.T) {
	data, err := os.ReadFile("dashboard_spa.html")
	if err != nil {
		t.Skipf("dashboard_spa.html not found: %v", err)
	}
	content := string(data)

	required := []string{
		`data-eff-tab="tuning"`,
		`>Tuning</button>`,
		`if (effTab === 'tuning')`,
		`Quick Optimizations`,
		`Semantic caching is already enabled`,
		`No concrete action returned by analysis`,
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Errorf("dashboard_spa.html missing prompt performance tuning contract string %q", needle)
		}
	}
}

func TestDashboard_WorkspaceOrchestratorShelterContract(t *testing.T) {
	data, err := os.ReadFile("dashboard_spa.html")
	if err != nil {
		t.Skipf("dashboard_spa.html not found: %v", err)
	}
	content := string(data)

	if strings.Count(content, `!== 'orchestrator'`) < 3 {
		t.Fatal("workspace shelter logic must exclude orchestrators from idle-agent hiding and badge counts")
	}
	required := []string{
		`stateKey === 'sleeping' ? 'sleeping'`,
		`var isSleeping = isAgent && String(bot.state || '').toLowerCase() === 'sleeping'`,
		`ctx.fillText('Z'`,
		`eyeY - 1`,
		`App._wsSubscribe(['workspace']);`,
		`'workspace.snapshot': function(data)`,
		`data-agents-tab="workspace">Workspace</button>`,
		`agents-workspace-tab`,
		`content.style.padding = '1.5rem'`,
		`hash === 'workspace'`,
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Errorf("dashboard_spa.html missing sleeping orchestrator contract string %q", needle)
		}
	}
	if strings.Contains(content, `renderWorkspace: function() {
      return api('/api/workspace/state')`) {
		t.Fatal("workspace render must be websocket-driven, not direct API-driven")
	}
	if strings.Contains(content, `workspaceCanvasActive ? '0 0 0 0'`) {
		t.Fatal("workspace tab must preserve the normal Agents content padding")
	}
}

func TestDashboard_SessionsOwnContextTab(t *testing.T) {
	data, err := os.ReadFile("dashboard_spa.html")
	if err != nil {
		t.Skipf("dashboard_spa.html not found: %v", err)
	}
	content := string(data)

	required := []string{
		`data-sessions-tab="list">List</button>`,
		`data-sessions-tab="context">Context</button>`,
		`hash === 'context'`,
		`App._sessionsTab = 'context'`,
		`var sessions = data.sessions || [];`,
		`(s.turn_count || 0) > 0`,
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Errorf("dashboard_spa.html missing sessions/context contract string %q", needle)
		}
	}
	if strings.Contains(content, `href="#context" data-page="context"`) {
		t.Fatal("Context must not be exposed as a top-level dashboard navigation item")
	}
}

func TestDashboard_ContextFootprintGraphContract(t *testing.T) {
	data, err := os.ReadFile("dashboard_spa.html")
	if err != nil {
		t.Skipf("dashboard_spa.html not found: %v", err)
	}
	content := string(data)

	required := []string{
		`function renderContextFootprint(ctx, compact)`,
		`REQUEST CONTEXT FOOTPRINT`,
		`ctx-k-tools`,
		`.ctx-turn-detail .ctx-bar`,
		`ctx-k-current_user`,
		`ctx-k-unused`,
		`data-footprint-target`,
		`ctx-footprint-shell`,
		`ctx-bar ctx-bar-vertical`,
		`ctx-footprint-pane`,
		`ctx-slice-label`,
		`ctx-detail-metric`,
		`ctx-footprint-detail`,
		`seg.details`,
		`zero-slice`,
		`ctx-slice-compact`,
		`ctx-slice-tiny`,
		`data-slice-label`,
		`style="flex-basis:' + displayPct + '%;color:var(--text)"`,
		`details.length > 0 ? ' · ' + details.length + ' items' : ''`,
		`classList.toggle('active', el === ctxSegment)`,
		`No item-level details recorded for this segment.`,
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Errorf("dashboard_spa.html missing context footprint graph contract string %q", needle)
		}
	}
}

func TestDashboard_ContextSessionInventoryContract(t *testing.T) {
	data, err := os.ReadFile("dashboard_spa.html")
	if err != nil {
		t.Skipf("dashboard_spa.html not found: %v", err)
	}
	content := string(data)

	required := []string{
		`var turnCount = s.turn_count || 0;`,
		`var messageCount = s.message_count || 0;`,
		`var traceCount = s.trace_count || 0;`,
		`var snapshotCount = s.snapshot_count || 0;`,
		`var evidence = traceCount + ' traces / ' + snapshotCount + ' snapshots';`,
		`var latest = s.last_activity_at || s.updated_at || s.created_at || '';`,
		`<th>Volume</th><th>Evidence</th><th>Tokens</th><th>Cost</th><th>Latest Activity</th>`,
		`+ turnCount + ' turns</span>`,
		`+ messageCount + ' msgs</span>`,
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Errorf("dashboard_spa.html missing context session inventory contract string %q", needle)
		}
	}
}

func TestDashboard_TurnAnalyzeUsesContextTurnIDContract(t *testing.T) {
	data, err := os.ReadFile("dashboard_spa.html")
	if err != nil {
		t.Skipf("dashboard_spa.html not found: %v", err)
	}
	content := string(data)

	required := []string{
		`data-analyze-turn="' + esc(self._ctxActiveTurn.id) + '"`,
		`api('/api/turns/' + encodeURIComponent(turnId) + '/analyze', { method: 'POST' })`,
		`var found = App._ctxTurns.find(function(t) { return t.id === turnId; });`,
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Errorf("dashboard_spa.html missing turn analyze ID contract string %q", needle)
		}
	}
}

func TestDashboard_TurnAnalyzeRendersCanonicalAnalysisPayload(t *testing.T) {
	data, err := os.ReadFile("dashboard_spa.html")
	if err != nil {
		t.Skipf("dashboard_spa.html not found: %v", err)
	}
	content := string(data)

	required := []string{
		`var analysisText = r.analysis || r.summary || '';`,
		`esc(analysisText || 'No analysis returned.')`,
		`var tips = r.heuristic_tips || r.tips || [];`,
		`No separate heuristic tips.`,
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Errorf("dashboard_spa.html missing canonical turn analysis renderer string %q", needle)
		}
	}
	if strings.Contains(content, `r.summary || 'No summary'`) {
		t.Fatal("turn analysis renderer must not prefer obsolete summary field")
	}
	if strings.Contains(content, `var tips = r.tips || []`) {
		t.Fatal("turn analysis renderer must prefer heuristic_tips from the canonical route contract")
	}
}

func TestDashboard_RuntimeVersionVisibleInHeaderAndFooter(t *testing.T) {
	data, err := os.ReadFile("dashboard_spa.html")
	if err != nil {
		t.Skipf("dashboard_spa.html not found: %v", err)
	}
	content := string(data)

	required := []string{
		`<div class="sidebar-footer">v<span id="version">&mdash;</span></div>`,
		`<span>v<span id="runtime-version">&mdash;</span></span>`,
		`var runtimeVersion = document.getElementById('runtime-version');`,
		`if (runtimeVersion) runtimeVersion.textContent = healthData.version || 'unknown';`,
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Errorf("dashboard_spa.html missing runtime version chrome contract string %q", needle)
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
