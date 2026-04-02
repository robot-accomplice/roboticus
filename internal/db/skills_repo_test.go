package db

import (
	"context"
	"testing"
)

func makeTestSkill(id, name, kind string) SkillRow {
	return SkillRow{
		ID:             id,
		Name:           name,
		Kind:           kind,
		SourcePath:     "/skills/" + name + ".yaml",
		ContentHash:    "abc123",
		RiskLevel:      "Caution",
		Enabled:        true,
		Version:        "1.0.0",
		Author:         "test",
		RegistrySource: "local",
	}
}

func TestSkillsRepository_UpsertAndGet(t *testing.T) {
	store := testTempStore(t)
	repo := NewSkillsRepository(store)
	ctx := context.Background()

	skill := makeTestSkill("sk-1", "web_search", "structured")
	if err := repo.Upsert(ctx, skill); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := repo.GetByName(ctx, "web_search")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got == nil {
		t.Fatal("expected skill, got nil")
	}
	if got.Kind != "structured" {
		t.Errorf("Kind = %q, want structured", got.Kind)
	}
	if !got.Enabled {
		t.Error("Enabled should be true")
	}
}

func TestSkillsRepository_List(t *testing.T) {
	store := testTempStore(t)
	repo := NewSkillsRepository(store)
	ctx := context.Background()

	_ = repo.Upsert(ctx, makeTestSkill("sk-1", "search", "structured"))
	_ = repo.Upsert(ctx, makeTestSkill("sk-2", "code_gen", "scripted"))
	_ = repo.Upsert(ctx, makeTestSkill("sk-3", "summarize", "instruction"))

	all, err := repo.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("got %d skills, want 3", len(all))
	}

	scripted, err := repo.List(ctx, "scripted")
	if err != nil {
		t.Fatalf("List scripted: %v", err)
	}
	if len(scripted) != 1 {
		t.Errorf("got %d scripted skills, want 1", len(scripted))
	}
}

func TestSkillsRepository_SetEnabledAndUpsertOverwrite(t *testing.T) {
	store := testTempStore(t)
	repo := NewSkillsRepository(store)
	ctx := context.Background()

	_ = repo.Upsert(ctx, makeTestSkill("sk-1", "my_skill", "builtin"))

	if err := repo.SetEnabled(ctx, "my_skill", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	got, _ := repo.GetByName(ctx, "my_skill")
	if got.Enabled {
		t.Error("expected Enabled=false after SetEnabled(false)")
	}

	// Upsert again with updated version — should overwrite.
	updated := makeTestSkill("sk-1", "my_skill", "builtin")
	updated.Version = "2.0.0"
	updated.Enabled = true
	_ = repo.Upsert(ctx, updated)

	got2, _ := repo.GetByName(ctx, "my_skill")
	if got2.Version != "2.0.0" {
		t.Errorf("Version = %q, want 2.0.0", got2.Version)
	}
}

func TestSkillsRepository_Delete(t *testing.T) {
	store := testTempStore(t)
	repo := NewSkillsRepository(store)
	ctx := context.Background()

	_ = repo.Upsert(ctx, makeTestSkill("sk-1", "to_delete", "instruction"))
	if err := repo.Delete(ctx, "to_delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, _ := repo.GetByName(ctx, "to_delete")
	if got != nil {
		t.Error("expected nil after delete")
	}
}
