//go:build windows

package e2e_test

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	processQueryLimitedInformation = 0x1000
	processSynchronize             = 0x00100000
	waitTimeout                    = 0x00000102
)

var procGetProcessTimes = syscall.NewLazyDLL("kernel32.dll").NewProc("GetProcessTimes")

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

// isPIDAlive reports whether pid refers to the same living process instance as
// on first sight. OpenProcess+WaitForSingleObject alone answers "alive" both
// for the original process and for an unrelated process that reused a dead
// descendant's pid, so a confirmed-live probe is verified against the process
// creation time (GetProcessTimes) recorded on first sight.
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
	if res != waitTimeout {
		// Signaled: the process object is terminated (or still dying).
		return false
	}

	var creation, exit, kernel, user syscall.Filetime
	r1, _, _ := procGetProcessTimes.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r1 == 0 {
		// Identity unavailable; trust the liveness probe result.
		return true
	}
	token := fmt.Sprintf("%x:%x", creation.HighDateTime, creation.LowDateTime)
	return checkPIDInstance(pid, token, true)
}
