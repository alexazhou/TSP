//go:build !windows

package pal

import (
	"fmt"
	"os/exec"
)

// ShellCommand 在 Unix 上直接使用 bash。
//
// Windows 版会用自带的 busybox（见 shell_windows.go）；Unix 上一律有 bash，
// 行为与引入 ShellCommand 之前完全一致，保证零回归。
func ShellCommand(script string) (*exec.Cmd, error) {
	path, err := exec.LookPath("bash")
	if err != nil {
		return nil, fmt.Errorf("bash not found in PATH: %v", err)
	}
	return exec.Command(path, "-c", script), nil
}
