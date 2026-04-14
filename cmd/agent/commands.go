package agent

import "github.com/spf13/cobra"

// Commands returns all agent commands for registration on the root command.
func Commands() []*cobra.Command {
	return []*cobra.Command{agentsCmd, statusCmd, sessionsCmd, memoryCmd, ingestCmd}
}
