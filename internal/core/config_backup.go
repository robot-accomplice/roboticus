package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BackupConfigFile creates a timestamped backup of a config file.
// Backups are stored in a backups/ subdirectory next to the config file.
// Old backups are pruned by count and age. Returns the backup path, or
// empty string if the source file doesn't exist.
//
// Matches Rust's backup_config_file in config_utils.rs.
func BackupConfigFile(path string, maxCount int, maxAgeDays int) (string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", nil
	}

	parent := filepath.Dir(path)
	backupDir := filepath.Join(parent, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}

	fileName := filepath.Base(path)
	stamp := time.Now().UTC().Format("20060102-150405")
	backupName := fmt.Sprintf("%s.bak.%s", fileName, stamp)
	backupPath := filepath.Join(backupDir, backupName)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read config: %w", err)
	}
	if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}

	// Prune old backups.
	PruneOldBackups(backupDir, fileName, maxCount, maxAgeDays)

	return backupPath, nil
}

// PruneOldBackups removes old config backups by count and age.
// Matches Rust's prune_old_backups.
func PruneOldBackups(backupDir, prefix string, maxCount, maxAgeDays int) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	var backups []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, prefix+".bak.") {
			backups = append(backups, filepath.Join(backupDir, name))
		}
	}
	sort.Strings(backups) // Oldest first (timestamp in name sorts correctly).

	// Age-based pruning.
	if maxAgeDays > 0 {
		cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)
		var kept []string
		for _, p := range backups {
			info, err := os.Stat(p)
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				_ = os.Remove(p)
			} else {
				kept = append(kept, p)
			}
		}
		backups = kept
	}

	// Count-based pruning: keep only the newest maxCount.
	if maxCount > 0 && len(backups) > maxCount {
		toRemove := backups[:len(backups)-maxCount]
		for _, p := range toRemove {
			_ = os.Remove(p)
		}
	}
}
