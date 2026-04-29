package tools

import (
	"gTSP/src/api"
	"encoding/json"
	"fmt"
)

var ProcessStopSchema = api.ToolDefinition{
	Name:        "process_stop",
	Description: "- Terminates a running background process\n- If the process has already exited, this is a no-op and returns successfully\n- Use this to clean up long-running background processes",
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

func ProcessStopHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	var p struct {
		ProcessID string `json:"process_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}

	bp, ok := api.GlobalProcessRegistry.Get(p.ProcessID)
	if !ok {
		return nil, &api.TSPError{
			Code:    api.ErrNotFound,
			Message: fmt.Sprintf("process %q not found", p.ProcessID),
		}
	}

	// No-op if already exited
	if !bp.IsDone() {
		bp.Kill()
	}

	return map[string]interface{}{}, nil
}
