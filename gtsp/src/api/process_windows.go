//go:build windows

package api

import "os"

// killProcessGroup kills the process (Windows doesn't have process groups like Unix)
func killProcessGroup(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
}
