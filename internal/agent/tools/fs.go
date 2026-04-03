package tools

import (
	"io/fs"
	"os"
	"path/filepath"
)

// FileSystem abstracts file operations for tool execution.
// Enables mock injection for testing without real filesystem access.
type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	ReadDir(path string) ([]fs.DirEntry, error)
	Stat(path string) (fs.FileInfo, error)
	Glob(pattern string) ([]string, error)
	OpenFile(name string, flag int, perm os.FileMode) (*os.File, error)
	Walk(root string, fn filepath.WalkFunc) error
}

// OSFileSystem is the real implementation wrapping the os and filepath packages.
type OSFileSystem struct{}

func (OSFileSystem) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
func (OSFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
func (OSFileSystem) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (OSFileSystem) ReadDir(path string) ([]fs.DirEntry, error)   { return os.ReadDir(path) }
func (OSFileSystem) Stat(path string) (fs.FileInfo, error)        { return os.Stat(path) }
func (OSFileSystem) Glob(pattern string) ([]string, error)        { return filepath.Glob(pattern) }
func (OSFileSystem) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag, perm)
}
func (OSFileSystem) Walk(root string, fn filepath.WalkFunc) error { return filepath.Walk(root, fn) }
