//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

// setProcessGroup creates a new process group for the command (Unix only)
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
