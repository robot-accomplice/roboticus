package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"goboticus/internal/core"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage agent configuration profiles",
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		reg := loadProfileRegistry()
		profiles := reg.List()

		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tACTIVE\tSOURCE\tDESCRIPTION")
		for _, p := range profiles {
			active := ""
			if p.Active {
				active = "*"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.Name, active, p.Source, p.Description)
		}
		return tw.Flush()
	},
}

var profileCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		desc, _ := cmd.Flags().GetString("description")
		reg := loadProfileRegistry()
		entry, err := reg.Create(args[0], desc)
		if err != nil {
			return err
		}
		fmt.Printf("Profile %q created at %s\n", entry.Name, reg.ConfigDir(args[0]))
		return nil
	},
}

var profileSwitchCmd = &cobra.Command{
	Use:   "switch <name>",
	Short: "Switch to a profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg := loadProfileRegistry()
		if err := reg.Switch(args[0]); err != nil {
			return err
		}
		fmt.Printf("Switched to profile %q\n", args[0])
		return nil
	},
}

var profileDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg := loadProfileRegistry()
		if err := reg.Delete(args[0]); err != nil {
			return err
		}
		fmt.Printf("Profile %q deleted\n", args[0])
		return nil
	},
}

func init() {
	profileCreateCmd.Flags().StringP("description", "d", "", "Profile description")
	profileCmd.AddCommand(profileListCmd, profileCreateCmd, profileSwitchCmd, profileDeleteCmd)
	rootCmd.AddCommand(profileCmd)
}

func loadProfileRegistry() *core.ProfileRegistry {
	home, _ := os.UserHomeDir()
	basePath := filepath.Join(home, ".goboticus")
	return core.NewProfileRegistry(basePath)
}
