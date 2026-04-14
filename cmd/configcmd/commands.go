package configcmd

import "github.com/spf13/cobra"

// Commands returns all configcmd commands for registration on the root command.
func Commands() []*cobra.Command {
	return []*cobra.Command{configCmd, profileCmd, setupCmd, initCmd}
}
