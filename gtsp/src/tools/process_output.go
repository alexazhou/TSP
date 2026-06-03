package tools

import (
	"gTSP/src/api"
	"encoding/json"
	"fmt"
	"time"
)

// ProcessOutputResult is the result of a process_output call
type ProcessOutputResult struct {
	ProcessID string  `json:"process_id"`
	Stdout    string  `json:"stdout"`
	Stderr    string  `json:"stderr"`
	Running   bool    `json:"running"`
	ExitCode  *int    `json:"exit_code"` // null while running
	Truncated bool    `json:"truncated"`
}

var ProcessOutputSchema = api.ToolDefinition{
	Name:        "process_output",
	Description: "- Retrieves output from a running or completed background process\n- Use block:true (default) to wait for process completion up to the timeout\n- Use block:false for a non-blocking snapshot of current output\n- Returns stdout, stderr, running status, and exit code when complete\n- Process IDs are only valid while the server session is active; they are lost on server restart\n- process_id is only returned when a process is promoted to background (via task_timeout expiry or run_in_background:true), not for processes that complete synchronously",
	InputSchema: map[string]interface{}{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]interface{}{
			"process_id": map[string]interface{}{
				"type":        "string",
				"description": "The process ID returned by execute_bash.",
			},
			"block": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether to wait for process completion. Defaults to true.",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Max wait time in ms when block:true. Defaults to 30000, max 60000. Note: actual effective wait time is also limited by the client's transport request timeout (typically 30-65s); if you need to wait longer, use block:false and poll repeatedly.",
			},
		},
		"required":             []string{"process_id"},
		"additionalProperties": false,
	},
}

func ProcessOutputHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	var p struct {
		ProcessID string `json:"process_id"`
		Block     *bool  `json:"block"`
		Timeout   *int   `json:"timeout"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}

	block := true
	if p.Block != nil {
		block = *p.Block
	}

	timeoutMs := 30000
	if p.Timeout != nil {
		timeoutMs = *p.Timeout
		if timeoutMs > 60000 {
			timeoutMs = 60000
		}
	}

	bp, ok := api.GlobalProcessRegistry.Get(p.ProcessID)
	if !ok {
		return nil, &api.TSPError{
			Code:    api.ErrNotFound,
			Message: fmt.Sprintf("process %q not found", p.ProcessID),
		}
	}

	if block && !bp.IsDone() {
		timer := time.NewTimer(time.Duration(timeoutMs) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-bp.WaitChan():
		case <-timer.C:
		}
	}

	running := !bp.IsDone()
	var exitCodePtr *int
	if !running {
		ec := bp.GetExitCode()
		exitCodePtr = &ec
	}

	stdoutStr, stdoutTrunc := truncateOutput(bp.Stdout.String())
	stderrStr, stderrTrunc := truncateOutput(bp.Stderr.String())

	return ProcessOutputResult{
		ProcessID: p.ProcessID,
		Stdout:    stdoutStr,
		Stderr:    stderrStr,
		Running:   running,
		ExitCode:  exitCodePtr,
		Truncated: stdoutTrunc || stderrTrunc,
	}, nil
}
