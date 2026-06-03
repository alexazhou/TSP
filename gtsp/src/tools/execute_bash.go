package tools

import (
	"gTSP/src/api"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ExecuteBashParams defines input for execute_bash
type ExecuteBashParams struct {
	Command         string `json:"command"`
	TaskTimeout     int    `json:"task_timeout,omitempty"`      // Timeout in seconds; 0 or omitted uses default (60s)
	TimeoutAction   string `json:"timeout_action,omitempty"`    // "background" (default) | "kill"
	RunInBackground bool   `json:"run_in_background,omitempty"` // Always run as background process
	Description     string `json:"description,omitempty"`       // Audit description
}

// ExecuteBashResult defines output for a completed execute_bash call
type ExecuteBashResult struct {
	Success   bool   `json:"success"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exit_code"`
	Truncated bool   `json:"truncated,omitempty"`
}

// BashBackgroundResult is returned when a process is running in the background
type BashBackgroundResult struct {
	ProcessID string `json:"process_id"`
	Status    string `json:"status"` // always "running"
	Stdout    string `json:"stdout"` // partial output captured before promotion
	Stderr    string `json:"stderr"`
}

var ExecuteBashSchema = api.ToolDefinition{
	Name:        "execute_bash",
	Description: "- Executes a system command in a controlled environment\n- Three execution modes:\n  1. Synchronous with timeout (default): command runs up to task_timeout seconds (default 60s); if still running, the action taken depends on 'timeout_action'\n  2. Synchronous no timeout: use run_in_background:true to start immediately in background\n  3. Background (run_in_background:true): starts immediately in background, returns process_id right away\n- Use process_output to poll or wait for background process completion\n- Automatically truncates large outputs to protect context window\n- Use this tool when you need to run shell commands, build scripts, run tests, or perform other system-level operations",
	InputSchema: map[string]interface{}{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The exact bash command to execute.",
			},
			"task_timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Optional: Timeout in seconds (default 60s). When the command exceeds this timeout, the action taken depends on 'timeout_action'. Set to 0 to use the default timeout.",
			},
			"timeout_action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"background", "kill"},
				"description": "Optional: Action to take when task_timeout expires. 'background' (default) promotes the process to background and returns a process_id. 'kill' terminates the process and returns partial output.",
			},
			"run_in_background": map[string]interface{}{
				"type":        "boolean",
				"description": "Optional: Start the process immediately in the background. Equivalent to task_timeout=0 with timeout_action='background'. Defaults to false.",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Optional: A brief description of the command's purpose.",
			},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	},
}

const (
	defaultTaskTimeout = 60 // Default timeout in seconds
	maxOutputBytes     = 50 * 1024 // 50KB limit as per spec
	maxOutputLines     = 1000      // Line limit for safety
)

// ExecuteBashHandler implements execute_bash with background promotion support
func ExecuteBashHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	var p ExecuteBashParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}

	cmd := exec.Command("bash", "-c", p.Command)
	setProcessGroup(cmd) // Create new process group for clean kill (Unix only)
	stdoutBuf := &api.ProcBuffer{}
	stderrBuf := &api.ProcBuffer{}
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %v", err)
	}

	// Create a background process wrapper (starts the Wait goroutine)
	id := api.GlobalProcessRegistry.GenerateID()
	bp := api.GlobalProcessRegistry.NewProcess(id, p.Command, cmd, stdoutBuf, stderrBuf)

	// RunInBackground: register and return immediately
	if p.RunInBackground {
		api.GlobalProcessRegistry.Register(bp)
		return BashBackgroundResult{
			ProcessID: bp.ID,
			Status:    "running",
			Stdout:    "",
			Stderr:    "",
		}, nil
	}

	// Determine timeout (default 60s)
	timeout := p.TaskTimeout
	if timeout <= 0 {
		timeout = defaultTaskTimeout
	}

	// Determine timeout action (default "background")
	action := p.TimeoutAction
	if action == "" {
		action = "background"
	}

	// Timed wait
	timer := time.NewTimer(time.Duration(timeout) * time.Second)
	defer timer.Stop()

	select {
	case <-bp.WaitChan():
		// Completed before timeout
		return buildSyncResult(bp), nil
	case <-timer.C:
		// Timeout
		switch action {
		case "kill":
			// Kill entire process group to ensure child processes are also terminated
			killProcessGroup(cmd.Process.Pid)
			<-bp.WaitChan() // Wait for process to be reaped
			return buildSyncResult(bp), nil
		default: // "background"
			api.GlobalProcessRegistry.Register(bp)
			return BashBackgroundResult{
				ProcessID: bp.ID,
				Status:    "running",
				Stdout:    stdoutBuf.String(),
				Stderr:    stderrBuf.String(),
			}, nil
		}
	}
}

func buildSyncResult(bp *api.BackgroundProcess) ExecuteBashResult {
	stdoutStr, stdoutTrunc := truncateOutput(bp.Stdout.String())
	stderrStr, stderrTrunc := truncateOutput(bp.Stderr.String())
	exitCode := bp.GetExitCode()
	return ExecuteBashResult{
		Success:   exitCode == 0,
		Stdout:    stdoutStr,
		Stderr:    stderrStr,
		ExitCode:  exitCode,
		Truncated: stdoutTrunc || stderrTrunc,
	}
}

func truncateOutput(s string) (string, bool) {
	truncated := false

	// 1. Line truncation
	lines := strings.Split(s, "\n")
	if len(lines) > maxOutputLines {
		lines = lines[:maxOutputLines]
		s = strings.Join(lines, "\n") + "\n... (further output truncated due to line limit)"
		truncated = true
	}

	// 2. Byte truncation
	if len(s) > maxOutputBytes {
		s = s[:maxOutputBytes] + "\n... (further output truncated due to byte limit)"
		truncated = true
	}

	return s, truncated
}
