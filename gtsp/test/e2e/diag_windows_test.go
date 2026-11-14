//go:build windows

package e2e_test

import (
	"os/exec"
	"testing"
	"time"
)

// TestDiagWindowsJobHandover is a temporary diagnostic for the Windows
// handover/process_stop investigation (P7/P9 CI failures). It logs, without
// asserting, the full chain of facts needed to locate the mismatch:
//
//  1. pid-file pid ($! as reported by busybox sh)
//  2. gtsp Job-view leftover tree (real Windows PIDs inside the Job)
//  3. OS probe of the pid-file pid before stop
//  4. process_stop result
//  5. gtsp Job view + OS probe after stop
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
	t.Logf("os probe of pid-file pids before stop: %v", osSurvivors(t, pidFilePIDs))

	stopRes := s.callTool("process_stop", map[string]any{"process_id": procID})
	t.Logf("process_stop result: %v", stopRes)

	out2 := s.callTool("process_output", map[string]any{"process_id": procID, "block": false})
	t.Logf("process_output after stop: %v", out2)
	t.Logf("os probe of pid-file pids after stop: %v", osSurvivors(t, pidFilePIDs))

	taskOut, _ := exec.Command("tasklist", "/FI", "IMAGENAME eq busybox64u.exe", "/FO", "LIST").Output()
	t.Logf("busybox process table after stop:\n%s", taskOut)
}
