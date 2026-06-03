//go:build !windows

package api

import "syscall"

// killProcessGroup kills the entire process group (Unix only)
func killProcessGroup(pid int) {
	syscall.Kill(-pid, syscall.SIGKILL)
}
