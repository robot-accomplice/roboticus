package skills

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Watcher monitors a skills directory for changes and triggers reloads.
type Watcher struct {
	dir      string
	loader   *Loader
	interval time.Duration

	mu     sync.RWMutex
	skills []*Skill
	hashes map[string]string // path → hash

	onChange func([]*Skill) // callback when skills change
}

// NewWatcher creates a skill watcher that polls for changes.
func NewWatcher(dir string, loader *Loader, interval time.Duration, onChange func([]*Skill)) *Watcher {
	if interval < time.Second {
		interval = 5 * time.Second
	}
	return &Watcher{
		dir:      dir,
		loader:   loader,
		interval: interval,
		hashes:   make(map[string]string),
		onChange: onChange,
	}
}

// Skills returns the current set of loaded skills.
func (sw *Watcher) Skills() []*Skill {
	sw.mu.RLock()
	defer sw.mu.RUnlock()
	result := make([]*Skill, len(sw.skills))
	copy(result, sw.skills)
	return result
}

// Run starts the watcher loop. It performs an initial load, then polls for changes.
// Blocks until ctx is cancelled.
func (sw *Watcher) Run(ctx context.Context) {
	// Initial load.
	sw.reload()

	ticker := time.NewTicker(sw.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if sw.hasChanges() {
				log.Info().Str("dir", sw.dir).Msg("skill changes detected, reloading")
				sw.reload()
			}
		}
	}
}

// reload loads all skills from the directory and updates the cache.
func (sw *Watcher) reload() {
	skills := sw.loader.LoadFromDir(sw.dir)

	// Update hash cache.
	newHashes := make(map[string]string, len(skills))
	for _, s := range skills {
		newHashes[s.SourcePath] = s.Hash
	}

	sw.mu.Lock()
	sw.skills = skills
	sw.hashes = newHashes
	sw.mu.Unlock()

	log.Info().Int("count", len(skills)).Str("dir", sw.dir).Msg("skills loaded")

	if sw.onChange != nil {
		sw.onChange(skills)
	}
}

// hasChanges checks if any skill files have been added, removed, or modified.
func (sw *Watcher) hasChanges() bool {
	sw.mu.RLock()
	oldHashes := sw.hashes
	sw.mu.RUnlock()

	currentHashes := make(map[string]string)
	_ = filepath.WalkDir(sw.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".md" && ext != ".toml" && ext != ".yaml" && ext != ".yml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		currentHashes[path] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})

	// Check for additions or modifications.
	if len(currentHashes) != len(oldHashes) {
		return true
	}
	for path, hash := range currentHashes {
		if oldHash, ok := oldHashes[path]; !ok || oldHash != hash {
			return true
		}
	}
	// Check for deletions.
	for path := range oldHashes {
		if _, ok := currentHashes[path]; !ok {
			return true
		}
	}
	return false
}
