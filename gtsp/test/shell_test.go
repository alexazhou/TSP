package handlers_test

import (
	"gTSP/src/api"
	"gTSP/src/tools"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func setupShellTestSession() api.Session {
	s := api.NewSession()
	s.SetInitialized(true)
	// For shell tests, we usually don't need path rules unless the command 
	// is checked against them, but current implementation of execute_bash 
	// might not check them. Let's provide a broad allow just in case.
	rule := api.PathRule{Action: "allow", Path: "/"}
	s.SetPathRules([]api.PathRule{rule}, []api.PathRule{rule})
	return s
}

func TestExecuteBashHandler(t *testing.T) {
	session := setupShellTestSession()
	t.Run("success command", func(t *testing.T) {
		params := json.RawMessage(`{"command": "echo 'hello world'"}`)
		res, err := tools.ExecuteBashHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.ExecuteBashResult)
		if strings.TrimSpace(result.Stdout) != "hello world" {
			t.Errorf("got %q", result.Stdout)
		}
		if result.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", result.ExitCode)
		}
		if !result.Success {
			t.Errorf("expected success=true for exit code 0, got success=%v", result.Success)
		}
	})

	t.Run("failed command", func(t *testing.T) {
		params := json.RawMessage(`{"command": "exit 42"}`)
		res, err := tools.ExecuteBashHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.ExecuteBashResult)
		if result.ExitCode != 42 {
			t.Errorf("expected exit code 42, got %d", result.ExitCode)
		}
		if result.Success {
			t.Errorf("expected success=false for non-zero exit code, got success=%v", result.Success)
		}
	})

	t.Run("command not found", func(t *testing.T) {
		params := json.RawMessage(`{"command": "xyz"}`)
		res, err := tools.ExecuteBashHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.ExecuteBashResult)
		if result.ExitCode == 0 {
			t.Errorf("expected non-zero exit code for command not found, got %d", result.ExitCode)
		}
		if !strings.Contains(result.Stderr, "command not found") {
			t.Errorf("expected 'command not found' in stderr, got %q", result.Stderr)
		}
		if result.Success {
			t.Errorf("expected success=false for command not found, got success=%v", result.Success)
		}
	})

	t.Run("stderr capture", func(t *testing.T) {
		params := json.RawMessage(`{"command": "echo 'error message' >&2"}`)
		res, err := tools.ExecuteBashHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.ExecuteBashResult)
		if strings.TrimSpace(result.Stderr) != "error message" {
			t.Errorf("got stderr %q", result.Stderr)
		}
	})

	t.Run("truncation", func(t *testing.T) {
		// Generate 2000 lines of output (exceeding 1000 lines limit)
		params := json.RawMessage(`{"command": "for i in {1..2000}; do echo \"Line $i\"; done"}`)
		res, err := tools.ExecuteBashHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.ExecuteBashResult)
		if !result.Truncated {
			t.Error("expected output to be truncated")
		}
		if !strings.Contains(result.Stdout, "truncated due to line limit") {
			t.Errorf("missing truncation message in stdout: %s", result.Stdout)
		}
	})
}

func TestExecuteBash_RunInBackground(t *testing.T) {
	session := setupShellTestSession()
	params := json.RawMessage(`{"command": "sleep 10", "run_in_background": true}`)
	res, err := tools.ExecuteBashHandler(session, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, ok := res.(tools.BashBackgroundResult)
	if !ok {
		t.Fatalf("expected BashBackgroundResult, got %T", res)
	}
	if result.ProcessID == "" {
		t.Error("expected non-empty process_id")
	}
	if result.Status != "running" {
		t.Errorf("expected status 'running', got %q", result.Status)
	}

	// Clean up
	stopParams := json.RawMessage(`{"process_id": "` + result.ProcessID + `"}`)
	tools.ProcessStopHandler(session, stopParams)
}

func TestExecuteBash_TaskTimeout(t *testing.T) {
	session := setupShellTestSession()
	// task_timeout:1 with a command that sleeps for 5 seconds
	params := json.RawMessage(`{"command": "sleep 5", "task_timeout": 1}`)
	res, err := tools.ExecuteBashHandler(session, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, ok := res.(tools.BashBackgroundResult)
	if !ok {
		t.Fatalf("expected BashBackgroundResult after timeout, got %T", res)
	}
	if result.ProcessID == "" {
		t.Error("expected non-empty process_id after task_timeout promotion")
	}
	if result.Status != "running" {
		t.Errorf("expected status 'running', got %q", result.Status)
	}

	// Clean up
	stopParams := json.RawMessage(`{"process_id": "` + result.ProcessID + `"}`)
	tools.ProcessStopHandler(session, stopParams)
}

func TestProcessOutput(t *testing.T) {
	session := setupShellTestSession()
	// Start a background process that writes output after a short delay
	params := json.RawMessage(`{"command": "echo started && sleep 0.5 && echo done", "run_in_background": true}`)
	res, err := tools.ExecuteBashHandler(session, params)
	if err != nil {
		t.Fatalf("failed to start background process: %v", err)
	}
	bgResult, ok := res.(tools.BashBackgroundResult)
	if !ok {
		t.Fatalf("expected BashBackgroundResult, got %T", res)
	}

	// Poll with block:true, wait up to 5s
	outputParams := json.RawMessage(`{"process_id": "` + bgResult.ProcessID + `", "block": true, "timeout": 5000}`)
	outRes, err := tools.ProcessOutputHandler(session, outputParams)
	if err != nil {
		t.Fatalf("process_output failed: %v", err)
	}
	outResult, ok := outRes.(tools.ProcessOutputResult)
	if !ok {
		t.Fatalf("expected ProcessOutputResult, got %T", outRes)
	}
	if outResult.Running {
		t.Error("expected process to have completed")
	}
	if outResult.ExitCode == nil {
		t.Error("expected non-nil exit_code after completion")
	}
	if !strings.Contains(outResult.Stdout, "done") {
		t.Errorf("expected 'done' in stdout, got %q", outResult.Stdout)
	}
}

func TestProcessStop(t *testing.T) {
	session := setupShellTestSession()
	// Start a long-running background process
	params := json.RawMessage(`{"command": "sleep 30", "run_in_background": true}`)
	res, err := tools.ExecuteBashHandler(session, params)
	if err != nil {
		t.Fatalf("failed to start background process: %v", err)
	}
	bgResult, ok := res.(tools.BashBackgroundResult)
	if !ok {
		t.Fatalf("expected BashBackgroundResult, got %T", res)
	}

	// Stop the process
	stopParams := json.RawMessage(`{"process_id": "` + bgResult.ProcessID + `"}`)
	stopRes, err := tools.ProcessStopHandler(session, stopParams)
	if err != nil {
		t.Fatalf("process_stop failed: %v", err)
	}
	if stopRes == nil {
		t.Fatal("expected non-nil stop result")
	}

	// Verify process is no longer running after a short wait
	time.Sleep(100 * time.Millisecond)

	bp, ok := api.GlobalProcessRegistry.Get(bgResult.ProcessID)
	if !ok {
		t.Fatal("expected process to still be in registry")
	}
	if !bp.IsDone() {
		t.Error("expected process to be done after stop")
	}
}

func TestProcessOutput_NotFound(t *testing.T) {
	session := setupShellTestSession()
	outputParams := json.RawMessage(`{"process_id": "proc_nonexistent"}`)
	_, err := tools.ProcessOutputHandler(session, outputParams)
	if err == nil {
		t.Fatal("expected error for unknown process_id")
	}
	tspErr, ok := err.(*api.TSPError)
	if !ok {
		t.Fatalf("expected *api.TSPError, got %T: %v", err, err)
	}
	if tspErr.Code != api.ErrNotFound {
		t.Errorf("expected %q, got %q", api.ErrNotFound, tspErr.Code)
	}
}

func TestProcessList(t *testing.T) {
	session := setupShellTestSession()

	// Start: no running processes
	listParams := json.RawMessage(`{}`)
	res, err := tools.ProcessListHandler(session, listParams)
	if err != nil {
		t.Fatalf("process_list failed: %v", err)
	}
	listResult, ok := res.(tools.ProcessListResult)
	if !ok {
		t.Fatalf("expected ProcessListResult, got %T", res)
	}
	if len(listResult.Processes) != 0 {
		t.Errorf("expected 0 processes initially, got %d", len(listResult.Processes))
	}

	// Start two background processes
	params1 := json.RawMessage(`{"command": "sleep 10", "run_in_background": true}`)
	res1, err := tools.ExecuteBashHandler(session, params1)
	if err != nil {
		t.Fatalf("failed to start process 1: %v", err)
	}
	bg1 := res1.(tools.BashBackgroundResult)

	params2 := json.RawMessage(`{"command": "sleep 20", "run_in_background": true}`)
	res2, err := tools.ExecuteBashHandler(session, params2)
	if err != nil {
		t.Fatalf("failed to start process 2: %v", err)
	}
	bg2 := res2.(tools.BashBackgroundResult)

	// List should now show 2 running processes
	res, err = tools.ProcessListHandler(session, listParams)
	if err != nil {
		t.Fatalf("process_list failed: %v", err)
	}
	listResult = res.(tools.ProcessListResult)
	if len(listResult.Processes) != 2 {
		t.Errorf("expected 2 processes, got %d", len(listResult.Processes))
	}

	// Verify process details
	found1, found2 := false, false
	for _, proc := range listResult.Processes {
		if proc.ProcessID == bg1.ProcessID {
			found1 = true
			if proc.Command != "sleep 10" {
				t.Errorf("expected command 'sleep 10', got %q", proc.Command)
			}
			if proc.Status != "running" {
				t.Errorf("expected status 'running', got %q", proc.Status)
			}
			if proc.StartedAt == "" {
				t.Error("expected non-empty started_at")
			}
		}
		if proc.ProcessID == bg2.ProcessID {
			found2 = true
			if proc.Command != "sleep 20" {
				t.Errorf("expected command 'sleep 20', got %q", proc.Command)
			}
		}
	}
	if !found1 {
		t.Error("process 1 not found in list")
	}
	if !found2 {
		t.Error("process 2 not found in list")
	}

	// Stop one process
	stopParams := json.RawMessage(`{"process_id": "` + bg1.ProcessID + `"}`)
	tools.ProcessStopHandler(session, stopParams)

	// Wait for process to be marked as done
	time.Sleep(100 * time.Millisecond)

	// List should now show only 1 running process (completed ones are filtered)
	res, err = tools.ProcessListHandler(session, listParams)
	if err != nil {
		t.Fatalf("process_list failed: %v", err)
	}
	listResult = res.(tools.ProcessListResult)
	if len(listResult.Processes) != 1 {
		t.Errorf("expected 1 running process after stop, got %d", len(listResult.Processes))
	}

	// Clean up remaining process
	stopParams = json.RawMessage(`{"process_id": "` + bg2.ProcessID + `"}`)
	tools.ProcessStopHandler(session, stopParams)
}
