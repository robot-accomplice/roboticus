package db

import (
	"context"
	"fmt"
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
	repo := NewSkillCompositionRepository(si.store, si.skillsDir)
	_, spec, err := repo.Upsert(ctx, SkillCompositionSpec{
		Name:           name,
		Kind:           "instruction",
		Content:        content,
		Enabled:        true,
		Version:        "1.0.0",
		Author:         "catalog",
		RegistrySource: "catalog",
		RiskLevel:      "Caution",
	})
	if err != nil {
		return "", err
	}
	return spec.SourcePath, nil
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
