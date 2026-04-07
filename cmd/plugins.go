package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	Short: "Install a plugin from the catalog or a local directory",
	Long: `Install a plugin. If SOURCE contains a path separator (/ or \),
it is treated as a local directory and plugin.json is read and posted.
Otherwise, SOURCE is treated as a catalog name.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		source := args[0]

		// Detect local directory install.
		if strings.Contains(source, "/") || strings.Contains(source, `\`) {
			pluginJSON := filepath.Join(source, "plugin.json")
			raw, err := os.ReadFile(pluginJSON)
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", pluginJSON, err)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				return fmt.Errorf("invalid plugin.json: %w", err)
			}
			// Include the source path so the server knows where the plugin lives.
			absPath, _ := filepath.Abs(source)
			payload["source_path"] = absPath

			data, err := apiPost("/api/plugins/install", payload)
			if err != nil {
				return err
			}
			fmt.Printf("Plugin installed from local directory %q.\n", source)
			printJSON(data)
			return nil
		}

		// Catalog install (existing behavior).
		data, err := apiPost("/api/plugins/catalog/install", map[string]any{
			"name": source,
		})
		if err != nil {
			return err
		}
		fmt.Printf("Plugin %q installed.\n", source)
		printJSON(data)
		return nil
	},
}

var pluginsUninstallCmd = &cobra.Command{
	Use:   "uninstall <NAME>",
	Short: "Uninstall a plugin",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		// Step 1: Disable the plugin via the API.
		_, err := apiPost("/api/plugins/"+name+"/disable", nil)
		if err != nil {
			fmt.Printf("Warning: could not disable plugin %q via API: %v\n", name, err)
		}

		// Step 2: Remove the plugin directory.
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to determine home directory: %w", err)
		}
		pluginDir := filepath.Join(home, ".roboticus", "plugins", name)
		if _, statErr := os.Stat(pluginDir); os.IsNotExist(statErr) {
			fmt.Printf("Plugin directory %q does not exist; nothing to remove.\n", pluginDir)
			return nil
		}
		if err := os.RemoveAll(pluginDir); err != nil {
			return fmt.Errorf("failed to remove plugin directory %q: %w", pluginDir, err)
		}

		fmt.Printf("Plugin %q uninstalled (disabled and directory removed).\n", name)
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
		registryURL := resolveRegistryURL(effectiveConfigPath())
		manifest, err := fetchRegistryManifest(context.Background(), registryURL)
		if err != nil {
			return fmt.Errorf("failed to fetch plugin catalog: %w", err)
		}
		if manifest.Packs.Plugins == nil {
			return fmt.Errorf("plugin catalog is not available in the registry")
		}

		query := strings.ToLower(strings.TrimSpace(args[0]))
		var results []pluginCatalogEntry
		for _, entry := range manifest.Packs.Plugins.Catalog {
			if query == "" ||
				strings.Contains(strings.ToLower(entry.Name), query) ||
				strings.Contains(strings.ToLower(entry.Description), query) ||
				strings.Contains(strings.ToLower(entry.Author), query) {
				results = append(results, entry)
			}
		}

		if len(results) == 0 {
			fmt.Printf("No plugins found matching %q.\n", args[0])
			return nil
		}
		printJSON(map[string]any{"plugins": results})
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

		// Compute SHA-256 hash of the archive.
		if _, seekErr := f.Seek(0, 0); seekErr != nil {
			return fmt.Errorf("failed to seek archive for hashing: %w", seekErr)
		}
		h := sha256.New()
		if _, copyErr := io.Copy(h, f); copyErr != nil {
			return fmt.Errorf("failed to hash archive: %w", copyErr)
		}

		fmt.Printf("Packed plugin to %s\n", outName)
		fmt.Printf("SHA-256: %x\n", h.Sum(nil))
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
