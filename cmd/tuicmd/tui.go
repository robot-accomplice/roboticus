package tuicmd

import (
	"roboticus/cmd/internal/cmdutil"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"roboticus/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch interactive terminal interface",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL := cmdutil.APIBaseURL()
		model := tui.NewModel(baseURL, "")
		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("TUI error: %w", err)
		}
		return nil
	},
}
