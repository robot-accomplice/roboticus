package channels

import "github.com/spf13/cobra"

// Commands returns all channels commands for registration on the root command.
func Commands() []*cobra.Command {
	return []*cobra.Command{channelsCmd, mcpCmd}
}
