//go:build windows

package pal

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

func KillProcessGroup(pid int) {
	// Use taskkill /T /F to kill the entire process tree
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// If taskkill fails (e.g., process already exited), ignore
		fmt.Fprintf(os.Stderr, "taskkill warning: %v\n", err)
	}
}

func SetProcessGroup(cmd *exec.Cmd) {
	// Windows doesn't have Unix-style process groups
	// taskkill /T handles process tree cleanup
}
