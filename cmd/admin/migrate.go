package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"roboticus/cmd/internal/cmdutil"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"roboticus/internal/core"
	"roboticus/internal/db"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run migrations and data import/export",
}

var (
	migrateAreas         []string
	migrateYes           bool
	migrateNoSafetyCheck bool
)

var migrateDBCmd = &cobra.Command{
	Use:   "db",
	Short: "Run database schema migrations",
	RunE:  runMigrate,
}

func runMigrate(cmd *cobra.Command, args []string) error {
	cfg, err := cmdutil.LoadConfig()
	if err != nil {
		return err
	}

	if err := cmdutil.EnsureParentDir(cfg.Database.Path); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	store, err := db.Open(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() { _ = store.Close() }()

	log.Info().Str("path", cfg.Database.Path).Msg("migrations complete")
	return nil
}

var migrateImportCmd = &cobra.Command{
	Use:   "import <SOURCE>",
	Short: "Import data from a Legacy workspace",
	Args:  cobra.ExactArgs(1),
	RunE:  runMigrateImport,
}

var migrateExportCmd = &cobra.Command{
	Use:   "export <TARGET>",
	Short: "Export data to Legacy format",
	Args:  cobra.ExactArgs(1),
	RunE:  runMigrateExport,
}

type migrationManifest struct {
	Source string              `json:"source"`
	Target string              `json:"target"`
	Areas  []string            `json:"areas"`
	Files  []migrationFileCopy `json:"files"`
}

type migrationFileCopy struct {
	Area        string `json:"area"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Kind        string `json:"kind"`
}

func runMigrateImport(cmd *cobra.Command, args []string) error {
	cfg, err := cmdutil.LoadConfig()
	if err != nil {
		return err
	}
	return importWorkspace(args[0], cfg, resolvedMigrationAreas(), migrateNoSafetyCheck || migrateYes)
}

func runMigrateExport(cmd *cobra.Command, args []string) error {
	cfg, err := cmdutil.LoadConfig()
	if err != nil {
		return err
	}
	return exportWorkspace(cfg, args[0], resolvedMigrationAreas())
}

func resolvedMigrationAreas() []string {
	if len(migrateAreas) == 0 {
		return []string{"config", "personality", "skills", "sessions", "cron", "channels", "agents"}
	}
	areas := make([]string, 0, len(migrateAreas))
	seen := make(map[string]bool)
	for _, area := range migrateAreas {
		normalized := strings.ToLower(strings.TrimSpace(area))
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		areas = append(areas, normalized)
	}
	return areas
}

func exportWorkspace(cfg core.Config, target string, areas []string) error {
	target = filepath.Clean(target)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	manifest := migrationManifest{
		Source: core.ConfigDir(),
		Target: target,
		Areas:  areas,
	}
	for _, area := range areas {
		switch area {
		case "config":
			if err := exportOptionalFile(&manifest, area, core.ConfigFilePath(), filepath.Join(target, "roboticus.toml")); err != nil {
				return err
			}
			if err := exportOptionalFile(&manifest, area, filepath.Join(core.ConfigDir(), "providers.toml"), filepath.Join(target, "providers.toml")); err != nil {
				return err
			}
			if err := exportOptionalFile(&manifest, area, filepath.Join(core.ConfigDir(), "keystore.enc"), filepath.Join(target, "keystore.enc")); err != nil {
				return err
			}
		case "personality":
			if err := exportOptionalFile(&manifest, area, cfg.Personality.OSPath, filepath.Join(target, "personality", filepath.Base(cfg.Personality.OSPath))); err != nil {
				return err
			}
			if err := exportOptionalFile(&manifest, area, cfg.Personality.FirmwarePath, filepath.Join(target, "personality", filepath.Base(cfg.Personality.FirmwarePath))); err != nil {
				return err
			}
			if err := exportOptionalFile(&manifest, area, cfg.Personality.OperatorPath, filepath.Join(target, "personality", filepath.Base(cfg.Personality.OperatorPath))); err != nil {
				return err
			}
		case "skills":
			if err := exportOptionalDir(&manifest, area, cfg.Skills.Directory, filepath.Join(target, "skills")); err != nil {
				return err
			}
		case "sessions", "cron", "channels", "agents":
			if err := exportOptionalFile(&manifest, area, cfg.Database.Path, filepath.Join(target, "roboticus.db")); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported migration area: %s", area)
		}
	}

	return writeMigrationManifest(filepath.Join(target, "migration_manifest.json"), manifest)
}

func importWorkspace(source string, cfg core.Config, areas []string, overwrite bool) error {
	source = filepath.Clean(source)
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("failed to access source: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source must be a directory")
	}

	for _, area := range areas {
		switch area {
		case "config":
			if err := importOptionalFile(filepath.Join(source, "roboticus.toml"), core.ConfigFilePath(), overwrite); err != nil {
				return err
			}
			if err := importOptionalFile(filepath.Join(source, "providers.toml"), filepath.Join(core.ConfigDir(), "providers.toml"), overwrite); err != nil {
				return err
			}
			if err := importOptionalFile(filepath.Join(source, "keystore.enc"), filepath.Join(core.ConfigDir(), "keystore.enc"), overwrite); err != nil {
				return err
			}
		case "personality":
			if err := importOptionalFile(filepath.Join(source, "personality", filepath.Base(cfg.Personality.OSPath)), cfg.Personality.OSPath, overwrite); err != nil {
				return err
			}
			if err := importOptionalFile(filepath.Join(source, "personality", filepath.Base(cfg.Personality.FirmwarePath)), cfg.Personality.FirmwarePath, overwrite); err != nil {
				return err
			}
			if err := importOptionalFile(filepath.Join(source, "personality", filepath.Base(cfg.Personality.OperatorPath)), cfg.Personality.OperatorPath, overwrite); err != nil {
				return err
			}
		case "skills":
			if err := importOptionalDir(filepath.Join(source, "skills"), cfg.Skills.Directory, overwrite); err != nil {
				return err
			}
		case "sessions", "cron", "channels", "agents":
			if err := importOptionalFile(filepath.Join(source, "roboticus.db"), cfg.Database.Path, overwrite); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported migration area: %s", area)
		}
	}
	return nil
}

func exportOptionalFile(manifest *migrationManifest, area, source, target string) error {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	if _, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", source, err)
	}
	if err := migrationCopyFile(source, target, true); err != nil {
		return err
	}
	manifest.Files = append(manifest.Files, migrationFileCopy{Area: area, Source: source, Destination: target, Kind: "file"})
	return nil
}

func exportOptionalDir(manifest *migrationManifest, area, source, target string) error {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	if _, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", source, err)
	}
	if err := migrationCopyDir(source, target, true); err != nil {
		return err
	}
	manifest.Files = append(manifest.Files, migrationFileCopy{Area: area, Source: source, Destination: target, Kind: "dir"})
	return nil
}

func importOptionalFile(source, target string, overwrite bool) error {
	if _, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", source, err)
	}
	return migrationCopyFile(source, target, overwrite)
}

func importOptionalDir(source, target string, overwrite bool) error {
	if _, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", source, err)
	}
	return migrationCopyDir(source, target, overwrite)
}

func migrationCopyFile(source, target string, overwrite bool) error {
	if source == "" || target == "" {
		return nil
	}
	if !overwrite {
		if _, err := os.Stat(target); err == nil {
			return fmt.Errorf("target already exists: %s", target)
		}
	}
	if err := cmdutil.EnsureParentDir(target); err != nil {
		return fmt.Errorf("create parent dir for %s: %w", target, err)
	}
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", source, err)
	}

	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s to %s: %w", source, target, err)
	}
	return nil
}

func migrationCopyDir(source, target string, overwrite bool) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		return migrationCopyFile(path, dst, overwrite)
	})
}

func writeMigrationManifest(path string, manifest migrationManifest) error {
	if err := cmdutil.EnsureParentDir(path); err != nil {
		return fmt.Errorf("create manifest dir: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func init() {
	// Default behavior: running `migrate` without subcommand runs DB migrations.
	migrateCmd.RunE = runMigrate
	migrateImportCmd.Flags().StringSliceVar(&migrateAreas, "areas", nil, "migration areas: config, personality, skills, sessions, cron, channels, agents")
	migrateImportCmd.Flags().BoolVar(&migrateYes, "yes", false, "skip confirmation prompts")
	migrateImportCmd.Flags().BoolVar(&migrateNoSafetyCheck, "no-safety-check", false, "overwrite existing files without safety checks")
	migrateExportCmd.Flags().StringSliceVar(&migrateAreas, "areas", nil, "migration areas: config, personality, skills, sessions, cron, channels, agents")

	migrateCmd.AddCommand(migrateDBCmd, migrateImportCmd, migrateExportCmd)
}
