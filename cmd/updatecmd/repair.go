package updatecmd

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type RepairAction struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type RepairSummary struct {
	Actions []RepairAction `json:"actions"`
}

func (s *RepairSummary) add(name, status, detail string) {
	s.Actions = append(s.Actions, RepairAction{Name: name, Status: status, Detail: detail})
}

// RunInstallCleanup runs safe, idempotent repair primitives shared by upgrade
// and mechanic. It intentionally avoids provider/skill downloads; those remain
// explicit update operations.
func RunInstallCleanup(ctx context.Context, configPath, currentVersion string) (RepairSummary, error) {
	_ = ctx // Reserved for future cleanup operations that need cancellation.
	var summary RepairSummary

	if _, repaired, err := reconcileUpdateState(configPath, currentVersion); err != nil {
		summary.add("updater_state", "failed", err.Error())
		return summary, err
	} else if repaired {
		summary.add("updater_state", "repaired", "reconciled update_state.json from local install artifacts")
	} else {
		summary.add("updater_state", "skipped", "already reconciled")
	}

	execPath, err := os.Executable()
	if err != nil {
		summary.add("stale_sidecars", "needs_manual_action", "unable to determine executable path: "+err.Error())
		return summary, nil
	}
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}
	cleanupStaleSidecars(execPath, &summary)
	return summary, nil
}

func cleanupStaleSidecars(execPath string, summary *RepairSummary) {
	pattern := execPath + ".old*"
	matches, err := filepath.Glob(pattern)
	if err != nil {
		summary.add("stale_sidecars", "failed", err.Error())
		return
	}
	if len(matches) == 0 {
		summary.add("stale_sidecars", "skipped", "no stale updater sidecars found")
		return
	}

	removed := 0
	blocked := 0
	for _, path := range matches {
		if !isSidecarForExecutable(execPath, path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			blocked++
			continue
		}
		if info.IsDir() {
			blocked++
			continue
		}
		if runtime.GOOS == "windows" {
			// A just-replaced running binary can remain locked on Windows. Try
			// removal anyway; a sharing violation becomes manual-action evidence.
		}
		if err := os.Remove(path); err != nil {
			blocked++
			continue
		}
		removed++
	}

	switch {
	case removed > 0 && blocked == 0:
		summary.add("stale_sidecars", "repaired", "removed stale updater sidecars")
	case removed > 0:
		summary.add("stale_sidecars", "needs_manual_action", "removed some sidecars; one or more remain locked or invalid")
	case blocked > 0:
		summary.add("stale_sidecars", "needs_manual_action", "sidecars exist but could not be removed safely")
	default:
		summary.add("stale_sidecars", "skipped", "no stale updater sidecars found")
	}
}

func isSidecarForExecutable(execPath, sidecar string) bool {
	base := execPath + ".old"
	return sidecar == base || strings.HasPrefix(sidecar, base+"-")
}
