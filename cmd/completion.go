package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion",
	Short: "Generate shell completions",
	Long: `Generate shell completion scripts for bash, zsh, or fish.

Example usage:
  # Bash
  goboticus completion bash > /etc/bash_completion.d/goboticus

  # Zsh
  goboticus completion zsh > "${fpath[1]}/_goboticus"

  # Fish
  goboticus completion fish > ~/.config/fish/completions/goboticus.fish`,
}

var completionBashCmd = &cobra.Command{
	Use:   "bash",
	Short: "Generate bash completion script",
	RunE: func(cmd *cobra.Command, args []string) error {
		return rootCmd.GenBashCompletion(os.Stdout)
	},
}

var completionZshCmd = &cobra.Command{
	Use:   "zsh",
	Short: "Generate zsh completion script",
	RunE: func(cmd *cobra.Command, args []string) error {
		return rootCmd.GenZshCompletion(os.Stdout)
	},
}

var completionFishCmd = &cobra.Command{
	Use:   "fish",
	Short: "Generate fish completion script",
	RunE: func(cmd *cobra.Command, args []string) error {
		return rootCmd.GenFishCompletion(os.Stdout, true)
	},
}

func init() {
	completionCmd.AddCommand(completionBashCmd, completionZshCmd, completionFishCmd)
	rootCmd.AddCommand(completionCmd)
}
