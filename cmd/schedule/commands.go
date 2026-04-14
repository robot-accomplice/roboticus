package schedule

import "github.com/spf13/cobra"

// Commands returns all schedule commands for registration on the root command.
func Commands() []*cobra.Command {
	return []*cobra.Command{cronCmd, scheduleCmd}
}
