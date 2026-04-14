package admin

import "github.com/spf13/cobra"

// Commands returns all admin commands for registration on the root command.
func Commands() []*cobra.Command {
	return []*cobra.Command{adminCmd, daemonCmd, serviceCmd, checkCmd, mechanicCmd, defragCmd, resetCmd, migrateCmd, securityCmd, uninstallCmd, logsCmd, webCmd, metricsCmd}
}
