package updatecmd

import "github.com/spf13/cobra"

// Commands returns all updatecmd commands for registration on the root command.
func Commands() []*cobra.Command {
	return []*cobra.Command{updateCmd, upgradeCmd, versionCmd, authCmd, keystoreCmd}
}
