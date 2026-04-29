package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/agent/skills"
	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/pipeline"
)

// skillInventory owns the live skill truth for the daemon. Files provide the
// executable artifact; the DB provides governance and metadata; the matcher and
// prompt consume the reconciled runtime set.
type skillInventory struct {
	cfg     *core.Config
	store   *db.Store
	loader  *skills.Loader
	matcher *skills.Matcher

	mu     sync.RWMutex
	loaded []*skills.Skill
}

func newSkillInventory(cfg *core.Config, store *db.Store, loader *skills.Loader, matcher *skills.Matcher) *skillInventory {
	return &skillInventory{cfg: cfg, store: store, loader: loader, matcher: matcher}
}

func (i *skillInventory) Reload(ctx context.Context) ([]*skills.Skill, error) {
	if i == nil || i.loader == nil {
		return nil, nil
	}

	var loaded []*skills.Skill
	for _, dir := range i.skillDirs() {
		loaded = mergeLoadedSkills(loaded, i.loader.LoadFromDir(dir))
	}
	loaded = mergeLoadedSkills(loaded, i.loader.LoadFromPaths(pipeline.EnabledSkillSourcePathsFromDB(i.store)))
	loaded = dedupeLoadedSkillsByName(loaded)

	enabledByName, err := i.enabledPolicy(ctx)
	if err != nil {
		return nil, err
	}

	repo := db.NewSkillsRepository(i.store)
	reconciled := make([]*skills.Skill, 0, len(loaded))
	for _, skill := range loaded {
		if skill == nil || strings.TrimSpace(skill.Name()) == "" {
			continue
		}
		if i.store != nil {
			if err := repo.UpsertDiscovered(ctx, discoveredSkillRow(skill)); err != nil {
				log.Warn().Err(err).Str("skill", skill.Name()).Msg("skill inventory: failed to upsert discovered skill")
			}
		}
		if enabled, known := enabledByName[strings.ToLower(skill.Name())]; known && !enabled {
			continue
		}
		reconciled = append(reconciled, skill)
	}

	i.mu.Lock()
	i.loaded = append([]*skills.Skill(nil), reconciled...)
	i.mu.Unlock()
	if i.matcher != nil {
		i.matcher.SetSkills(reconciled)
	}

	log.Info().Int("count", len(reconciled)).Msg("runtime skill inventory refreshed")
	return reconciled, nil
}

func (i *skillInventory) StartWatchers(ctx context.Context, wg *sync.WaitGroup) {
	if i == nil || i.cfg == nil || (!i.cfg.Skills.WatchMode && !i.cfg.Skills.HotReload) {
		return
	}
	for _, dir := range i.skillDirs() {
		watcher := skills.NewWatcher(dir, i.loader, 5*time.Second, func(_ []*skills.Skill) {
			if _, err := i.Reload(context.Background()); err != nil {
				log.Warn().Err(err).Msg("skill inventory: hot reload failed")
			}
		})
		if wg != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				watcher.Run(ctx)
			}()
			continue
		}
		go watcher.Run(ctx)
	}
}

func (i *skillInventory) Names() []string {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	names := make([]string, 0, len(i.loaded))
	for _, skill := range i.loaded {
		if skill != nil && strings.TrimSpace(skill.Name()) != "" {
			names = append(names, skill.Name())
		}
	}
	return names
}

func (i *skillInventory) skillDirs() []string {
	if i == nil || i.cfg == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var dirs []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			return
		}
		seen[clean] = struct{}{}
		dirs = append(dirs, clean)
	}
	configured := strings.TrimSpace(i.cfg.Skills.Directory)
	add(configured)
	if configured == "" || sameCleanPath(configured, i.cfg.Agent.Workspace) {
		add(filepath.Join(core.ConfigDir(), "skills"))
	}
	return dirs
}

func sameCleanPath(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	return a != "" && b != "" && filepath.Clean(a) == filepath.Clean(b)
}

func (i *skillInventory) enabledPolicy(ctx context.Context) (map[string]bool, error) {
	policy := make(map[string]bool)
	if i == nil || i.store == nil {
		return policy, nil
	}
	rows, err := i.store.QueryContext(ctx, `SELECT name, enabled FROM skills`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		var enabled bool
		if err := rows.Scan(&name, &enabled); err == nil {
			policy[strings.ToLower(strings.TrimSpace(name))] = enabled
		}
	}
	return policy, rows.Err()
}

func dedupeLoadedSkillsByName(in []*skills.Skill) []*skills.Skill {
	seen := make(map[string]struct{}, len(in))
	out := make([]*skills.Skill, 0, len(in))
	for _, skill := range in {
		if skill == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(skill.Name()))
		if key == "" {
			key = strings.TrimSpace(skill.SourcePath)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, skill)
	}
	return out
}

func discoveredSkillRow(skill *skills.Skill) db.DiscoveredSkillRow {
	kind := "instruction"
	if skill.Type == skills.Structured {
		kind = "structured"
	}
	triggersJSON, _ := json.Marshal(skill.Manifest.Triggers.Keywords)
	toolNames := make([]string, 0, len(skill.Manifest.ToolChain))
	for _, step := range skill.Manifest.ToolChain {
		if name := strings.TrimSpace(step.ToolName); name != "" {
			toolNames = append(toolNames, name)
		}
	}
	toolChainJSON, _ := json.Marshal(toolNames)
	return db.DiscoveredSkillRow{
		Name:           skill.Name(),
		Kind:           kind,
		Description:    skill.Manifest.Description,
		SourcePath:     skill.SourcePath,
		ContentHash:    skill.Hash,
		TriggersJSON:   string(triggersJSON),
		ToolChainJSON:  string(toolChainJSON),
		Version:        skill.Manifest.Version,
		Author:         skill.Manifest.Author,
		RegistrySource: "local",
	}
}
