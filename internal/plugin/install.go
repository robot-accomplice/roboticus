package plugin

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// InstallFromSource copies a manifest-backed plugin directory into the runtime plugin root.
func InstallFromSource(sourcePath, pluginDir string) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("stat source_path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source_path %q is not a directory", sourcePath)
	}

	return filepath.Walk(sourcePath, func(path string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}
		target := filepath.Join(pluginDir, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer func() { _ = out.Close() }()
		_, err = io.Copy(out, in)
		return err
	})
}

// InstallCompat writes a minimal manifest-backed plugin directory for the legacy
// name/content install contract.
func InstallCompat(pluginDir, name, content string) error {
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return fmt.Errorf("create plugin dir: %w", err)
	}
	manifest := []byte(fmt.Sprintf(
		"name = %q\nversion = %q\ndescription = %q\n\n[[tools]]\nname = %q\ndescription = %q\n",
		name, "1.0.0", "compatibility-installed plugin", "main", "compatibility-installed tool",
	))
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.toml"), manifest, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	scriptPath := filepath.Join(pluginDir, "main")
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		return fmt.Errorf("write script: %w", err)
	}
	return nil
}

// RemoveInstall removes an installed plugin directory.
func RemoveInstall(pluginDir string) error {
	return os.RemoveAll(pluginDir)
}
