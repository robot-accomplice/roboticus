package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Default(t *testing.T) {
	// loadConfig should return a valid default config even without a config file.
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Server.Port == 0 {
		t.Error("expected non-zero port in default config")
	}
}

func TestInitConfig_NoFile(t *testing.T) {
	// initConfig should not panic even without a config file.
	// It reads from viper, which has defaults.
	initConfig()
}

func TestInitConfig_WithFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "roboticus.toml")
	content := `[server]
port = 4444
`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	oldCfgFile := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = oldCfgFile }()

	initConfig()
}

func TestRootCmd_PersistentFlags(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("config")
	if f == nil {
		t.Fatal("expected 'config' persistent flag")
	}

	f = rootCmd.PersistentFlags().Lookup("port")
	if f == nil {
		t.Fatal("expected 'port' persistent flag")
	}

	f = rootCmd.PersistentFlags().Lookup("bind")
	if f == nil {
		t.Fatal("expected 'bind' persistent flag")
	}
}

func TestRootCmd_Name(t *testing.T) {
	if rootCmd.Name() != "roboticus" {
		t.Errorf("expected root command name 'roboticus', got %q", rootCmd.Name())
	}
}
