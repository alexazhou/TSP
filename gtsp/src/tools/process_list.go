package tools

import (
	"gTSP/src/api"
	"encoding/json"
	"fmt"
	"time"
)

// ProcessInfo represents a single running process in the list
type ProcessInfo struct {
	ProcessID   string `json:"process_id"`
	Command     string `json:"command"`
	RunningTime string `json:"running_time"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at,omitempty"`
}

// ProcessListResult is the result of a process_list call
type ProcessListResult struct {
	Processes []ProcessInfo `json:"processes"`
}

var ProcessListSchema = api.ToolDefinition{
	Name:        "process_list",
	Description: "- Lists all currently running background processes started by execute_bash\n- Returns process_id, command, running_time, and status for each process\n- Use this to check what background processes are active before calling process_output or process_stop",
	InputSchema: map[string]interface{}{
		"$schema":             "https://json-schema.org/draft/2020-12/schema",
		"type":                "object",
		"properties":          map[string]interface{}{},
		"required":            []string{},
		"additionalProperties": false,
	},
}

func ProcessListHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	procs := api.GlobalProcessRegistry.List()

	processes := make([]ProcessInfo, 0, len(procs))
	for _, bp := range procs {
		if bp.IsDone() {
			continue // Skip completed processes
		}

		runningTime := formatRunningTime(bp.StartedAt)
		processes = append(processes, ProcessInfo{
			ProcessID:   bp.ID,
			Command:     bp.Command,
			RunningTime: runningTime,
			Status:      "running",
			StartedAt:   bp.StartedAt.Format(time.RFC3339),
		})
	}

	return ProcessListResult{Processes: processes}, nil
}

func formatRunningTime(startedAt time.Time) string {
	duration := time.Since(startedAt)

	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}