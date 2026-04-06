package cmd

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var pluginsCmd = &cobra.Command{
	Use:   "plugins",
	Short: "Manage plugins",
}

var pluginsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed plugins",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/plugins")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var pluginsInfoCmd = &cobra.Command{
	Use:   "info <NAME>",
	Short: "Show details for a specific plugin",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		data, err := apiGet("/api/plugins")
		if err != nil {
			return err
		}

		// Filter for the requested plugin.
		if plugins, ok := data["plugins"].([]any); ok {
			for _, p := range plugins {
				pm, _ := p.(map[string]any)
				if pm["name"] == name || pm["id"] == name {
					printJSON(pm)
					return nil
				}
			}
		}

		fmt.Printf("Plugin %q not found.\n", name)
		return nil
	},
}

var pluginsInstallCmd = &cobra.Command{
	Use:   "install <SOURCE>",
	Short: "Install a plugin from the catalog",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/plugins/catalog/install", map[string]any{
			"name": args[0],
		})
		if err != nil {
			return err
		}
		fmt.Printf("Plugin %q installed.\n", args[0])
		printJSON(data)
		return nil
	},
}

var pluginsUninstallCmd = &cobra.Command{
	Use:   "uninstall <NAME>",
	Short: "Uninstall a plugin (requires server restart)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("To uninstall plugin %q:\n", args[0])
		fmt.Println("  1. Remove the plugin directory from your plugins folder")
		fmt.Println("  2. Restart the roboticus server")
		return nil
	},
}

var pluginsEnableCmd = &cobra.Command{
	Use:   "enable <NAME>",
	Short: "Enable a plugin",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/plugins/"+args[0]+"/enable", nil)
		if err != nil {
			return err
		}
		fmt.Printf("Plugin %q enabled.\n", args[0])
		if data != nil {
			printJSON(data)
		}
		return nil
	},
}

var pluginsDisableCmd = &cobra.Command{
	Use:   "disable <NAME>",
	Short: "Disable a plugin",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/plugins/"+args[0]+"/disable", nil)
		if err != nil {
			return err
		}
		fmt.Printf("Plugin %q disabled.\n", args[0])
		if data != nil {
			printJSON(data)
		}
		return nil
	},
}

var pluginsSearchCmd = &cobra.Command{
	Use:   "search <QUERY>",
	Short: "Search the plugin catalog",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("catalog search not yet available")
		return nil
	},
}

var pluginsPackCmd = &cobra.Command{
	Use:   "pack <DIR>",
	Short: "Package a plugin directory into an archive",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := args[0]

		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("%q is not a directory", dir)
		}

		outName := filepath.Base(dir) + ".tar.gz"
		f, err := os.Create(outName)
		if err != nil {
			return fmt.Errorf("failed to create %q: %w", outName, err)
		}
		defer func() { _ = f.Close() }()

		gw := gzip.NewWriter(f)
		defer func() { _ = gw.Close() }()
		tw := tar.NewWriter(gw)
		defer func() { _ = tw.Close() }()

		err = filepath.Walk(dir, func(path string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, _ := filepath.Rel(dir, path)
			if fi.IsDir() {
				return nil
			}
			hdr, hdrErr := tar.FileInfoHeader(fi, "")
			if hdrErr != nil {
				return hdrErr
			}
			hdr.Name = filepath.Join(filepath.Base(dir), rel)
			if writeErr := tw.WriteHeader(hdr); writeErr != nil {
				return writeErr
			}
			file, openErr := os.Open(path)
			if openErr != nil {
				return openErr
			}
			defer func() { _ = file.Close() }()
			_, copyErr := io.Copy(tw, file)
			return copyErr
		})
		if err != nil {
			return fmt.Errorf("failed to pack directory: %w", err)
		}

		fmt.Printf("Packed plugin to %s\n", outName)
		return nil
	},
}

func init() {
	pluginsCmd.AddCommand(
		pluginsListCmd,
		pluginsInfoCmd,
		pluginsInstallCmd,
		pluginsUninstallCmd,
		pluginsEnableCmd,
		pluginsDisableCmd,
		pluginsSearchCmd,
		pluginsPackCmd,
	)
	rootCmd.AddCommand(pluginsCmd)
}
