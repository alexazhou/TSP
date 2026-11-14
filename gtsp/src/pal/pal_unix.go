//go:build !windows

package pal

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// ProcessTreeHandle tracks a Unix process group for tree-level probing and cleanup.
type ProcessTreeHandle struct {
	mu   sync.Mutex
	pgid int
	pid  int
	ppid int
}

// SetProcessTree configures cmd to start in its own process group (and sets
// Pdeathsig on Linux) before cmd.Start().
func SetProcessTree(cmd *exec.Cmd) (*ProcessTreeHandle, error) {
	attr := &syscall.SysProcAttr{Setpgid: true}
	setPdeathsig(attr)
	cmd.SysProcAttr = attr
	return &ProcessTreeHandle{ppid: os.Getpid()}, nil
}

// BindStartedProcess records the started process's PID/PGID.
func (h *ProcessTreeHandle) BindStartedProcess(cmd *exec.Cmd) error {
	if h == nil || cmd == nil || cmd.Process == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pid = cmd.Process.Pid
	h.pgid = cmd.Process.Pid
	return nil
}

// KillProcessTree sends SIGKILL to the entire process group.
func KillProcessTree(h *ProcessTreeHandle) {
	if h == nil {
		return
	}
	h.mu.Lock()
	pgid := h.pgid
	h.mu.Unlock()
	if pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}

// Close is a no-op on Unix because process group IDs do not hold kernel handles.
func (h *ProcessTreeHandle) Close() {}

// HasLeftoverProcesses checks whether any process in the process group is still alive.
func (h *ProcessTreeHandle) HasLeftoverProcesses() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	pgid := h.pgid
	h.mu.Unlock()
	if pgid <= 0 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || err == syscall.EPERM
}

// EnumerateLeftoverProcesses queries `ps` for surviving non-zombie members of the
// process group and returns a process tree rooted at the exited shell command.
func (h *ProcessTreeHandle) EnumerateLeftoverProcesses(command string) *LeftoverProcesses {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	pgid := h.pgid
	rootPID := h.pid
	rootPPID := h.ppid
	h.mu.Unlock()

	if pgid <= 0 {
		return buildProcessTree(rootPID, rootPPID, command, nil)
	}

	out, err := exec.Command("ps", "-ax", "-o", "pid=,ppid=,pgid=,state=,args=").Output()
	if err != nil {
		return buildProcessTree(rootPID, rootPPID, command, nil)
	}

	entries := parsePSOutput(out, pgid, rootPID)
	return buildProcessTree(rootPID, rootPPID, command, entries)
}

func parsePSOutput(out []byte, targetPGID, rootPID int) []procEntry {
	var entries []procEntry
	for _, rawLine := range bytes.Split(out, []byte("\n")) {
		line := strings.TrimSpace(string(rawLine))
		if line == "" {
			continue
		}
		pid, ppid, pgid, state, cmdArgs, ok := parsePSLine(line)
		if !ok {
			continue
		}
		if pgid != targetPGID || pid == rootPID {
			continue
		}
		if strings.HasPrefix(state, "Z") {
			continue
		}
		entries = append(entries, procEntry{
			pid:     pid,
			ppid:    ppid,
			command: cmdArgs,
		})
	}
	return entries
}

// parsePSLine extracts pid, ppid, pgid, state, and args from a single ps output line.
func parsePSLine(line string) (pid, ppid, pgid int, state, args string, ok bool) {
	fields := make([]string, 0, 4)
	i := 0
	n := len(line)
	for len(fields) < 4 {
		for i < n && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i >= n {
			return 0, 0, 0, "", "", false
		}
		start := i
		for i < n && line[i] != ' ' && line[i] != '\t' {
			i++
		}
		fields = append(fields, line[start:i])
	}
	for i < n && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	args = strings.TrimSpace(line[i:])

	var err error
	if pid, err = strconv.Atoi(fields[0]); err != nil {
		return 0, 0, 0, "", "", false
	}
	if ppid, err = strconv.Atoi(fields[1]); err != nil {
		return 0, 0, 0, "", "", false
	}
	if pgid, err = strconv.Atoi(fields[2]); err != nil {
		return 0, 0, 0, "", "", false
	}
	return pid, ppid, pgid, fields[3], args, true
}
