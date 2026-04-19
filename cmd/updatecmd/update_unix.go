//go:build !windows

package updatecmd

import "os"

// replaceRunningBinary installs stagePath at execPath.
//
// On Unix, a running binary can be os.Rename()'d over in place: the kernel
// keeps the old inode alive for the running process (by virtue of the open
// file handle), while the new file takes the directory entry. Subsequent
// invocations of the binary path pick up the new file. This is the
// canonical atomic-swap pattern for Unix self-update flows.
//
// (The Windows counterpart has to do a rename-current-to-.old dance
// instead — see update_windows.go.)
func replaceRunningBinary(stagePath, execPath string) error {
	return os.Rename(stagePath, execPath)
}
