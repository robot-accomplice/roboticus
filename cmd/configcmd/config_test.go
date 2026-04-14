package configcmd

import (
	"testing"

	"github.com/spf13/viper"
)

func TestConfigGetCmd_FoundKey(t *testing.T) {
	// Set a known key.
	viper.Set("test.known.key", "hello-value")
	defer viper.Set("test.known.key", nil)

	err := configGetCmd.RunE(configGetCmd, []string{"test.known.key"})
	if err != nil {
		t.Fatalf("config get with known key: %v", err)
	}
}

func TestConfigGetCmd_MissingKey(t *testing.T) {
	err := configGetCmd.RunE(configGetCmd, []string{"absolutely.nonexistent.deep.key"})
	if err == nil {
		t.Error("expected error for missing config key")
	}
}

func TestConfigValidateCmd_DefaultConfig(t *testing.T) {
	// This should work with the default config since cmdutil.LoadConfig() returns
	// DefaultConfig if viper has no overrides.
	err := configValidateCmd.RunE(configValidateCmd, nil)
	if err != nil {
		t.Fatalf("config validate: %v", err)
	}
}
