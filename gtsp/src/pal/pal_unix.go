//go:build !windows

package pal

import (
	"os/exec"
	"syscall"
)

func KillProcessGroup(pid int) {
	syscall.Kill(-pid, syscall.SIGKILL)
}

func SetProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
