package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage agent skills",
}

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List loaded skills",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/skills")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var skillsReloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Reload skills from disk",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/skills/reload", nil)
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var skillsCatalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "Browse skill catalog",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/skills/catalog")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var skillsShowCmd = &cobra.Command{
	Use:   "show <ID>",
	Short: "Show details for a specific skill",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/skills/" + args[0])
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var skillsCatalogListCmd = &cobra.Command{
	Use:   "catalog-list",
	Short: "List available skills in the catalog",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "/api/skills/catalog"
		query, _ := cmd.Flags().GetString("query")
		if query != "" {
			path += "?query=" + query
		}
		data, err := apiGet(path)
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var skillsCatalogInstallCmd = &cobra.Command{
	Use:   "catalog-install <SKILL...>",
	Short: "Install skills from the catalog",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/skills/catalog/install", map[string]any{
			"names": args,
		})
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var skillsCatalogActivateCmd = &cobra.Command{
	Use:   "catalog-activate [SKILL...]",
	Short: "Activate installed catalog skills",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiPost("/api/skills/catalog/activate", map[string]any{
			"names": args,
		})
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var skillsImportCmd = &cobra.Command{
	Use:   "import <SOURCE>",
	Short: "Import skills from a file, directory, or .tar.gz archive",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		source := args[0]
		info, err := os.Stat(source)
		if err != nil {
			return fmt.Errorf("cannot access %q: %w", source, err)
		}

		var files []struct {
			name    string
			content []byte
		}

		if info.IsDir() {
			// Import all skill files from a directory.
			entries, err := os.ReadDir(source)
			if err != nil {
				return fmt.Errorf("failed to read directory %q: %w", source, err)
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(e.Name()))
				if ext != ".md" && ext != ".toml" && ext != ".yaml" && ext != ".yml" {
					continue
				}
				raw, readErr := os.ReadFile(filepath.Join(source, e.Name()))
				if readErr != nil {
					fmt.Fprintf(os.Stderr, "  warning: skipping %q: %v\n", e.Name(), readErr)
					continue
				}
				files = append(files, struct {
					name    string
					content []byte
				}{e.Name(), raw})
			}
		} else if strings.HasSuffix(source, ".tar.gz") || strings.HasSuffix(source, ".tgz") {
			// Extract and import from tar.gz archive.
			f, openErr := os.Open(source)
			if openErr != nil {
				return fmt.Errorf("failed to open %q: %w", source, openErr)
			}
			defer func() { _ = f.Close() }()

			gr, gzErr := gzip.NewReader(f)
			if gzErr != nil {
				return fmt.Errorf("failed to decompress %q: %w", source, gzErr)
			}
			defer func() { _ = gr.Close() }()

			tr := tar.NewReader(gr)
			for {
				hdr, tarErr := tr.Next()
				if tarErr == io.EOF {
					break
				}
				if tarErr != nil {
					return fmt.Errorf("failed to read archive: %w", tarErr)
				}
				if hdr.Typeflag != tar.TypeReg {
					continue
				}
				ext := strings.ToLower(filepath.Ext(hdr.Name))
				if ext != ".md" && ext != ".toml" && ext != ".yaml" && ext != ".yml" {
					continue
				}
				raw, readErr := io.ReadAll(tr)
				if readErr != nil {
					fmt.Fprintf(os.Stderr, "  warning: skipping %q: %v\n", hdr.Name, readErr)
					continue
				}
				files = append(files, struct {
					name    string
					content []byte
				}{filepath.Base(hdr.Name), raw})
			}
		} else {
			// Single file import.
			raw, readErr := os.ReadFile(source)
			if readErr != nil {
				return fmt.Errorf("failed to read skill file %q: %w", source, readErr)
			}
			files = append(files, struct {
				name    string
				content []byte
			}{filepath.Base(source), raw})
		}

		if len(files) == 0 {
			return fmt.Errorf("no skill files found in %q", source)
		}

		imported := 0
		for _, sf := range files {
			_, postErr := apiPost("/api/skills", map[string]any{
				"content":  string(sf.content),
				"filename": sf.name,
			})
			if postErr != nil {
				fmt.Fprintf(os.Stderr, "  failed to import %q: %v\n", sf.name, postErr)
				continue
			}
			fmt.Printf("  imported %q\n", sf.name)
			imported++
		}

		fmt.Printf("Imported %d skill(s) from %s\n", imported, source)
		return nil
	},
}

var skillsExportCmd = &cobra.Command{
	Use:   "export [IDS...]",
	Short: "Export skills to a .tar.gz archive",
	RunE: func(cmd *cobra.Command, args []string) error {
		output, _ := cmd.Flags().GetString("output")

		// Collect skill data.
		var skills []any
		if len(args) == 0 {
			data, err := apiGet("/api/skills")
			if err != nil {
				return err
			}
			if list, ok := data["skills"].([]any); ok {
				skills = list
			} else {
				skills = []any{data}
			}
		} else {
			for _, id := range args {
				data, err := apiGet("/api/skills/" + id)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  warning: failed to export %q: %v\n", id, err)
					continue
				}
				skills = append(skills, data)
			}
		}

		if len(skills) == 0 {
			return fmt.Errorf("no skills to export")
		}

		f, err := os.Create(output)
		if err != nil {
			return fmt.Errorf("failed to create %q: %w", output, err)
		}
		defer func() { _ = f.Close() }()

		gw := gzip.NewWriter(f)
		defer func() { _ = gw.Close() }()
		tw := tar.NewWriter(gw)
		defer func() { _ = tw.Close() }()

		for i, skill := range skills {
			sm, _ := skill.(map[string]any)
			// Determine filename from skill data.
			name := fmt.Sprintf("skill-%d.json", i)
			if id, ok := sm["id"].(string); ok && id != "" {
				name = id + ".json"
			} else if sname, ok := sm["name"].(string); ok && sname != "" {
				name = sname + ".json"
			}

			b, marshalErr := json.MarshalIndent(sm, "", "  ")
			if marshalErr != nil {
				fmt.Fprintf(os.Stderr, "  warning: failed to marshal skill %d: %v\n", i, marshalErr)
				continue
			}

			hdr := &tar.Header{
				Name: name,
				Mode: 0o644,
				Size: int64(len(b)),
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return fmt.Errorf("failed to write tar header: %w", err)
			}
			if _, err := tw.Write(b); err != nil {
				return fmt.Errorf("failed to write tar entry: %w", err)
			}
		}

		fmt.Printf("Exported %d skill(s) to %s\n", len(skills), output)
		return nil
	},
}

func init() {
	skillsCatalogListCmd.Flags().StringP("query", "q", "", "Filter catalog by query string")
	skillsExportCmd.Flags().StringP("output", "o", "roboticus-skills-export.tar.gz", "Output file path")

	skillsCmd.AddCommand(
		skillsListCmd,
		skillsReloadCmd,
		skillsCatalogCmd,
		skillsShowCmd,
		skillsCatalogListCmd,
		skillsCatalogInstallCmd,
		skillsCatalogActivateCmd,
		skillsImportCmd,
		skillsExportCmd,
	)
	rootCmd.AddCommand(skillsCmd)
}
