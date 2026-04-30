package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"roboticus/cmd/admin"
	"roboticus/cmd/agent"
	"roboticus/cmd/channels"
	"roboticus/cmd/configcmd"
	"roboticus/cmd/internal/cmdutil"
	"roboticus/cmd/models"
	"roboticus/cmd/schedule"
	"roboticus/cmd/servecmd"
	"roboticus/cmd/skills"
	"roboticus/cmd/tuicmd"
	"roboticus/cmd/updatecmd"
	"roboticus/cmd/wallet"
	"roboticus/internal/core"
)

var cfgFile string

// rootCmd is the base command for roboticus.
var rootCmd = &cobra.Command{
	Use:   "roboticus",
	Short: "Roboticus — autonomous agent runtime",
	Long:  `Roboticus is an autonomous agent runtime with multi-channel chat, LLM orchestration, memory, and tool execution.`,
}

const (
	cliGroupRuntime      = "runtime"
	cliGroupConfig       = "config"
	cliGroupIntelligence = "intelligence"
	cliGroupAutomation   = "automation"
	cliGroupAdmin        = "admin"
	cliGroupHelp         = "help"
)

var rootCommandGroupsAdded bool

// Execute adds all child commands to the root command and sets flags.
func Execute() {
	configureRootCommandGroups()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig, initLogger)

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default ~/.roboticus/roboticus.toml)")
	rootCmd.PersistentFlags().Int("port", core.DefaultServerPort, "server port")
	rootCmd.PersistentFlags().String("bind", core.DefaultServerBind, "server bind address")
	rootCmd.PersistentFlags().String("url", "", "gateway URL for management commands (env: ROBOTICUS_URL)")
	rootCmd.PersistentFlags().String("profile", "", "profile name for state isolation (env: ROBOTICUS_PROFILE)")
	rootCmd.PersistentFlags().String("color", "auto", "color output: auto, always, never")
	rootCmd.PersistentFlags().String("theme", "crt-green", "color theme (env: ROBOTICUS_THEME)")
	rootCmd.PersistentFlags().Bool("no-draw", false, "disable CRT typewriter draw effect")
	rootCmd.PersistentFlags().Bool("nerdmode", false, "retro mode (env: ROBOTICUS_NERDMODE)")
	rootCmd.PersistentFlags().Bool("quiet", false, "suppress informational output")
	rootCmd.PersistentFlags().Bool("json", false, "output structured JSON")

	_ = viper.BindPFlag("server.port", rootCmd.PersistentFlags().Lookup("port"))
	_ = viper.BindPFlag("server.bind", rootCmd.PersistentFlags().Lookup("bind"))
	_ = viper.BindPFlag("gateway.url", rootCmd.PersistentFlags().Lookup("url"))
	_ = viper.BindPFlag("profile", rootCmd.PersistentFlags().Lookup("profile"))
	_ = viper.BindPFlag("quiet", rootCmd.PersistentFlags().Lookup("quiet"))
	_ = viper.BindPFlag("json", rootCmd.PersistentFlags().Lookup("json"))
	_ = viper.BindEnv("config", "ROBOTICUS_CONFIG")

	// Register commands from domain subpackages.
	registerSubpackageCommands()
	configureRootCommandGroups()
}

// registerSubpackageCommands adds all commands from domain subpackages to rootCmd.
func registerSubpackageCommands() {
	rootCmd.AddCommand(agent.Commands()...)
	rootCmd.AddCommand(models.Commands()...)
	rootCmd.AddCommand(channels.Commands()...)
	rootCmd.AddCommand(configcmd.Commands()...)
	rootCmd.AddCommand(schedule.Commands()...)
	rootCmd.AddCommand(wallet.Commands()...)
	rootCmd.AddCommand(admin.Commands()...)
	rootCmd.AddCommand(skills.Commands()...)
	rootCmd.AddCommand(tuicmd.Commands()...)
	rootCmd.AddCommand(updatecmd.Commands()...)
	rootCmd.AddCommand(servecmd.Commands()...)
}

func configureRootCommandGroups() {
	if !rootCommandGroupsAdded {
		rootCmd.AddGroup(
			&cobra.Group{ID: cliGroupRuntime, Title: "Runtime & Operations"},
			&cobra.Group{ID: cliGroupConfig, Title: "Configuration & Security"},
			&cobra.Group{ID: cliGroupIntelligence, Title: "Models, Agents & Memory"},
			&cobra.Group{ID: cliGroupAutomation, Title: "Automation & Channels"},
			&cobra.Group{ID: cliGroupAdmin, Title: "Administration & Maintenance"},
			&cobra.Group{ID: cliGroupHelp, Title: "Help"},
		)
		rootCmd.SetHelpCommandGroupID(cliGroupHelp)
		rootCmd.SetCompletionCommandGroupID(cliGroupHelp)
		rootCommandGroupsAdded = true
	}

	commandGroups := map[string]string{
		"serve":      cliGroupRuntime,
		"status":     cliGroupRuntime,
		"mechanic":   cliGroupRuntime,
		"check":      cliGroupRuntime,
		"logs":       cliGroupRuntime,
		"daemon":     cliGroupRuntime,
		"service":    cliGroupRuntime,
		"web":        cliGroupRuntime,
		"tui":        cliGroupRuntime,
		"init":       cliGroupConfig,
		"setup":      cliGroupConfig,
		"config":     cliGroupConfig,
		"profile":    cliGroupConfig,
		"auth":       cliGroupConfig,
		"keystore":   cliGroupConfig,
		"security":   cliGroupConfig,
		"wallet":     cliGroupConfig,
		"models":     cliGroupIntelligence,
		"circuit":    cliGroupIntelligence,
		"agents":     cliGroupIntelligence,
		"sessions":   cliGroupIntelligence,
		"memory":     cliGroupIntelligence,
		"skills":     cliGroupIntelligence,
		"plugins":    cliGroupIntelligence,
		"mcp":        cliGroupIntelligence,
		"ingest":     cliGroupIntelligence,
		"schedule":   cliGroupAutomation,
		"cron":       cliGroupAutomation,
		"channels":   cliGroupAutomation,
		"admin":      cliGroupAdmin,
		"metrics":    cliGroupAdmin,
		"update":     cliGroupAdmin,
		"upgrade":    cliGroupAdmin,
		"version":    cliGroupAdmin,
		"migrate":    cliGroupAdmin,
		"defrag":     cliGroupAdmin,
		"reset":      cliGroupAdmin,
		"uninstall":  cliGroupAdmin,
		"completion": cliGroupHelp,
	}
	for _, command := range rootCmd.Commands() {
		if groupID, ok := commandGroups[command.Name()]; ok {
			command.GroupID = groupID
		}
	}
}

func initConfig() {
	// Track which config search path the operator actually got so we can
	// log it explicitly. The pre-v1.0.6 behavior silently swallowed
	// ConfigFileNotFoundError and fell through to DefaultConfig, which
	// produced the rogue ambient `~/.roboticus/roboticus.db` whenever
	// viper couldn't find roboticus.toml (sudo invocations that reset
	// HOME, scripts that don't propagate env, etc.). Operators had no
	// way to spot this from the CLI output — the daemon just silently
	// opened the wrong DB.
	var searchSource string
	switch {
	case cfgFile != "":
		viper.SetConfigFile(cfgFile)
		searchSource = "--config flag: " + cfgFile
	case os.Getenv("ROBOTICUS_CONFIG") != "":
		envCfg := os.Getenv("ROBOTICUS_CONFIG")
		viper.SetConfigFile(envCfg)
		searchSource = "ROBOTICUS_CONFIG env: " + envCfg
	default:
		configDir := core.ConfigDir()
		viper.AddConfigPath(configDir)
		viper.SetConfigName("roboticus")
		viper.SetConfigType("toml")
		searchSource = "search path: " + filepath.Join(configDir, "roboticus.toml")
	}

	viper.SetEnvPrefix("ROBOTICUS")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Loud warning: the operator ran a CLI verb that needs
			// config, but no config file was found. Falling back to
			// built-in defaults will cause the daemon to open
			// ~/.roboticus/roboticus.db rather than the configured
			// state.db, plant ambient empty stubs in workspace/, and
			// generally look like the wrong roboticus is running.
			// Surfacing this loudly is the only way the operator can
			// catch the silent-default failure mode that produced the
			// v1.0.5 rogue-DB report.
			fmt.Fprintf(os.Stderr,
				"warning: no roboticus config file found (%s); using built-in defaults — this is likely not what you want.\n"+
					"  Run `roboticus config init` to create one, or pass --config / set ROBOTICUS_CONFIG to point at an existing file.\n",
				searchSource)
			// Also record on the system-warnings surface so the
			// dashboard banner and bootStepWarn paths can pick it
			// up — log lines are easy to miss; the dashboard banner
			// keeps showing it until the operator addresses the
			// underlying condition.
			core.AddSystemWarning(core.SystemWarning{
				Code:     core.WarningCodeConfigDefaultsUsed,
				Title:    "No config file loaded — running on built-in defaults",
				Detail:   "Searched: " + searchSource + ". Without a config file, the daemon will open the default database (~/.roboticus/roboticus.db) and run with the default agent identity, which is almost certainly not the runtime you intended.",
				Remedy:   "Run `roboticus config init` to create a config, or pass --config / set ROBOTICUS_CONFIG to point at an existing file.",
				Severity: core.SystemWarningSeverityHigh,
			})
		} else {
			fmt.Fprintf(os.Stderr, "warning: config file error (%s): %v\n", searchSource, err)
		}
		return
	}
	// Successfully loaded — record the resolved file path so operators
	// can see in startup output which config was actually used. This is
	// the audit trail that prevents future "I'm running Duncan but the
	// daemon is acting like default roboticus" mysteries.
	if used := viper.ConfigFileUsed(); used != "" {
		fmt.Fprintf(os.Stderr, "config: loaded %s\n", used)
	}
}

// loadConfig unmarshals viper config into a core.Config struct.
// Deprecated: use cmdutil.LoadConfig() in subpackages.
func loadConfig() (core.Config, error) {
	return cmdutil.LoadConfig()
}

func initLogger() {
	log.Logger = zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
	).With().Timestamp().Caller().Logger()

	// Log level priority: env var > config file > default.
	// Env var takes precedence so operators can override without editing config.
	level := os.Getenv("ROBOTICUS_LOG_LEVEL")
	if level == "" {
		level = viper.GetString("agent.log_level")
	}
	if level == "" {
		level = "info"
	}
	zerolog.SetGlobalLevel(parseLogLevel(level))
}

func parseLogLevel(s string) zerolog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return zerolog.TraceLevel
	case "debug":
		return zerolog.DebugLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	default:
		return zerolog.InfoLevel
	}
}

// ensureParentDir creates the parent directory for a file path.
// Deprecated: use cmdutil.EnsureParentDir() in subpackages.
func ensureParentDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o700)
}
