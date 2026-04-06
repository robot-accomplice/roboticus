package cmd

import (
	"encoding/json"
	"fmt"
	"os"

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
	Short: "Import a skill from a file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		raw, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("failed to read skill file %q: %w", args[0], err)
		}
		data, err := apiPost("/api/skills", map[string]any{
			"content": string(raw),
		})
		if err != nil {
			return err
		}
		fmt.Println("Skill imported.")
		printJSON(data)
		return nil
	},
}

var skillsExportCmd = &cobra.Command{
	Use:   "export [IDS...]",
	Short: "Export skills to a file",
	RunE: func(cmd *cobra.Command, args []string) error {
		output, _ := cmd.Flags().GetString("output")

		// Collect skill data.
		var skills []any
		if len(args) == 0 {
			// Export all skills.
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

		b, err := json.MarshalIndent(skills, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal skills: %w", err)
		}

		if err := os.WriteFile(output, b, 0644); err != nil {
			return fmt.Errorf("failed to write %q: %w", output, err)
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
