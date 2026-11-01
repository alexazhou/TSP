//go:build !windows

package pal

import (
	"fmt"
	"log"
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

// LogShellResolution 记录 execute_bash 实际会使用的 shell。
//
// Unix 上一律是 PATH 里的 bash；与 Windows 保持对称，日志格式一致，
// 便于按平台排查「命令到底在哪个 shell 里跑」。
func LogShellResolution() {
	path, err := exec.LookPath("bash")
	if err != nil {
		log.Printf("execute_bash: bash not found in PATH: %v", err)
		return
	}
	log.Printf("execute_bash: shell resolved: %s (bash)", path)
}
