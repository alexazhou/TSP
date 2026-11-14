package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcess_SyncAndTruncation(t *testing.T) {
	s := startGTSP(t)

	t.Run("P1_sync_stdout_stderr_exitcode", func(t *testing.T) {
		res := s.callTool("execute_bash", map[string]any{
			"command": "echo hello_out; echo hello_err >&2; exit 3",
		})
		if ok, _ := res["success"].(bool); ok {
			t.Errorf("expected success=false for exit 3, got %v", res)
		}
		if ec := int(res["exit_code"].(float64)); ec != 3 {
			t.Errorf("expected exit_code=3, got %d", ec)
		}
		if !strings.Contains(res["stdout"].(string), "hello_out") {
			t.Errorf("missing stdout: %q", res["stdout"])
		}
		if !strings.Contains(res["stderr"].(string), "hello_err") {
			t.Errorf("missing stderr: %q", res["stderr"])
		}
	})

	t.Run("P2_output_truncation", func(t *testing.T) {
		res := s.callTool("execute_bash", map[string]any{
			"command": "awk 'BEGIN{for(i=0;i<1500;i++) printf \"line-%04d\\n\", i}'",
		})
		if trunc, _ := res["truncated"].(bool); !trunc {
			t.Errorf("expected truncated=true for >1000 lines, got %v", res)
		}
		if !strings.Contains(res["stdout"].(string), "truncated") {
			t.Errorf("expected truncation marker in stdout")
		}
	})
}

func TestProcess_TimeoutAndBackground(t *testing.T) {
	s := startGTSP(t)

	t.Run("P3_timeout_kill", func(t *testing.T) {
		pidFile := "p3.pid"
		res := s.callTool("execute_bash", map[string]any{
			"command":        "sleep 60 >/dev/null 2>&1 & echo $! > " + pidFile + "; echo partial_out; wait",
			"task_timeout":   1,
			"timeout_action": "kill",
		}, 10*time.Second)

		if !strings.Contains(res["message"].(string), "timed out") {
			t.Errorf("expected timeout message, got %q", res["message"])
		}
		if !strings.Contains(res["stdout"].(string), "partial_out") {
			t.Errorf("expected partial stdout, got %q", res["stdout"])
		}
		if data, err := os.ReadFile(filepath.Join(s.workdir, pidFile)); err == nil && len(data) > 0 {
			pids := readPIDFile(t, filepath.Join(s.workdir, pidFile))
			if surv := osSurvivors(t, pids); len(surv) > 0 {
				t.Fatalf("descendant survived timeout kill: %v", surv)
			}
		}
	})

	t.Run("P4_timeout_promote_to_background", func(t *testing.T) {
		res := s.callTool("execute_bash", map[string]any{
			"command":        "sleep 30",
			"task_timeout":   1,
			"timeout_action": "background",
		}, 10*time.Second)

		procID, _ := res["process_id"].(string)
		if procID == "" {
			t.Fatalf("expected process_id on background promotion, got %v", res)
		}
		if status, _ := res["status"].(string); status != "running" {
			t.Errorf("expected status=running, got %q", status)
		}
		stopRes := s.callTool("process_stop", map[string]any{"process_id": procID})
		if ok, _ := stopRes["success"].(bool); !ok {
			t.Errorf("process_stop failed: %v", stopRes)
		}
	})

	t.Run("P5_P6_run_in_background_list_and_output", func(t *testing.T) {
		res := s.callTool("execute_bash", map[string]any{
			"command":           "sleep 1; echo bg_done",
			"run_in_background": true,
		})
		procID, _ := res["process_id"].(string)
		if procID == "" {
			t.Fatalf("expected process_id, got %v", res)
		}

		// P6: process_list shows running entry
		listRes := s.callTool("process_list", map[string]any{})
		procs, _ := listRes["processes"].([]any)
		found := false
		for _, item := range procs {
			if m, ok := item.(map[string]any); ok && m["process_id"] == procID {
				found = true
				if m["running_time"] == "" {
					t.Errorf("expected non-empty running_time: %v", m)
				}
			}
		}
		if !found {
			t.Errorf("expected %q in process_list: %v", procID, procs)
		}

		// P5: blocking process_output with explicit 10s callTool timeout
		outRes := s.callTool("process_output", map[string]any{
			"process_id": procID,
			"block":      true,
			"timeout":    8000,
		}, 10*time.Second)
		if running, _ := outRes["running"].(bool); running {
			t.Errorf("expected running=false after completion, got %v", outRes)
		}
		if ec := int(outRes["exit_code"].(float64)); ec != 0 {
			t.Errorf("expected exit_code=0, got %d", ec)
		}
		if !strings.Contains(outRes["stdout"].(string), "bg_done") {
			t.Errorf("expected bg_done in stdout, got %q", outRes["stdout"])
		}
	})
}

func TestProcess_LeftoverHandoverAndCleanup(t *testing.T) {
	s := startGTSP(t)

	t.Run("P7_P8_P10_incident_repro_handover_stop_idempotent", func(t *testing.T) {
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
		s.callTool("write_file", map[string]any{
			"file_path": "load230.sh",
			"content":   script,
		})

		res := s.callTool("execute_bash", map[string]any{
			"command":        "sh load230.sh 2>&1 | head -1",
			"task_timeout":   10,
			"timeout_action": "kill",
		}, 15*time.Second)

		procID, _ := res["process_id"].(string)
		if procID == "" {
			t.Fatalf("expected process_id for leftover descendants, got %v", res)
		}
		lp, _ := res["leftover_processes"].(map[string]any)
		if lp == nil {
			t.Fatalf("expected leftover_processes in result: %v", res)
		}
		if alive := int(lp["alive"].(float64)); alive < 4 {
			t.Errorf("expected leftover_processes.alive >= 4, got %d (%v)", alive, lp)
		}

		// Out-of-band OS verification
		filePIDs := readPIDFile(t, filepath.Join(s.workdir, "load_pids.txt"))
		if len(filePIDs) != 4 {
			t.Fatalf("expected 4 pids in load_pids.txt, got %v", filePIDs)
		}
		if surv := osSurvivors(t, filePIDs); len(surv) != 4 {
			t.Fatalf("expected 4 surviving OS processes before process_stop, got %v", surv)
		}

		// P8: process_stop cleans all descendants
		stopRes := s.callTool("process_stop", map[string]any{"process_id": procID})
		if ok, _ := stopRes["success"].(bool); !ok {
			t.Fatalf("process_stop failed: %v", stopRes)
		}
		if surv := osSurvivors(t, filePIDs); len(surv) != 0 {
			t.Fatalf("descendants survived process_stop: %v", surv)
		}

		// Verify removed from process_list
		listRes := s.callTool("process_list", map[string]any{})
		for _, item := range listRes["processes"].([]any) {
			if m, ok := item.(map[string]any); ok && m["process_id"] == procID {
				t.Errorf("stopped leftover entry %q still in process_list", procID)
			}
		}

		// P10: process_stop is idempotent
		stopAgain := s.callTool("process_stop", map[string]any{"process_id": procID})
		if ok, _ := stopAgain["success"].(bool); !ok {
			t.Errorf("second process_stop should succeed idempotently, got %v", stopAgain)
		}
	})

	t.Run("P9_intentional_background_not_auto_killed", func(t *testing.T) {
		res := s.callTool("execute_bash", map[string]any{
			"command": "sleep 30 >/dev/null 2>&1 & echo $! > p9.pid; echo started",
		}, 10*time.Second)

		procID, _ := res["process_id"].(string)
		if procID == "" {
			t.Fatalf("expected process_id for background child, got %v", res)
		}
		pids := readPIDFile(t, filepath.Join(s.workdir, "p9.pid"))
		if surv := osSurvivors(t, pids); len(surv) == 0 {
			t.Fatalf("intentional background child was killed unexpectedly")
		}

		outRes := s.callTool("process_output", map[string]any{
			"process_id": procID,
			"block":      false,
		})
		if running, _ := outRes["running"].(bool); !running {
			t.Errorf("expected running=true for surviving background child")
		}

		stopRes := s.callTool("process_stop", map[string]any{"process_id": procID})
		if ok, _ := stopRes["success"].(bool); !ok {
			t.Errorf("process_stop failed: %v", stopRes)
		}
		if surv := osSurvivors(t, pids); len(surv) != 0 {
			t.Errorf("child survived process_stop: %v", surv)
		}
	})
}
