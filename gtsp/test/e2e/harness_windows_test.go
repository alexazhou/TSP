//go:build windows

package e2e_test

import (
	"os/exec"
	"strconv"
	"syscall"
)

const (
	processQueryLimitedInformation = 0x1000
	processSynchronize             = 0x00100000
	waitTimeout                    = 0x00000102
)

func setHarnessProcessGroup(_ *exec.Cmd) {}

func killHarnessProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	_ = cmd.Process.Kill()
}

func sendTerminateSignal(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// Windows lacks SIGTERM; fall back to Process.Kill.
	return cmd.Process.Kill()
}

func isPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(processQueryLimitedInformation|processSynchronize, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	res, err := syscall.WaitForSingleObject(h, 0)
	if err != nil {
		return false
	}
	return res == waitTimeout
}
