package core

// OsConfig represents the agent's identity and voice configuration (OS.toml).
type OsConfig struct {
	Identity   OsIdentity `json:"identity" mapstructure:"identity"`
	Voice      OsVoice    `json:"voice" mapstructure:"voice"`
	PromptText string     `json:"prompt_text" mapstructure:"prompt_text"`
}

// OsIdentity holds agent identity fields.
type OsIdentity struct {
	Name        string `json:"name" mapstructure:"name"`
	Version     string `json:"version" mapstructure:"version"`
	GeneratedBy string `json:"generated_by" mapstructure:"generated_by"`
}

// OsVoice holds voice/tone parameters.
type OsVoice struct {
	Formality     string `json:"formality" mapstructure:"formality"`
	Proactiveness string `json:"proactiveness" mapstructure:"proactiveness"`
	Verbosity     string `json:"verbosity" mapstructure:"verbosity"`
	Humor         string `json:"humor" mapstructure:"humor"`
	Domain        string `json:"domain" mapstructure:"domain"`
}

// FirmwareConfig represents guardrails and rules (FIRMWARE.toml).
type FirmwareConfig struct {
	Approvals FirmwareApprovals `json:"approvals" mapstructure:"approvals"`
	Rules     []FirmwareRule    `json:"rules" mapstructure:"rules"`
}

// FirmwareApprovals holds approval thresholds.
type FirmwareApprovals struct {
	SpendingThreshold   float64 `json:"spending_threshold" mapstructure:"spending_threshold"`
	RequireConfirmation string  `json:"require_confirmation" mapstructure:"require_confirmation"`
}

// FirmwareRule is a single guardrail rule.
type FirmwareRule struct {
	RuleType string `json:"rule_type" mapstructure:"rule_type"`
	Rule     string `json:"rule" mapstructure:"rule"`
}

// OperatorConfig represents the operator's profile (OPERATOR.toml).
type OperatorConfig struct {
	Identity    OperatorIdentity    `json:"identity" mapstructure:"identity"`
	Preferences OperatorPreferences `json:"preferences" mapstructure:"preferences"`
	Context     string              `json:"context" mapstructure:"context"`
}

// OperatorIdentity holds operator info.
type OperatorIdentity struct {
	Name     string `json:"name" mapstructure:"name"`
	Role     string `json:"role" mapstructure:"role"`
	Timezone string `json:"timezone" mapstructure:"timezone"`
}

// OperatorPreferences holds operator preferences.
type OperatorPreferences struct {
	CommunicationChannels []string `json:"communication_channels" mapstructure:"communication_channels"`
}

// DefaultOsConfig returns defaults for the personality system.
func DefaultOsConfig() OsConfig {
	return OsConfig{
		Identity: OsIdentity{
			Name:    "goboticus",
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
