//go:build linux

package e2e_test

import (
	"bytes"
	"os/exec"
	"strconv"
	"syscall"
)

func setHarnessProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killHarnessProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

func sendTerminateSignal(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(syscall.SIGTERM)
}

// isPIDAlive reports whether pid refers to a runnable/running process.
// Linux refinement: kill(pid, 0) also succeeds for zombies (state Z), which
// linger after SIGKILL until the parent (or its reparent target) reaps them;
// /proc/<pid>/stat exposes the real state, so treat Z as dead.
func isPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err != nil && err != syscall.EPERM {
		return false
	}
	out, err := exec.Command("cat", "/proc/"+strconv.Itoa(pid)+"/stat").Output()
	if err != nil {
		// Process vanished between kill() and stat read: it is gone.
		return false
	}
	// stat layout: pid (comm) state ... — comm may contain spaces/parens,
	// so the state char is the first non-space byte after the closing paren.
	i := bytes.LastIndexByte(out, ')')
	if i < 0 || i+2 >= len(out) {
		return true
	}
	return out[i+2] != 'Z'
}
