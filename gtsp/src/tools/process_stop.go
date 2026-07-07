package tools

import (
	"gTSP/src/api"
	"encoding/json"
	"fmt"
	"time"
)

var ProcessStopSchema = api.ToolDefinition{
	Name:        "process_stop",
	Description: "- Terminates a running background process\n- If the process has already exited, this is a no-op and returns successfully\n- Returns process_id, success (true/false), and exit_code\n- Use this to clean up long-running background processes",
	InputSchema: map[string]interface{}{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]interface{}{
			"process_id": map[string]interface{}{
				"type":        "string",
				"description": "The process ID to terminate.",
			},
		},
		"required":             []string{"process_id"},
		"additionalProperties": false,
	},
}

// ProcessStopResult is the result of a process_stop call
type ProcessStopResult struct {
	ProcessID string `json:"process_id"`
	Success   bool   `json:"success"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Message   string `json:"message,omitempty"`
}

func ProcessStopHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	var p struct {
		ProcessID string `json:"process_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}

	bp, ok := api.GlobalProcessRegistry.Get(p.ProcessID)
	if !ok {
		return ProcessStopResult{
			ProcessID: p.ProcessID,
			Success:   false,
			Message:   fmt.Sprintf("process %q not found", p.ProcessID),
		}, nil
	}

	if bp.IsDone() {
		ec := bp.GetExitCode()
		return ProcessStopResult{
			ProcessID: p.ProcessID,
			Success:   true,
			ExitCode:  &ec,
			Message:   "process already exited",
		}, nil
	}

	bp.Kill()
	select {
	case <-bp.WaitChan():
		// Process terminated
	case <-time.After(2 * time.Second):
		return ProcessStopResult{
			ProcessID: p.ProcessID,
			Success:   false,
			Message:   "process kill timeout: did not terminate within 2 seconds",
		}, nil
	}

	ec := bp.GetExitCode()
	return ProcessStopResult{
		ProcessID: p.ProcessID,
		Success:   true,
		ExitCode:  &ec,
		Message:   "process terminated",
	}, nil
}
