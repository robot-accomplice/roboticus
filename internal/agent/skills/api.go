package skills

// SkillLoader is the public loader type used by daemon wiring and parity checks.
// It aliases Loader so existing code keeps working while the subsystem has a
// first-class, stable API surface.
type SkillLoader = Loader

// NewSkillLoader creates a skill loader.
func NewSkillLoader() *SkillLoader {
	return NewLoader()
}

// LoadSkills is the package-level convenience entry point for loading all
// skills from a directory tree.
func LoadSkills(dir string) []*Skill {
	return NewSkillLoader().LoadFromDir(dir)
}
