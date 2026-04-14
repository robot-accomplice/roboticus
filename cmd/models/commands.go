package models

import "github.com/spf13/cobra"

// Commands returns all models commands for registration on the root command.
func Commands() []*cobra.Command {
	return []*cobra.Command{modelsCmd, circuitCmd}
}
