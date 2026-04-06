package skills

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Watcher tests
// ---------------------------------------------------------------------------

func TestNewWatcher_MinInterval(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader()

	// Interval below 1s should be clamped to 5s.
	w := NewWatcher(dir, loader, 100*time.Millisecond, nil)
	if w.interval != 5*time.Second {
		t.Errorf("expected interval clamped to 5s, got %v", w.interval)
	}
}

func TestNewWatcher_NormalInterval(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader()

	w := NewWatcher(dir, loader, 10*time.Second, nil)
	if w.interval != 10*time.Second {
		t.Errorf("expected 10s interval, got %v", w.interval)
	}
}

func TestWatcher_InitialLoad(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "s1.md"), []byte("---\nname: s1\n---\nbody"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "s2.yaml"), []byte("name: s2\n"), 0644)

	loader := NewLoader()
	var callbackSkills []*Skill
	var mu sync.Mutex

	w := NewWatcher(dir, loader, 5*time.Second, func(skills []*Skill) {
		mu.Lock()
		callbackSkills = skills
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run in background.
	go w.Run(ctx)

	// Wait for initial load to complete.
	time.Sleep(200 * time.Millisecond)
	cancel()

	skills := w.Skills()
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills after initial load, got %d", len(skills))
	}

	mu.Lock()
	if len(callbackSkills) != 2 {
		t.Errorf("callback should have received 2 skills, got %d", len(callbackSkills))
	}
	mu.Unlock()
}

func TestWatcher_Skills_ReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "s.md"), []byte("---\nname: s\n---\nbody"), 0644)

	loader := NewLoader()
	w := NewWatcher(dir, loader, 5*time.Second, nil)

	// Manually trigger reload.
	w.reload()

	s1 := w.Skills()
	s2 := w.Skills()

	if len(s1) != 1 || len(s2) != 1 {
		t.Fatal("expected 1 skill each")
	}

	// Mutate s1 slice — s2 should be unaffected.
	s1[0] = nil
	if s2[0] == nil {
		t.Error("Skills() should return a copy, not a reference")
	}
}

func TestWatcher_HasChanges_NoChange(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "s.md"), []byte("---\nname: s\n---\nbody"), 0644)

	loader := NewLoader()
	w := NewWatcher(dir, loader, 5*time.Second, nil)
	w.reload()

	if w.hasChanges() {
		t.Error("expected no changes right after reload")
	}
}

func TestWatcher_HasChanges_FileModified(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.md")
	_ = os.WriteFile(path, []byte("---\nname: s\n---\nbody"), 0644)

	loader := NewLoader()
	w := NewWatcher(dir, loader, 5*time.Second, nil)
	w.reload()

	// Modify the file.
	_ = os.WriteFile(path, []byte("---\nname: s-modified\n---\nnew body"), 0644)

	if !w.hasChanges() {
		t.Error("expected changes after file modification")
	}
}

func TestWatcher_HasChanges_FileAdded(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "s.md"), []byte("---\nname: s\n---\nbody"), 0644)

	loader := NewLoader()
	w := NewWatcher(dir, loader, 5*time.Second, nil)
	w.reload()

	// Add a new file.
	_ = os.WriteFile(filepath.Join(dir, "s2.yaml"), []byte("name: s2\n"), 0644)

	if !w.hasChanges() {
		t.Error("expected changes after file addition")
	}
}

func TestWatcher_HasChanges_FileDeleted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.md")
	_ = os.WriteFile(path, []byte("---\nname: s\n---\nbody"), 0644)

	loader := NewLoader()
	w := NewWatcher(dir, loader, 5*time.Second, nil)
	w.reload()

	// Delete the file.
	_ = os.Remove(path)

	if !w.hasChanges() {
		t.Error("expected changes after file deletion")
	}
}

func TestWatcher_HasChanges_NonSkillFileIgnored(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "s.md"), []byte("---\nname: s\n---\nbody"), 0644)

	loader := NewLoader()
	w := NewWatcher(dir, loader, 5*time.Second, nil)
	w.reload()

	// Add a non-skill file — should not trigger changes.
	_ = os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0644)

	if w.hasChanges() {
		t.Error("adding non-skill file should not trigger changes")
	}
}

func TestWatcher_DetectsChangeAndReloads(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "s.md"), []byte("---\nname: original\n---\nbody"), 0644)

	loader := NewLoader()
	var mu sync.Mutex
	reloadCount := 0

	w := NewWatcher(dir, loader, 1*time.Second, func(skills []*Skill) {
		mu.Lock()
		reloadCount++
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx)

	// Wait for initial load.
	time.Sleep(200 * time.Millisecond)

	// Modify the file to trigger a change detection.
	_ = os.WriteFile(filepath.Join(dir, "s.md"), []byte("---\nname: updated\n---\nnew body"), 0644)

	// Wait for the watcher tick and reload.
	time.Sleep(1500 * time.Millisecond)
	cancel()

	mu.Lock()
	// At least 2 reloads: initial + change-triggered.
	if reloadCount < 2 {
		t.Errorf("expected at least 2 reloads, got %d", reloadCount)
	}
	mu.Unlock()

	skills := w.Skills()
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name() != "updated" {
		t.Errorf("expected updated name, got %q", skills[0].Name())
	}
}

func TestWatcher_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader()
	w := NewWatcher(dir, loader, 1*time.Second, nil)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Cancel immediately.
	cancel()

	select {
	case <-done:
		// Good, Run exited.
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

func TestWatcher_NilOnChange(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "s.md"), []byte("---\nname: s\n---\nbody"), 0644)

	loader := NewLoader()
	// nil onChange should not panic.
	w := NewWatcher(dir, loader, 5*time.Second, nil)
	w.reload() // Should not panic.

	skills := w.Skills()
	if len(skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(skills))
	}
}

func TestWatcher_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader()
	w := NewWatcher(dir, loader, 5*time.Second, nil)
	w.reload()

	skills := w.Skills()
	if len(skills) != 0 {
		t.Errorf("expected 0 skills from empty dir, got %d", len(skills))
	}
}

func TestWatcher_HasChanges_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader()
	w := NewWatcher(dir, loader, 5*time.Second, nil)
	w.reload()

	if w.hasChanges() {
		t.Error("empty dir should have no changes")
	}
}

func TestWatcher_HasChanges_NonexistentDir(t *testing.T) {
	dir := "/nonexistent/skills/dir"
	loader := NewLoader()
	w := NewWatcher(dir, loader, 5*time.Second, nil)

	// Manual reload with nonexistent dir — should not panic.
	w.reload()

	skills := w.Skills()
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}

	// hasChanges with nonexistent dir should not panic.
	_ = w.hasChanges()
}

func TestWatcher_Subdirectories(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "root.md"), []byte("---\nname: root\n---\nbody"), 0644)

	sub := filepath.Join(dir, "sub")
	_ = os.MkdirAll(sub, 0755)
	_ = os.WriteFile(filepath.Join(sub, "nested.yaml"), []byte("name: nested\n"), 0644)

	loader := NewLoader()
	w := NewWatcher(dir, loader, 5*time.Second, nil)
	w.reload()

	skills := w.Skills()
	if len(skills) != 2 {
		t.Errorf("expected 2 skills (with subdirectory), got %d", len(skills))
	}
}
