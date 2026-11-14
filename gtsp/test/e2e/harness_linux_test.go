//go:build linux

package e2e_test

import (
	"bytes"
	"os"
	"strconv"
	"strings"
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

// isPIDAlive reports whether pid refers to the same living process instance
// as on first sight.
//
// Two Linux refinements over the plain kill(pid, 0) probe:
//   - Zombies (state Z) linger after SIGKILL until reaped and still answer
//     kill(pid, 0); /proc/<pid>/stat exposes the real state, so treat Z as dead.
//   - pid numbers are reused: a dead descendant's pid can be grabbed by an
//     unrelated process. /proc/<pid>/stat field 22 (starttime) is the unique
//     instance identity (same scheme as ps/procps); record it on first sight
//     and treat a later mismatch as "the original process is gone".
func isPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err != nil && err != syscall.EPERM {
		return false
	}
	out, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		// Process vanished between kill() and the stat read: it is gone.
		return false
	}
	// stat layout: pid (comm) state ppid ... starttime ... — comm may contain
	// spaces/parens, so parsing restarts after the closing paren.
	i := bytes.LastIndexByte(out, ')')
	if i < 0 || i+2 >= len(out) {
		return true
	}
	fields := strings.Fields(string(out[i+2:]))
	// fields[0] is state (field 3); starttime (field 22) sits 19 tokens later.
	if len(fields) < 20 {
		return true
	}
	if fields[0] == "Z" {
		return false
	}
	return checkPIDInstance(pid, "st:"+fields[19], true)
}
