package cmd

import (
	"archive/tar"
	"archive/zip"
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
	Use:     "plugins",
	Aliases: []string{"apps"}, // Rust parity: `apps` is an alias for `plugins`
	Short:   "Manage plugins (alias: apps)",
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

		// Detect archive install (.tar.gz, .zip, .ic.zip).
		if strings.HasSuffix(source, ".tar.gz") || strings.HasSuffix(source, ".zip") || strings.HasSuffix(source, ".ic.zip") {
			tmpDir, err := os.MkdirTemp("", "roboticus-plugin-*")
			if err != nil {
				return err
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			if strings.HasSuffix(source, ".tar.gz") {
				if err := extractTarGz(source, tmpDir); err != nil {
					return fmt.Errorf("failed to extract archive: %w", err)
				}
			} else {
				if err := extractZip(source, tmpDir); err != nil {
					return fmt.Errorf("failed to extract archive: %w", err)
				}
			}

			// Find plugin.json in extracted contents.
			pluginJSON := filepath.Join(tmpDir, "plugin.json")
			if _, err := os.Stat(pluginJSON); os.IsNotExist(err) {
				// Try one level deeper.
				dirEntries, _ := os.ReadDir(tmpDir)
				for _, e := range dirEntries {
					if e.IsDir() {
						candidate := filepath.Join(tmpDir, e.Name(), "plugin.json")
						if _, statErr := os.Stat(candidate); statErr == nil {
							pluginJSON = candidate
							break
						}
					}
				}
			}

			raw, err := os.ReadFile(pluginJSON)
			if err != nil {
				return fmt.Errorf("no plugin.json found in archive")
			}

			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				return fmt.Errorf("invalid plugin.json in archive: %w", err)
			}
			payload["source_path"] = filepath.Dir(pluginJSON)

			checkPluginDependencies(payload)

			data, err := apiPost("/api/plugins/install", payload)
			if err != nil {
				return err
			}
			fmt.Printf("Plugin installed from archive %q.\n", source)
			printJSON(data)
			return nil
		}

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

			checkPluginDependencies(payload)

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

		// Check for companion skills that reference this plugin.
		skillsDir := filepath.Join(home, ".roboticus", "skills")
		entries, _ := os.ReadDir(skillsDir)
		var companions []string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			content, _ := os.ReadFile(filepath.Join(skillsDir, e.Name()))
			if strings.Contains(string(content), name) {
				companions = append(companions, e.Name())
			}
		}
		if len(companions) > 0 {
			fmt.Printf("Found %d companion skill(s): %s\n", len(companions), strings.Join(companions, ", "))
			fmt.Println("Remove them manually if they depend on this plugin.")
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

// extractTarGz extracts a .tar.gz archive to the destination directory.
func extractTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(dest, hdr.Name)
		// Guard against zip-slip.
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid path in archive: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return err
			}
			_ = out.Close()
		}
	}
	return nil
}

// extractZip extracts a .zip archive to the destination directory.
func extractZip(archive, dest string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		target := filepath.Join(dest, f.Name)
		// Guard against zip-slip.
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid path in archive: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			_ = rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			_ = out.Close()
			_ = rc.Close()
			return err
		}
		_ = out.Close()
		_ = rc.Close()
	}
	return nil
}

// checkPluginDependencies inspects a parsed plugin.json payload for a
// "dependencies" map and warns about any that are not currently installed.
func checkPluginDependencies(payload map[string]any) {
	deps, ok := payload["dependencies"].(map[string]any)
	if !ok || len(deps) == 0 {
		return
	}

	plugins, err := apiGet("/api/plugins")
	if err != nil {
		// Server may not be running; skip the check silently.
		return
	}

	installed := make(map[string]bool)
	if list, ok := plugins["plugins"].([]any); ok {
		for _, p := range list {
			pm, _ := p.(map[string]any)
			if name, ok := pm["name"].(string); ok {
				installed[name] = true
			}
		}
	}

	for depName, depVersion := range deps {
		if !installed[depName] {
			fmt.Printf("Warning: dependency %q (version %v) is not installed\n", depName, depVersion)
		}
	}
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
