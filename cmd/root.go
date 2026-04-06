package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"roboticus/internal/core"
)

var cfgFile string

// rootCmd is the base command for roboticus.
var rootCmd = &cobra.Command{
	Use:   "roboticus",
	Short: "Roboticus — autonomous agent runtime",
	Long:  `Roboticus is an autonomous agent runtime with multi-channel chat, LLM orchestration, memory, and tool execution.`,
}

// Execute adds all child commands to the root command and sets flags.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig, initLogger)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default ~/.roboticus/roboticus.toml)")
	rootCmd.PersistentFlags().Int("port", core.DefaultServerPort, "server port")
	rootCmd.PersistentFlags().String("bind", core.DefaultServerBind, "server bind address")

	_ = viper.BindPFlag("server.port", rootCmd.PersistentFlags().Lookup("port"))
	_ = viper.BindPFlag("server.bind", rootCmd.PersistentFlags().Lookup("bind"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		configDir := core.ConfigDir()
		viper.AddConfigPath(configDir)
		viper.SetConfigName("roboticus")
		viper.SetConfigType("toml")
	}

	viper.SetEnvPrefix("ROBOTICUS")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "warning: config file error: %v\n", err)
		}
	}
}

// loadConfig unmarshals viper config into a core.Config struct.
func loadConfig() (core.Config, error) {
	cfg := core.DefaultConfig()
	if err := viper.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("failed to parse config: %w", err)
	}
	cfg.MergeBundledProviders()
	cfg.NormalizePaths()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func initLogger() {
	log.Logger = zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
	).With().Timestamp().Caller().Logger()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}

// ensureParentDir creates the parent directory for a file path.
func ensureParentDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o700)
}
