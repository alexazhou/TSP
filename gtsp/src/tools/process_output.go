package tools

import (
	"encoding/json"
	"fmt"
	"gTSP/src/api"
	"gTSP/src/pal"
	"time"
)

// ProcessOutputResult is the result of a process_output call
type ProcessOutputResult struct {
	ProcessID string `json:"process_id"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Running   bool   `json:"running"`
	ExitCode  *int   `json:"exit_code"` // null while running
	Truncated bool   `json:"truncated"`
	// Continuation cursors. len(stdout) is NOT the cursor: the text contains
	// markers and may start mid-line, so always feed these values back in.
	StdoutOffset int64 `json:"stdout_offset"`
	StderrOffset int64 `json:"stderr_offset"`
	// Overflow: the stream's ring buffer evicted its oldest bytes for good.
	StdoutOverflow bool `json:"stdout_overflow,omitempty"`
	StderrOverflow bool `json:"stderr_overflow,omitempty"`
	// OffsetExpired: the requested offset was already evicted. The returned
	// *_offset is still the correct cursor to continue from (do not reset to 0).
	StdoutOffsetExpired bool                   `json:"stdout_offset_expired,omitempty"`
	StderrOffsetExpired bool                   `json:"stderr_offset_expired,omitempty"`
	LeftoverProcesses   *pal.LeftoverProcesses `json:"leftover_processes,omitempty"`
}

var ProcessOutputSchema = api.ToolDefinition{
	Name: "process_output",
	Description: "- Retrieves output from a running or completed background process\n" +
		"- process_id comes from: execute_bash with run_in_background:true, a task_timeout promotion (timeout_action:'background'), or a sync run that exited but left descendants alive (handover)\n" +
		"- Default (no stdout_offset/stderr_offset/tail_lines) returns the TAIL of each stream: at most the last 1000 lines and at most 50 KB - i.e. the latest result, not a repeated 20% head\n" +
		"- Incremental polling: pass back the stdout_offset/stderr_offset you got last time to read only new bytes; pass 0 to re-read what the buffer still retains\n" +
		"- tail_lines: at most the last N lines (max 1000) of both streams; do not combine tail_lines with *_offset (mutually exclusive)\n" +
		"- include_output:false skips the output entirely (zero-copy) and just advances the cursors - ideal for wait loops\n" +
		"- block:true (default) waits for the process (and any surviving descendants) to finish, up to timeout ms; timeout > 60000 is clamped to 60000, timeout <= 0 returns immediately\n" +
		"- Output is bounded by the stream ring buffer and by a 1000 lines / 50 KB per-call truncation limit; markers such as '... (N bytes omitted) ...' are inserted where text was dropped\n" +
		"- Never infer the next cursor from len(stdout): use the returned stdout_offset/stderr_offset (they account for dropped markers and rune alignment)\n" +
		"- running is true while the main process lives OR surviving descendants exist; exit_code is null in both those cases\n" +
		"- Process IDs are only valid while the server session is active; they are lost on server restart",
	InputSchema: map[string]interface{}{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]interface{}{
			"process_id": map[string]interface{}{
				"type":        "string",
				"description": "The process ID returned by execute_bash (background start, timeout promotion, or sync handover). Opaque string, shape like proc_1.",
			},
			"block": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether to wait for process completion (and for surviving descendants to exit). Defaults to true.",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Max wait time in ms when block:true. Defaults to 30000, values > 60000 are clamped to 60000, values <= 0 return immediately. Note: actual effective wait time is also limited by the client's transport request timeout (typically 30-65s); if you need to wait longer, use block:false and poll repeatedly.",
			},
			"stdout_offset": map[string]interface{}{
				"type":        "integer",
				"minimum":     0,
				"description": "Explicit incremental read start for stdout (omit for the default tail mode). Page boundaries may fall inside a line; resume from the returned stdout_offset to concatenate pages byte-exactly.",
			},
			"stderr_offset": map[string]interface{}{
				"type":        "integer",
				"minimum":     0,
				"description": "Explicit incremental read start for stderr (omit for the default tail mode).",
			},
			"tail_lines": map[string]interface{}{
				"type":        "integer",
				"minimum":     1,
				"maximum":     1000,
				"description": "Return at most the last N lines of both streams (values > 1000 are clamped). Mutually exclusive with stdout_offset/stderr_offset.",
			},
			"include_output": map[string]interface{}{
				"type":        "boolean",
				"description": "Defaults to true. When false, no output is returned at all (zero-copy) and the cursors advance to the latest position; use tail_lines or an earlier *_offset afterwards to read results.",
			},
		},
		"required":             []string{"process_id"},
		"additionalProperties": false,
	},
}

// processOutputParams mirrors the schema; pointers distinguish "absent" from "zero".
type processOutputParams struct {
	ProcessID     string `json:"process_id"`
	Block         *bool  `json:"block"`
	Timeout       *int   `json:"timeout"`
	StdoutOffset  *int64 `json:"stdout_offset"`
	StderrOffset  *int64 `json:"stderr_offset"`
	TailLines     *int   `json:"tail_lines"`
	IncludeOutput *bool  `json:"include_output"`
}

func invalidProcessOutputParams(format string, args ...interface{}) error {
	return &api.TSPError{
		Code:    api.ErrInvalidParams,
		Message: fmt.Sprintf(format, args...),
	}
}

// streamRead is the per-stream outcome of one process_output call.
type streamRead struct {
	text     string
	offset   int64
	clipped  bool
	overflow bool
	expired  bool
}

// readStream applies the mode for one stream:
//
//	tail_lines set   -> tail mode with N lines
//	offset set       -> prefix mode from offset
//	neither          -> default tail mode (1000 lines)
func readStream(buf *api.ProcBuffer, offset *int64, tailLines int, hasTail bool) streamRead {
	if offset != nil && !hasTail {
		text, next, clipped, overflow, expired, _ := renderPrefix(buf, *offset)
		return streamRead{text: text, offset: next, clipped: clipped, overflow: overflow, expired: expired}
	}
	// Tail modes: snapshot the cursor BEFORE rendering so that any output landing
	// in between is re-read next time (duplicate) rather than silently skipped.
	total := buf.Stats().TotalBytes
	lines := maxOutputLines
	if hasTail {
		lines = tailLines
	}
	text, clipped, overflow := renderTail(buf, lines)
	return streamRead{text: text, offset: total, clipped: clipped, overflow: overflow}
}

func ProcessOutputHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	var p processOutputParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, invalidProcessOutputParams("invalid params: %v", err)
	}

	if p.ProcessID == "" {
		return nil, invalidProcessOutputParams("process_id is required")
	}
	if p.StdoutOffset != nil && *p.StdoutOffset < 0 {
		return nil, invalidProcessOutputParams("stdout_offset must be >= 0, got %d", *p.StdoutOffset)
	}
	if p.StderrOffset != nil && *p.StderrOffset < 0 {
		return nil, invalidProcessOutputParams("stderr_offset must be >= 0, got %d", *p.StderrOffset)
	}

	hasTail := p.TailLines != nil
	tailLines := maxOutputLines
	if hasTail {
		if *p.TailLines <= 0 {
			return nil, invalidProcessOutputParams("tail_lines must be >= 1, got %d", *p.TailLines)
		}
		tailLines = *p.TailLines
		if tailLines > maxOutputLines {
			tailLines = maxOutputLines
		}
		if p.StdoutOffset != nil || p.StderrOffset != nil {
			return nil, invalidProcessOutputParams("tail_lines is mutually exclusive with stdout_offset/stderr_offset")
		}
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

	includeOutput := true
	if p.IncludeOutput != nil {
		includeOutput = *p.IncludeOutput
	}

	bp, ok := api.GlobalProcessRegistry.Get(p.ProcessID)
	if !ok {
		return nil, &api.TSPError{
			Code:    api.ErrNotFound,
			Message: fmt.Sprintf("process %q not found", p.ProcessID),
		}
	}

	if block {
		// Absolute deadline: the leftover-polling stage must NOT restart the
		// clock, otherwise handed-over entries can block past the timeout.
		deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
		for !(bp.IsDone() && !bp.HasLeftoverProcesses()) {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				break
			}
			slice := 100 * time.Millisecond
			if remaining < slice {
				slice = remaining
			}
			if !bp.IsDone() {
				select {
				case <-bp.WaitChan():
				case <-time.After(slice):
				}
			} else {
				time.Sleep(slice)
			}
		}
	}

	hasLeftover := false
	var leftover *pal.LeftoverProcesses
	if bp.IsDone() {
		if bp.HasLeftoverProcesses() {
			leftover = bp.EnumerateLeftoverProcesses()
			if leftover != nil && leftover.Alive > 0 {
				hasLeftover = true
			} else {
				leftover = nil
			}
		}
		if !hasLeftover {
			bp.WaitIO(100 * time.Millisecond)
		}
	}

	running := !bp.IsDone() || hasLeftover
	var exitCodePtr *int
	if !running {
		ec := bp.GetExitCode()
		exitCodePtr = &ec
	}

	res := ProcessOutputResult{
		ProcessID:         p.ProcessID,
		Running:           running,
		ExitCode:          exitCodePtr,
		LeftoverProcesses: leftover,
	}

	if !includeOutput {
		// Zero-copy status read: no truncation, no expiry evaluation.
		stdoutStats := bp.Stdout.Stats()
		stderrStats := bp.Stderr.Stats()
		res.StdoutOffset = stdoutStats.TotalBytes
		res.StderrOffset = stderrStats.TotalBytes
		if p.StdoutOffset != nil && *p.StdoutOffset > res.StdoutOffset {
			res.StdoutOffset = *p.StdoutOffset
		}
		if p.StderrOffset != nil && *p.StderrOffset > res.StderrOffset {
			res.StderrOffset = *p.StderrOffset
		}
		res.Truncated = stdoutStats.Overflow || stderrStats.Overflow
		res.StdoutOverflow = stdoutStats.Overflow
		res.StderrOverflow = stderrStats.Overflow
		return res, nil
	}

	stdoutRead := readStream(bp.Stdout, p.StdoutOffset, tailLines, hasTail)
	stderrRead := readStream(bp.Stderr, p.StderrOffset, tailLines, hasTail)

	res.Stdout = stdoutRead.text
	res.Stderr = stderrRead.text
	res.StdoutOffset = stdoutRead.offset
	res.StderrOffset = stderrRead.offset
	res.StdoutOverflow = stdoutRead.overflow
	res.StderrOverflow = stderrRead.overflow
	res.StdoutOffsetExpired = stdoutRead.expired
	res.StderrOffsetExpired = stderrRead.expired
	res.Truncated = stdoutRead.clipped || stderrRead.clipped || stdoutRead.overflow || stderrRead.overflow
	return res, nil
}
