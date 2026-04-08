package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Installer handles skill installation, activation, and filesystem management.
// This logic was extracted from routes/session_detail.go to comply with
// architecture_rules.md §4.1 (connectors do parse→call→format only).
type Installer struct {
	store     *Store
	skillsDir string
}

// NewInstaller creates a skill installer for the given directory and store.
func NewInstaller(store *Store, skillsDir string) *Installer {
	return &Installer{store: store, skillsDir: skillsDir}
}

// Install writes a skill file to disk and registers it in the database.
func (si *Installer) Install(ctx context.Context, name, content string) (string, error) {
	if name == "" || content == "" {
		return "", fmt.Errorf("name and content are required")
	}

	dir := si.skillsDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create skills directory: %w", err)
	}

	path := filepath.Join(dir, name+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write skill file: %w", err)
	}

	// Register in database.
	if si.store != nil {
		id := NewID()
		_, _ = si.store.ExecContext(ctx,
			`INSERT INTO skills (id, name, kind, source_path, content_hash, enabled, version, risk_level)
			 VALUES (?, ?, 'instruction', ?, '', 1, '1.0.0', 'Safe')
			 ON CONFLICT(name) DO UPDATE SET source_path = excluded.source_path`,
			id, name, path)
	}

	return path, nil
}

// Activate enables a skill by name in the database.
func (si *Installer) Activate(ctx context.Context, name string) error {
	if si.store == nil {
		return fmt.Errorf("no store")
	}
	_, err := si.store.ExecContext(ctx,
		`UPDATE skills SET enabled = 1 WHERE name = ?`, name)
	return err
}
