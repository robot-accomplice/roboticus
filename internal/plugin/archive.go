package plugin

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxArchiveSize = 50 * 1024 * 1024 // 50MB

// PackPlugin zips the plugin directory into a .zip archive at outputPath.
// The archive must contain a manifest.toml or manifest.yaml at the root.
func PackPlugin(pluginDir, outputPath string) error {
	// Verify the plugin directory contains a manifest.
	if !hasManifest(pluginDir) {
		return fmt.Errorf("plugin directory %q must contain manifest.toml or manifest.yaml", pluginDir)
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create archive: %w", err)
	}
	defer func() { _ = outFile.Close() }()

	zw := zip.NewWriter(outFile)
	defer func() { _ = zw.Close() }()

	var totalSize int64
	err = filepath.Walk(pluginDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}

		totalSize += info.Size()
		if totalSize > maxArchiveSize {
			return fmt.Errorf("plugin exceeds maximum archive size of %d MB", maxArchiveSize/(1024*1024))
		}

		relPath, _ := filepath.Rel(pluginDir, path)
		relPath = filepath.ToSlash(relPath)

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return fmt.Errorf("failed to create zip header for %s: %w", relPath, err)
		}
		header.Name = relPath
		header.Method = zip.Deflate

		writer, err := zw.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("failed to create zip entry for %s: %w", relPath, err)
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", path, err)
		}
		defer func() { _ = f.Close() }()

		_, err = io.Copy(writer, f)
		return err
	})
	if err != nil {
		// Clean up partial file on error.
		_ = outFile.Close()
		_ = os.Remove(outputPath)
		return err
	}

	return nil
}

// UnpackPlugin extracts a zip archive into the destination directory.
// Validates that the archive contains a manifest.toml or manifest.yaml at the root.
func UnpackPlugin(archivePath, destDir string) error {
	// Check archive size before opening.
	info, err := os.Stat(archivePath)
	if err != nil {
		return fmt.Errorf("failed to stat archive: %w", err)
	}
	if info.Size() > maxArchiveSize {
		return fmt.Errorf("archive exceeds maximum size of %d MB", maxArchiveSize/(1024*1024))
	}

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer func() { _ = zr.Close() }()

	// Validate that the archive contains a manifest at root.
	hasValidManifest := false
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		if name == "manifest.toml" || name == "manifest.yaml" || name == "manifest.yml" {
			hasValidManifest = true
			break
		}
	}
	if !hasValidManifest {
		return fmt.Errorf("archive must contain manifest.toml or manifest.yaml at root")
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}

	for _, f := range zr.File {
		// Prevent zip slip.
		target := filepath.Join(destDir, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) &&
			filepath.Clean(target) != filepath.Clean(destDir) {
			return fmt.Errorf("illegal file path in archive: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, f.Mode()); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		outFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("failed to create %s: %w", f.Name, err)
		}

		rc, err := f.Open()
		if err != nil {
			_ = outFile.Close()
			return fmt.Errorf("failed to open archive entry %s: %w", f.Name, err)
		}

		// Limit copy to prevent decompression bomb.
		_, err = io.Copy(outFile, io.LimitReader(rc, maxArchiveSize))
		_ = rc.Close()
		_ = outFile.Close()
		if err != nil {
			return fmt.Errorf("failed to extract %s: %w", f.Name, err)
		}
	}

	return nil
}

func hasManifest(dir string) bool {
	for _, name := range []string{"manifest.toml", "manifest.yaml", "manifest.yml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}
