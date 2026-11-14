//go:build windows

package e2e_test

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestDiagWindowsJobHandover is a temporary diagnostic for the Windows
// handover/process_stop investigation (P7/P9 CI failures). It logs, without
// asserting, the full chain of facts needed to locate the mismatch:
//
//  1. pid-file pid ($! as reported by busybox sh)
//  2. gtsp Job-view leftover tree (real Windows PIDs inside the Job)
//  3. OS probes: OpenProcess+WaitForSingleObject AND tasklist /FI "PID eq X"
//  4. process_stop result
//  5. gtsp Job view + both OS probes after stop
//  6. full busybox process table snapshot
//
// Remove once the root cause is fixed and P7/P9 re-enable on Windows.
func TestDiagWindowsJobHandover(t *testing.T) {
	s := startGTSP(t)

	res := s.callTool("execute_bash", map[string]any{
		"command": "sleep 30 >/dev/null 2>&1 & echo $! > diag.pid; echo started",
	}, 10*time.Second)
	t.Logf("execute_bash result: %v", res)

	procID, _ := res["process_id"].(string)
	if procID == "" {
		t.Fatalf("no handover happened: %v", res)
	}

	pidFilePIDs := readPIDFile(t, s.workdir+"/diag.pid")
	t.Logf("pid-file pids ($! from busybox sh): %v", pidFilePIDs)
	time.Sleep(200 * time.Millisecond)

	out1 := s.callTool("process_output", map[string]any{"process_id": procID, "block": false})
	t.Logf("process_output before stop: %v", out1)
	t.Logf("os probe (OpenProcess) before stop: %v", osSurvivors(t, pidFilePIDs))
	t.Logf("os probe (tasklist PID eq) before stop: %v", tasklistPIDProbe(t, pidFilePIDs))

	stopRes := s.callTool("process_stop", map[string]any{"process_id": procID})
	t.Logf("process_stop result: %v", stopRes)

	out2 := s.callTool("process_output", map[string]any{"process_id": procID, "block": false})
	t.Logf("process_output after stop: %v", out2)
	t.Logf("os probe (OpenProcess) after stop: %v", osSurvivors(t, pidFilePIDs))
	t.Logf("os probe (tasklist PID eq) after stop: %v", tasklistPIDProbe(t, pidFilePIDs))

	taskOut, _ := exec.Command("tasklist", "/FI", "IMAGENAME eq busybox64u.exe", "/FO", "LIST").Output()
	t.Logf("busybox process table after stop:\n%s", taskOut)
}

// tasklistPIDProbe reports which of pids tasklist sees as running processes,
// independent of OpenProcess semantics. A dead pid yields no output line.
func tasklistPIDProbe(t *testing.T, pids []int) []int {
	t.Helper()
	var found []int
	for _, pid := range pids {
		out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH", "/FO", "CSV").Output()
		if err != nil {
			t.Logf("tasklist PID %d: err=%v out=%q", pid, err, out)
			continue
		}
		line := strings.TrimSpace(string(out))
		if line != "" && !strings.Contains(line, "INFO:") {
			t.Logf("tasklist PID %d: %s", pid, line)
			found = append(found, pid)
		} else {
			t.Logf("tasklist PID %d: no entry", pid)
		}
	}
	return found
}

// TestDiagWindowsIncidentRepro reproduces the P7 scenario with full dumps:
// 4 busy-loop subshells + pipe truncation, comparing the pid-file pids
// against the gtsp Job-view pids BEFORE stopping, then the same after.
func TestDiagWindowsIncidentRepro(t *testing.T) {
	s := startGTSP(t)

	script := strings.Join([]string{
		"#!/bin/sh",
		": > load_pids.txt",
		"i=0",
		"while [ $i -lt 4 ]; do",
		"  (while :; do :; done) >/dev/null 2>&1 &",
		"  echo $! >> load_pids.txt",
		"  i=$((i + 1))",
		"done",
		"echo header_line_1",
		"j=0",
		"while [ $j -lt 50 ]; do",
		"  echo \"tick_$j\"",
		"  sleep 0.05",
		"  j=$((j + 1))",
		"done",
		"",
	}, "\n")
	s.callTool("write_file", map[string]any{"file_path": "load230.sh", "content": script})

	res := s.callTool("execute_bash", map[string]any{
		"command":        "sh load230.sh 2>&1 | head -1",
		"task_timeout":   10,
		"timeout_action": "kill",
	}, 15*time.Second)
	procID, _ := res["process_id"].(string)
	t.Logf("execute_bash result: %v", res)
	if procID == "" {
		t.Fatalf("no handover happened: %v", res)
	}

	pidFilePIDs := readPIDFile(t, s.workdir+"/load_pids.txt")
	t.Logf("pid-file pids: %v", pidFilePIDs)

	lp, _ := res["leftover_processes"].(map[string]any)
	t.Logf("leftover tree from execute_bash: %v", lp)

	out1 := s.callTool("process_output", map[string]any{"process_id": procID, "block": false})
	t.Logf("process_output before stop: %v", out1)
	t.Logf("os probe (OpenProcess) before stop: %v", osSurvivors(t, pidFilePIDs))
	t.Logf("os probe (tasklist PID eq) before stop: %v", tasklistPIDProbe(t, pidFilePIDs))

	stopRes := s.callTool("process_stop", map[string]any{"process_id": procID})
	t.Logf("process_stop result: %v", stopRes)

	out2 := s.callTool("process_output", map[string]any{"process_id": procID, "block": false})
	t.Logf("process_output after stop: %v", out2)
	t.Logf("os probe (OpenProcess) after stop: %v", osSurvivors(t, pidFilePIDs))
	t.Logf("os probe (tasklist PID eq) after stop: %v", tasklistPIDProbe(t, pidFilePIDs))
}
