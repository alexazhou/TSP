package tools

import (
	"encoding/json"
	"fmt"
	"gTSP/src/api"
	"gTSP/src/pal"
	"os"
	"strings"
	"time"
)

// ExecuteBashParams defines input for execute_bash
type ExecuteBashParams struct {
	Command         string `json:"command"`
	TaskTimeout     int    `json:"task_timeout,omitempty"`      // Timeout in seconds; 0 or omitted uses default (180s)
	TimeoutAction   string `json:"timeout_action,omitempty"`    // "background" (default) | "kill"
	RunInBackground bool   `json:"run_in_background,omitempty"` // Always run as background process
	Description     string `json:"description,omitempty"`       // Audit description
}

// ExecuteBashResult defines output for a completed execute_bash call
type ExecuteBashResult struct {
	Success           bool                   `json:"success"`
	Stdout            string                 `json:"stdout"`
	Stderr            string                 `json:"stderr"`
	ExitCode          int                    `json:"exit_code"`
	Truncated         bool                   `json:"truncated,omitempty"`
	Message           string                 `json:"message,omitempty"`            // Optional: human-readable status message
	ProcessID         string                 `json:"process_id,omitempty"`         // Set when descendant processes survive after command exit
	LeftoverProcesses *pal.LeftoverProcesses `json:"leftover_processes,omitempty"` // Snapshot of surviving descendants
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
	Description: executeBashDescription(),
	InputSchema: map[string]interface{}{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type": "string",
				// 这里刻意不写死 "bash"：Windows 上实际是 busybox ash（POSIX sh），
				// 按 bash 习惯写 [[ ]]、{1..10}、数组会静默不生效（ash 把 {1..10}
				// 当字面量原样输出，不报错）。写错 shell 名称等于给出错误契约。
				"description": ShellCommandDescription(),
			},
			"task_timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Optional: Timeout in seconds (default 180s). When the command exceeds this timeout, the action taken depends on 'timeout_action'. Set to 0 to use the default timeout.",
			},
			"timeout_action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"background", "kill"},
				"description": "Optional: Action to take when task_timeout expires. 'background' (default) promotes the process to background and returns a process_id. 'kill' terminates the process and returns partial output.",
			},
			"run_in_background": map[string]interface{}{
				"type": "boolean",
				// 这里不能说「等价于 task_timeout=0 + timeout_action=background」：
				// task_timeout<=0 在代码里会被改成默认 180s（见 handler），命令要跑满
				// 180s 才提升。实测 `{"command":"sleep 3","task_timeout":0,
				// "timeout_action":"background"}` 阻塞 3.04s 后返回 ExecuteBashResult，
				// 而 run_in_background:true 是 0.01s 返回 BashBackgroundResult。
				"description": "Optional: Start the process immediately in the background and return a process_id right away, without waiting. Unlike setting task_timeout=0 (which falls back to the default 180s and only promotes to background if the command is still running then), this never blocks. Defaults to false.",
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
	defaultTaskTimeout = 180       // Default timeout in seconds
	maxOutputBytes     = 50 * 1024 // 50KB limit as per spec
	maxOutputLines     = 1000      // Line limit for safety
)

// executeBashDescription 拼出工具描述。
//
// shell 与路径两段由 pal 提供（按平台编译）：描述必须与真实行为一致，
// 而 Windows 上真实行为与其他平台不同。写死一份描述就等于对其中一个平台撒谎。
func executeBashDescription() string {
	return "- Executes a shell command in a controlled environment\n" +
		"- Shell: " + pal.ShellDescription() + "\n" +
		"- Paths: " + pal.PathNotes() + "\n" +
		"- Three execution modes:\n" +
		"  1. Synchronous, kill on timeout (timeout_action: \"kill\"): waits up to task_timeout seconds (default 180s); returns the result if the command finishes, otherwise kills the process group and returns partial output\n" +
		"  2. Synchronous, background on timeout (timeout_action: \"background\", default): waits up to task_timeout seconds (default 180s); returns the result if the command finishes, otherwise promotes the still-running process to background and returns a process_id\n" +
		"  3. Asynchronous (run_in_background: true): starts the command immediately in the background and returns a process_id without waiting (task_timeout and timeout_action are ignored)\n" +
		"- Synchronous mode leftover descendants: if descendant processes forked by the command (e.g., '&' or daemons) are still running after the command exits, they are NOT automatically terminated; the result will include process_id, leftover_processes, and a message so you can terminate them with process_stop or inspect them with process_output (unclaimed processes persist until server shutdown)\n" +
		"- Prefer run_in_background: true for intentionally long-running tasks (such as starting servers or listening on ports)\n" +
		"- Use process_output to poll or wait for background process completion\n" +
		"- Automatically truncates large outputs to protect context window\n" +
		"- Use this tool when you need to run shell commands, build scripts, run tests, or perform other system-level operations"
}

// ShellCommandDescription 是 command 参数的说明。
func ShellCommandDescription() string {
	return "The exact shell command to execute. " + pal.ShellDescription() + "."
}

// ExecuteBashHandler implements execute_bash with background promotion support
func ExecuteBashHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	var p ExecuteBashParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}

	cmd, err := pal.ShellCommand(p.Command)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare shell: %v", err)
	}

	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		_ = outR.Close()
		_ = outW.Close()
		return nil, fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	treeHandle, err := pal.SetProcessTree(cmd) // Create isolated process group / Job Object
	if err != nil {
		_ = outR.Close()
		_ = outW.Close()
		_ = errR.Close()
		_ = errW.Close()
		return nil, fmt.Errorf("failed to initialize process tree: %v", err)
	}

	stdoutBuf := &api.ProcBuffer{}
	stderrBuf := &api.ProcBuffer{}
	cmd.Stdout = outW
	cmd.Stderr = errW

	if err := cmd.Start(); err != nil {
		_ = outR.Close()
		_ = outW.Close()
		_ = errR.Close()
		_ = errW.Close()
		treeHandle.Close()
		return nil, fmt.Errorf("failed to start command: %v", err)
	}
	_ = outW.Close()
	_ = errW.Close()

	if err := treeHandle.BindStartedProcess(cmd); err != nil {
		_ = outR.Close()
		_ = errR.Close()
		treeHandle.Close()
		return nil, fmt.Errorf("failed to bind process tree: %v", err)
	}

	sessionID := ""
	if session != nil {
		sessionID = session.GetSessionID()
	}

	// Create a background process wrapper (starts I/O copy and Wait goroutines)
	id := api.GlobalProcessRegistry.GenerateID()
	bp := api.GlobalProcessRegistry.NewProcess(id, p.Command, sessionID, cmd, treeHandle, outR, errR, stdoutBuf, stderrBuf)

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

	// Determine timeout (default 180s)
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
		// Main shell exited before timeout; check for surviving descendants.
		if !bp.HasLeftoverProcesses() {
			bp.WaitIO(2 * time.Second)
			bp.Close()
			return buildSyncResult(bp), nil
		}
		leftover := bp.EnumerateLeftoverProcesses()
		if leftover == nil || leftover.Alive == 0 {
			bp.WaitIO(2 * time.Second)
			bp.Close()
			return buildSyncResult(bp), nil
		}

		// Hand over surviving descendants to the agent without killing them.
		api.GlobalProcessRegistry.Register(bp)
		bp.WaitIO(500 * time.Millisecond)
		result := buildSyncResult(bp)
		result.ProcessID = bp.ID
		result.LeftoverProcesses = leftover
		result.Message = "command exited but descendant process(es) are still running; " +
			"use process_stop to terminate, or process_output to poll; " +
			"unclaimed processes persist until server shutdown"
		return result, nil
	case <-timer.C:
		// Timeout
		switch action {
		case "kill":
			// Kill entire process tree to ensure child processes are also terminated
			bp.Kill()
			<-bp.WaitChan() // Wait for process to be reaped
			bp.WaitIO(500 * time.Millisecond)
			bp.Close()
			result := buildSyncResult(bp)
			result.Success = true // Expected behavior, not a failure
			result.Message = fmt.Sprintf("Command timed out after %ds and was killed", timeout)
			return result, nil
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
		Truncated: stdoutTrunc || stderrTrunc || bp.Stdout.Truncated() || bp.Stderr.Truncated(),
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
