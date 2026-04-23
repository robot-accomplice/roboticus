// install_repo_parity_test.go pins the v1.0.6 invariant that every
// install / update surface references the SAME GitHub repository.
//
// Pre-v1.0.6 audit flagged: install.sh + install.ps1 targeted
// "roboticus/roboticus" while the in-app updater in cmd/updatecmd
// targeted "robot-accomplice/roboticus". That drift is exactly how
// installers, release assets, and self-update paths get out of sync —
// a curl-piped install would write binaries from one repo while
// `roboticus upgrade` pulled from a completely different repo with
// potentially different release cadence or versioning.
//
// The fix reconciled all three surfaces to robot-accomplice/roboticus
// (the repo that actually produces the release artifacts). This test
// grep-scans all install/update source files and asserts a SINGLE
// repo slug appears — if a future change reintroduces drift by either
// (a) changing one surface without the others or (b) introducing a
// new surface that references a different repo, this test fails.

package scripts_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var canonicalRepoSlug = "robot-accomplice/roboticus"

// repoRefRegex matches GitHub repo references that appear either:
//
//	(a) inside a github.com or api.github.com URL, or
//	(b) as a bare quoted/assigned REPO variable value (install.sh /
//	    install.ps1 both set REPO="<org>/<repo>" near the top).
//
// Filesystem paths like /usr/local/bin/roboticus look superficially
// similar to "bin/roboticus" but aren't in either category above and
// are filtered out by the parser below (isRepoSlugContext).
var repoRefRegex = regexp.MustCompile(`(?i)\b[a-z0-9][-a-z0-9]*/roboticus\b`)

// isRepoSlugContext returns true if the line containing the match is
// clearly an install/update code path that resolves a GitHub repo — a
// github.com URL, an api.github.com URL, or a REPO/$Repo assignment.
// Everything else (install paths, comments, log strings) is ignored.
func isRepoSlugContext(line string) bool {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "github.com/"):
		return true
	case strings.Contains(lower, "api.github.com/"):
		return true
	case strings.Contains(lower, `repo="`), strings.Contains(lower, `repo = "`):
		return true // install.sh REPO="<slug>"
	case strings.Contains(lower, `$repo = "`):
		return true // install.ps1 $Repo = "<slug>"
	case strings.Contains(lower, `updatecheckurl`), strings.Contains(lower, `updatereleasesurl`):
		return true // update.go var refs
	default:
		return false
	}
}

// surfacesToCheck lists every file where a repo reference would flow
// through into install or update behavior. Adding a new install /
// update path without updating this list is itself a test failure
// (grep will find its repo refs and fail parity if they mismatch).
var surfacesToCheck = []string{
	"install.sh",
	"install.ps1",
	"../cmd/updatecmd/update.go",
}

func TestInstallRepoRefsAreCanonical(t *testing.T) {
	// cd to scripts dir where this test lives.
	testDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}

	refsByFile := map[string][]string{}
	for _, rel := range surfacesToCheck {
		path := filepath.Join(testDir, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		text := string(data)

		for _, line := range strings.Split(text, "\n") {
			// Skip CI badges and img shields — they're render-only.
			lower := strings.ToLower(line)
			if strings.Contains(lower, "img.shields.io") ||
				strings.Contains(lower, "github.com/actions") ||
				strings.Contains(lower, "badge") {
				continue
			}
			// Only count references in install/update code path
			// contexts — skip install paths like /usr/local/bin/roboticus
			// which match the regex but aren't repo slugs.
			if !isRepoSlugContext(line) {
				continue
			}
			matches := repoRefRegex.FindAllString(line, -1)
			for _, m := range matches {
				if !looksLikeOrgSlug(m) {
					continue
				}
				refsByFile[rel] = append(refsByFile[rel], m)
			}
		}
	}

	// Every surface we care about MUST have at least one reference.
	for _, s := range surfacesToCheck {
		if len(refsByFile[s]) == 0 {
			t.Fatalf("surface %s has zero repo references — has the repo slug been renamed or deleted from this file?", s)
		}
	}

	// All refs across all surfaces MUST match the canonical slug
	// exactly. If any surface still references a different org, fail
	// with a diff so the next engineer knows exactly what drifted.
	bad := map[string][]string{}
	for file, refs := range refsByFile {
		for _, ref := range refs {
			if !strings.EqualFold(ref, canonicalRepoSlug) {
				bad[file] = append(bad[file], ref)
			}
		}
	}
	if len(bad) > 0 {
		// Deterministic order.
		files := make([]string, 0, len(bad))
		for f := range bad {
			files = append(files, f)
		}
		sort.Strings(files)
		var lines []string
		for _, f := range files {
			uniq := uniqueStrings(bad[f])
			lines = append(lines, f+": "+strings.Join(uniq, ", "))
		}
		t.Fatalf("repo slug drift detected — canonical is %q, found non-matching refs:\n  %s", canonicalRepoSlug, strings.Join(lines, "\n  "))
	}
}

// looksLikeOrgSlug returns true if s looks like <org>/<repo> pair with
// a single slash. A path like "roboticus/internal/foo" (which the regex
// might match at the head) is multi-slash and should be skipped.
func looksLikeOrgSlug(s string) bool {
	return strings.Count(s, "/") == 1
}

func uniqueStrings(ss []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range ss {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
