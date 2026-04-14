package tuicmd

import "github.com/spf13/cobra"

// Commands returns all tuicmd commands for registration on the root command.
func Commands() []*cobra.Command {
	return []*cobra.Command{tuiCmd}
}
