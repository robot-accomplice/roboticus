package cmd

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"goboticus/internal/core"
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Open the dashboard in your default browser",
	RunE: func(cmd *cobra.Command, args []string) error {
		port := viper.GetInt("server.port")
		if port == 0 {
			port = core.DefaultServerPort
		}
		url := fmt.Sprintf("http://localhost:%d", port)

		fmt.Printf("Opening %s ...\n", url)
		return openBrowser(url)
	},
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", url).Start()
	default:
		return fmt.Errorf("unsupported platform %s — open %s manually", runtime.GOOS, url)
	}
}

func init() {
	rootCmd.AddCommand(webCmd)
}
