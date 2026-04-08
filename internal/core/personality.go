package core

// OsConfig represents the agent's identity and voice configuration (OS.toml).
type OsConfig struct {
	Identity   OsIdentity `json:"identity" toml:"identity" mapstructure:"identity"`
	Voice      OsVoice    `json:"voice" toml:"voice" mapstructure:"voice"`
	PromptText string     `json:"prompt_text" toml:"prompt_text" mapstructure:"prompt_text"`
}

// OsIdentity holds agent identity fields.
type OsIdentity struct {
	Name        string `json:"name" toml:"name" mapstructure:"name"`
	Version     string `json:"version" toml:"version" mapstructure:"version"`
	GeneratedBy string `json:"generated_by" toml:"generated_by" mapstructure:"generated_by"`
}

// OsVoice holds voice/tone parameters.
type OsVoice struct {
	Formality     string `json:"formality" toml:"formality" mapstructure:"formality"`
	Proactiveness string `json:"proactiveness" toml:"proactiveness" mapstructure:"proactiveness"`
	Verbosity     string `json:"verbosity" toml:"verbosity" mapstructure:"verbosity"`
	Humor         string `json:"humor" toml:"humor" mapstructure:"humor"`
	Warmth        string `json:"warmth" toml:"warmth" mapstructure:"warmth"`
	Domain        string `json:"domain" toml:"domain" mapstructure:"domain"`
	PromptText    string `json:"prompt_text" toml:"prompt_text" mapstructure:"prompt_text"`
}

// FirmwareConfig represents guardrails and rules (FIRMWARE.toml).
type FirmwareConfig struct {
	Approvals FirmwareApprovals `json:"approvals" toml:"approvals" mapstructure:"approvals"`
	Rules     []FirmwareRule    `json:"rules" toml:"rules" mapstructure:"rules"`
}

// FirmwareApprovals holds approval thresholds.
type FirmwareApprovals struct {
	SpendingThreshold   float64 `json:"spending_threshold" toml:"spending_threshold" mapstructure:"spending_threshold"`
	RequireConfirmation string  `json:"require_confirmation" toml:"require_confirmation" mapstructure:"require_confirmation"`
}

// FirmwareRule is a single guardrail rule.
type FirmwareRule struct {
	RuleType string `json:"rule_type" toml:"rule_type" mapstructure:"rule_type"`
	Rule     string `json:"rule" toml:"rule" mapstructure:"rule"`
}

// OperatorConfig represents the operator's profile (OPERATOR.toml).
type OperatorConfig struct {
	Identity    OperatorIdentity    `json:"identity" toml:"identity" mapstructure:"identity"`
	Preferences OperatorPreferences `json:"preferences" toml:"preferences" mapstructure:"preferences"`
	Context     string              `json:"context" toml:"context" mapstructure:"context"`
}

// OperatorIdentity holds operator info.
type OperatorIdentity struct {
	Name     string `json:"name" toml:"name" mapstructure:"name"`
	Role     string `json:"role" toml:"role" mapstructure:"role"`
	Timezone string `json:"timezone" toml:"timezone" mapstructure:"timezone"`
}

// OperatorPreferences holds operator preferences.
type OperatorPreferences struct {
	CommunicationChannels []string `json:"communication_channels" toml:"communication_channels" mapstructure:"communication_channels"`
}

// DirectivesConfig represents goals, missions, and integrations (DIRECTIVES.toml).
type DirectivesConfig struct {
	Goals        DirectivesGoals        `json:"goals" toml:"goals" mapstructure:"goals"`
	Missions     []DirectivesMission    `json:"missions" toml:"missions" mapstructure:"missions"`
	Priorities   []string               `json:"priorities" toml:"priorities" mapstructure:"priorities"`
	Integrations DirectivesIntegrations `json:"integrations" toml:"integrations" mapstructure:"integrations"`
	Context      string                 `json:"context" toml:"context" mapstructure:"context"`
}

// DirectivesGoals holds monthly and yearly goals.
type DirectivesGoals struct {
	Monthly []string `json:"monthly" toml:"monthly" mapstructure:"monthly"`
	Yearly  []string `json:"yearly" toml:"yearly" mapstructure:"yearly"`
}

// DirectivesMission is a named mission with priority.
type DirectivesMission struct {
	Name        string `json:"name" toml:"name" mapstructure:"name"`
	Description string `json:"description" toml:"description" mapstructure:"description"`
	Priority    string `json:"priority" toml:"priority" mapstructure:"priority"`
	Timeframe   string `json:"timeframe" toml:"timeframe" mapstructure:"timeframe"`
}

// DirectivesIntegrations holds platform and workflow preferences.
type DirectivesIntegrations struct {
	Platforms []string `json:"platforms" toml:"platforms" mapstructure:"platforms"`
	Workflow  string   `json:"workflow" toml:"workflow" mapstructure:"workflow"`
}

// DefaultOsConfig returns defaults for the personality system.
func DefaultOsConfig() OsConfig {
	return OsConfig{
		Identity: OsIdentity{
			Name:    "roboticus",
			Version: "1.0",
		},
		Voice: OsVoice{
			Formality:     "balanced",
			Proactiveness: "suggest",
			Verbosity:     "concise",
			Humor:         "dry",
			Domain:        "general",
		},
	}
}

// DefaultFirmwareConfig returns default guardrails.
func DefaultFirmwareConfig() FirmwareConfig {
	return FirmwareConfig{
		Approvals: FirmwareApprovals{
			SpendingThreshold:   50.0,
			RequireConfirmation: "risky",
		},
	}
}

// DefaultOperatorConfig returns an empty operator config (optional file).
func DefaultOperatorConfig() OperatorConfig {
	return OperatorConfig{}
}

// DefaultDirectivesConfig returns an empty directives config (optional file).
func DefaultDirectivesConfig() DirectivesConfig {
	return DirectivesConfig{}
}
