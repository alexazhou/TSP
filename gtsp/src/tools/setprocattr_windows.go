//go:build windows

package tools

import "os/exec"

// setProcessGroup is a no-op on Windows (no process group concept)
func setProcessGroup(cmd *exec.Cmd) {
	// Windows doesn't have Unix-style process groups
}
