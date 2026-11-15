package tools

import (
	"encoding/json"
	"fmt"
	"gTSP/src/api"
	"gTSP/src/pal"
	"os"
	"strings"
	"time"
	"unicode/utf8"
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
	// StdoutOffset/StderrOffset are continuation cursors for process_output: pass
	// them as stdout_offset/stderr_offset to read only what came after this call.
	StdoutOffset *int64 `json:"stdout_offset,omitempty"`
	StderrOffset *int64 `json:"stderr_offset,omitempty"`
	// StdoutOverflow/StderrOverflow report that the ring buffer had to evict older
	// bytes, i.e. the *beginning* of the stream is gone for good.
	StdoutOverflow bool `json:"stdout_overflow,omitempty"`
	StderrOverflow bool `json:"stderr_overflow,omitempty"`
}

// BashBackgroundResult is returned when a process is running in the background
type BashBackgroundResult struct {
	ProcessID string `json:"process_id"`
	Status    string `json:"status"` // always "running"
	Stdout    string `json:"stdout"` // partial output captured before promotion (truncated)
	Stderr    string `json:"stderr"`
	Truncated bool   `json:"truncated,omitempty"`
	// Continuation cursors: pass as stdout_offset/stderr_offset to read only new
	// output; pass 0 to re-read what is still retained in the buffer.
	StdoutOffset   int64 `json:"stdout_offset"`
	StderrOffset   int64 `json:"stderr_offset"`
	StdoutOverflow bool  `json:"stdout_overflow,omitempty"`
	StderrOverflow bool  `json:"stderr_overflow,omitempty"`
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

	// markerBudget is reserved per stream so that "cut once" already guarantees
	// total length <= maxOutputBytes; every marker format below is far below it.
	markerBudget = 256

	// Head+Tail split (execute_bash sync / timeout-promoted background):
	// head keeps context, tail keeps the failure/result the agent actually needs.
	headBudgetPercent = 20
	headLinesMax      = 200
	tailLinesMax      = 800

	// tailWindowBytes is the read window for tail mode. It must be >= the 50 KB
	// output limit + marker budget so tail mode never under-delivers because the
	// window was too small.
	tailWindowBytes = 64 * 1024
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
		"- Output is capped per stream at 1000 lines / 50 KB, keeping BOTH the head and the tail (Head+Tail): the head keeps context and the tail keeps the failure/result. Dropped text is replaced by a single marker like '... (N bytes omitted) ...'; if the stream ring buffer had to evict older bytes you also get '... (N bytes earlier output evicted by buffer) ...' and stdout_overflow/stderr_overflow are set\n" +
		"- When a result carries process_id (timeout promotion or leftover handover) it also carries stdout_offset/stderr_offset: pass them to process_output to read only newer output. Never derive a cursor from len(stdout), because markers make the text longer than the bytes it represents\n" +
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
			ProcessID:    bp.ID,
			Status:       "running",
			Stdout:       "",
			Stderr:       "",
			Truncated:    false,
			StdoutOffset: 0,
			StderrOffset: 0,
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
		stdoutStats := bp.Stdout.Stats()
		stderrStats := bp.Stderr.Stats()
		result := buildSyncResult(bp)
		result.ProcessID = bp.ID
		result.LeftoverProcesses = leftover
		result.StdoutOffset = &stdoutStats.TotalBytes
		result.StderrOffset = &stderrStats.TotalBytes
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
			// Promotion must truncate too: the raw buffer can hold ~1 MB across
			// both streams, which would blow the agent's context window.
			// Cursor first, content second: if more output lands in between, the
			// cursor stays behind the text (re-read = duplicate) instead of ahead
			// of it (which would silently skip bytes).
			stdoutStats := stdoutBuf.Stats()
			stderrStats := stderrBuf.Stats()
			stdoutStr, stdoutClipped, stdoutOverflow := renderHeadTail(stdoutBuf)
			stderrStr, stderrClipped, stderrOverflow := renderHeadTail(stderrBuf)
			return BashBackgroundResult{
				ProcessID:      bp.ID,
				Status:         "running",
				Stdout:         stdoutStr,
				Stderr:         stderrStr,
				Truncated:      stdoutClipped || stderrClipped || stdoutOverflow || stderrOverflow,
				StdoutOffset:   stdoutStats.TotalBytes,
				StderrOffset:   stderrStats.TotalBytes,
				StdoutOverflow: stdoutOverflow,
				StderrOverflow: stderrOverflow,
			}, nil
		}
	}
}

func buildSyncResult(bp *api.BackgroundProcess) ExecuteBashResult {
	stdoutStr, stdoutClipped, stdoutOverflow := renderHeadTail(bp.Stdout)
	stderrStr, stderrClipped, stderrOverflow := renderHeadTail(bp.Stderr)
	exitCode := bp.GetExitCode()
	return ExecuteBashResult{
		Success:        exitCode == 0,
		Stdout:         stdoutStr,
		Stderr:         stderrStr,
		ExitCode:       exitCode,
		Truncated:      stdoutClipped || stderrClipped || stdoutOverflow || stderrOverflow,
		StdoutOverflow: stdoutOverflow,
		StderrOverflow: stderrOverflow,
	}
}

// --------------------------------------------------------------------------- #
// 截断：三种模式共用一套辅助，全部单遍完成
//
//   Head+Tail : execute_bash 同步完成 / 超时转后台
//   Prefix    : process_output 显式 offset 增量分页
//   Tail      : process_output 默认模式 / tail_lines
//
// 统一口径：
//   clipped  = 本次请求范围内有内容被丢弃或无法完整返回
//   truncated = clipped || overflow（overflow = 缓冲已覆盖最老字节）
//   clipped 与「是否输出 marker」解耦：无损翻页时 clipped=false 仍可能有尾部续读 marker。
// --------------------------------------------------------------------------- #

// utf8FloorByteLen returns the largest length <= limit that does not split a rune.
func utf8FloorByteLen(b []byte, limit int) int {
	if limit >= len(b) {
		return len(b)
	}
	if limit <= 0 {
		return 0
	}
	for limit > 0 && b[limit]&0xC0 == 0x80 {
		limit--
	}
	return limit
}

// utf8CeilStart returns the first index >= start that is not a continuation byte.
func utf8CeilStart(b []byte, start int) int {
	if start <= 0 {
		return 0
	}
	if start >= len(b) {
		return len(b)
	}
	for start < len(b) && b[start]&0xC0 == 0x80 {
		start++
	}
	return start
}

// splitLines splits on '\n' and drops the empty string produced by a trailing
// newline. A partial last line counts as one line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// firstNLines keeps at most the first n lines (including their newlines).
func firstNLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	seen := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '\n' {
			continue
		}
		seen++
		if seen >= n {
			return s[:i+1]
		}
	}
	return s
}

// tailFromLastLines returns the suffix covering at most the last n lines,
// starting on a line boundary. A trailing newline does not create a phantom line.
func tailFromLastLines(b []byte, n int) []byte {
	if n <= 0 || len(b) == 0 {
		return nil
	}
	i := len(b) - 1
	if b[i] == '\n' {
		i--
	}
	seen := 0
	for ; i >= 0; i-- {
		if b[i] != '\n' {
			continue
		}
		seen++
		if seen >= n {
			return b[i+1:]
		}
	}
	return b
}

// renderHeadTail implements the Head+Tail mode in a single pass.
func renderHeadTail(b *api.ProcBuffer) (text string, clipped bool, overflow bool) {
	snap := b.ReadHeadTail(maxOutputBytes, maxOutputBytes)
	overflow = snap.Overflow

	evictedMarker := func() string {
		return fmt.Sprintf("... (%d bytes earlier output evicted by buffer) ...\n", snap.EvictedBytes)
	}

	if !snap.Split {
		full := string(snap.Full)
		if len(splitLines(full)) <= maxOutputLines && len(full) <= maxOutputBytes {
			if snap.EvictedBytes == 0 {
				return full, false, overflow
			}
			// Fast path with an eviction notice: only when the notice still fits,
			// otherwise the notice itself would push us over 50 KB and clamping
			// would eat the tail we are trying to protect.
			if len(full) <= maxOutputBytes-markerBudget {
				return evictedMarker() + full, false, overflow
			}
		}
	}

	avail := maxOutputBytes - markerBudget
	headBudget := avail * headBudgetPercent / 100
	tailBudget := avail - headBudget

	headSrc, tailSrc := snap.Head, snap.Tail
	if !snap.Split {
		headSrc, tailSrc = snap.Full, snap.Full
	}

	headText := firstNLines(string(headSrc), headLinesMax)
	if len(headText) > headBudget {
		headText = headText[:utf8FloorByteLen([]byte(headText), headBudget)]
	}

	tailBytes := tailFromLastLines(tailSrc, tailLinesMax)
	if len(tailBytes) > tailBudget {
		tailBytes = tailBytes[utf8CeilStart(tailBytes, len(tailBytes)-tailBudget):]
	}

	retained := snap.MiddleBytes + int64(len(snap.Head)) + int64(len(snap.Tail))
	if !snap.Split {
		retained = int64(len(snap.Full))
	}
	omitted := retained - int64(len(headText)) - int64(len(tailBytes))
	if omitted < 0 {
		omitted = 0
	}

	marker := fmt.Sprintf("... (%d bytes omitted) ...", omitted)
	if snap.Overflow {
		marker = fmt.Sprintf("... (%d bytes earlier output evicted by buffer; %d bytes omitted) ...",
			snap.EvictedBytes, omitted)
	}

	var sb strings.Builder
	sb.Grow(headBudget + tailBudget + markerBudget)
	sb.WriteString(headText)
	if headText != "" && !strings.HasSuffix(headText, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString(marker)
	sb.WriteString("\n")
	sb.Write(tailBytes)
	return clampToOutputLimit(sb.String()), true, overflow
}

// renderTail implements the tail-only mode (process_output default & tail_lines).
func renderTail(b *api.ProcBuffer, targetLines int) (text string, clipped bool, overflow bool) {
	if targetLines <= 0 || targetLines > maxOutputLines {
		targetLines = maxOutputLines
	}
	snap := b.ReadTail(tailWindowBytes)
	overflow = snap.Overflow

	sel := snap.Data
	before := snap.BeforeBytes
	if trimmed := tailFromLastLines(sel, targetLines); len(trimmed) < len(sel) {
		before += int64(len(sel) - len(trimmed))
		sel = trimmed
	}

	budget := maxOutputBytes
	if before > 0 {
		budget = maxOutputBytes - markerBudget
	}

	hasByteTrunc := false
	if len(sel) > budget {
		start := utf8CeilStart(sel, len(sel)-budget)
		before += int64(start)
		sel = sel[start:]
		hasByteTrunc = true
	}

	out := string(sel)
	if before > 0 {
		out = fmt.Sprintf("... (%d bytes earlier output omitted/evicted) ...\n", before) + out
	}
	return clampToOutputLimit(out), hasByteTrunc, overflow
}

// renderPrefix implements the explicit-offset sequential pagination mode.
//
// deliveredBytes is always a precise byte count in the source stream (page
// boundaries may fall inside a line, which is fine: the returned next offset is
// exactly the right place to resume).
func renderPrefix(b *api.ProcBuffer, offset int64) (text string, next int64, clipped bool, overflow bool, expired bool, skipped int64) {
	avail := maxOutputBytes - markerBudget
	// Read up to avail - utf8.UTFMax so ReadFrom's rune-boundary extension
	// (up to UTFMax-1 bytes) never pushes len(data) past avail and triggers a
	// false clipped=true on lossless UTF-8 pagination.
	res := b.ReadFrom(offset, avail-utf8.UTFMax)
	overflow = res.Overflow

	if offset >= res.TotalBytes {
		// Echo the requested offset back: a cursor must never move backwards.
		return "", offset, false, overflow, false, 0
	}

	expired = res.OffsetExpired
	skipped = res.SkippedBytes

	data := res.Data
	delivered := len(data)
	// Stop at the maxOutputLines-th line boundary, in the source bytes.
	seen := 0
	for i := 0; i < len(data); i++ {
		if data[i] != '\n' {
			continue
		}
		seen++
		if seen >= maxOutputLines {
			delivered = i + 1
			break
		}
	}
	if delivered > avail {
		delivered = avail
	}
	delivered = utf8FloorByteLen(data, delivered)
	if len(data) > 0 && delivered == 0 {
		// Guarantee forward progress even for a single huge rune-sized window.
		delivered = utf8CeilStart(data, 0)
		if delivered < len(data) {
			_, size := utf8.DecodeRune(data[delivered:])
			delivered += size
		}
		if delivered > len(data) {
			delivered = len(data)
		}
	}

	next = res.NextBaseOffset + int64(delivered)
	if next > res.TotalBytes {
		next = res.TotalBytes
	}
	clipped = expired || delivered < len(data)

	var sb strings.Builder
	sb.Grow(len(data) + markerBudget)
	if expired {
		sb.WriteString(fmt.Sprintf("... (%d bytes evicted by buffer before this point) ...\n", skipped))
	} else if skipped > 0 {
		sb.WriteString(fmt.Sprintf("... (%d bytes skipped for UTF-8 alignment) ...\n", skipped))
	}
	sb.Write(data[:delivered])
	if next < res.TotalBytes {
		sb.WriteString(fmt.Sprintf("\n... (%d bytes remaining in buffer) ...", res.TotalBytes-next))
	}
	return clampToOutputLimit(sb.String()), next, clipped, overflow, expired, skipped
}

// clampToOutputLimit is a structural safety net: every mode above already
// budgets its output, so this only fires if a future marker format grows.
func clampToOutputLimit(s string) string {
	if len(s) <= maxOutputBytes {
		return s
	}
	b := []byte(s)
	cut := utf8FloorByteLen(b, maxOutputBytes)
	return string(b[:cut])
}
