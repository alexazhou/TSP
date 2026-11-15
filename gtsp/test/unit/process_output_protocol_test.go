package unit_test

// process_output 的协议级判据：分页游标、len(stdout) 不可推算游标、尾部模式、
// include_output 零拷贝、参数校验、绝对 deadline、handover/后台新字段、schema 断言。

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"gTSP/src/api"
	"gTSP/src/tools"
)

// --------------------------------------------------------------------------- //
// helpers
// --------------------------------------------------------------------------- //

func callProcessOutput(t *testing.T, session api.Session, params map[string]interface{}) (tools.ProcessOutputResult, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	res, err := tools.ProcessOutputHandler(session, raw)
	if err != nil {
		return tools.ProcessOutputResult{}, err
	}
	out, ok := res.(tools.ProcessOutputResult)
	if !ok {
		t.Fatalf("expected ProcessOutputResult, got %T", res)
	}
	return out, nil
}

func mustProcessOutput(t *testing.T, session api.Session, params map[string]interface{}) tools.ProcessOutputResult {
	t.Helper()
	out, err := callProcessOutput(t, session, params)
	if err != nil {
		t.Fatalf("process_output(%v) failed: %v", params, err)
	}
	return out
}

// startBackground 起一个后台进程并返回 process_id。
func startBackground(t *testing.T, session api.Session, command string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]interface{}{"command": command, "run_in_background": true})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	res, err := tools.ExecuteBashHandler(session, raw)
	if err != nil {
		t.Fatalf("execute_bash failed: %v", err)
	}
	bg, ok := res.(tools.BashBackgroundResult)
	if !ok {
		t.Fatalf("expected BashBackgroundResult, got %T", res)
	}
	t.Cleanup(func() {
		_, _ = tools.ProcessStopHandler(session, json.RawMessage(fmt.Sprintf(`{"process_id":%q}`, bg.ProcessID)))
	})
	return bg.ProcessID
}

// waitFor 等进程结束并返回最终快照。
func waitFor(t *testing.T, session api.Session, id string) tools.ProcessOutputResult {
	t.Helper()
	out := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": id, "block": true, "timeout": 20000,
	})
	if out.Running {
		t.Fatalf("process %s still running after 20s", id)
	}
	return out
}

// fixedWidthLines 期望的输出流：20000 行 * 8 B = 160000 B。
func fixedWidthLines(n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(fmt.Sprintf("L%06d\n", i))
	}
	return sb.String()
}

// stripTrailingMarker 去掉分页尾 marker，便于比较正文。
func stripTrailingMarker(s string) string {
	if idx := strings.LastIndex(s, "\n... ("); idx >= 0 {
		return s[:idx]
	}
	return s
}

const fixedWidthCmd = `i=0; while [ $i -lt 20000 ]; do printf 'L%06d\n' $i; i=$((i+1)); done`

// --------------------------------------------------------------------------- //
// 分页游标
// --------------------------------------------------------------------------- //

// TestProcessOutputPaginationCursors 断言游标严格递增、终值 == totalBytes、页数有界。
func TestProcessOutputPaginationCursors(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	id := startBackground(t, session, fixedWidthCmd)
	waitFor(t, session, id)

	stats := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": id, "include_output": false,
	})
	total := stats.StdoutOffset
	want := int64(len(fixedWidthLines(20000)))
	if total != want {
		t.Fatalf("totalBytes = %d, want %d", total, want)
	}

	offset := int64(0)
	pages := 0
	for offset < total {
		page := mustProcessOutput(t, session, map[string]interface{}{
			"process_id": id, "stdout_offset": offset, "block": false,
		})
		if page.StdoutOffset <= offset {
			t.Fatalf("page %d: cursor did not advance: %d -> %d", pages, offset, page.StdoutOffset)
		}
		if page.StdoutOffset > total {
			t.Fatalf("page %d: cursor %d overshoots totalBytes %d", pages, page.StdoutOffset, total)
		}
		if want := page.StdoutOffset < total; page.Truncated != want {
			t.Fatalf("page %d: truncated = %v, want %v (cursor %d of %d)",
				pages, page.Truncated, want, page.StdoutOffset, total)
		}
		offset = page.StdoutOffset
		pages++
	}
	if offset != total {
		t.Fatalf("final cursor = %d, want %d", offset, total)
	}
	// 页数有界：每页同时受 50 KB - markerBudget 与 1000 行两个上限约束。
	// 本流每行 8 B，1000 行 = 8000 B 才是实际生效的窗口，故按行数算下界。
	byBytes := int(total/(maxOutputBytesForTest-markerBudgetForTest)) + 2
	byLines := 20000/maxOutputLinesForTest + 2
	maxPages := byBytes
	if byLines > maxPages {
		maxPages = byLines
	}
	if pages > maxPages {
		t.Errorf("pagination used %d pages, want <= %d (window is bounded)", pages, maxPages)
	}
}

const (
	maxOutputBytesForTest = 50 * 1024
	markerBudgetForTest   = 256
	maxOutputLinesForTest = 1000
)

// TestProcessOutputCursorIsNotLenStdout 是本单要求的「实测差异」：
// 用返回的 *_offset 续读能正确拼接；用 len(stdout) 推算会跳过 marker 的那些字节。
func TestProcessOutputCursorIsNotLenStdout(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	id := startBackground(t, session, fixedWidthCmd)
	waitFor(t, session, id)

	stream := fixedWidthLines(20000)

	page1 := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": id, "stdout_offset": 0, "block": false,
	})
	if !strings.Contains(page1.Stdout, "bytes remaining in buffer") {
		t.Fatalf("expected a trailing continuation marker on page 1, got tail %.120s", page1.Stdout)
	}
	body1 := stripTrailingMarker(page1.Stdout)
	if !strings.HasPrefix(stream, body1) {
		t.Fatalf("page 1 body is not a prefix of the expected stream")
	}

	// 正确用法：用返回的游标续读 ⇒ 正文恰是流的下一个片段。
	page2 := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": id, "stdout_offset": page1.StdoutOffset, "block": false,
	})
	body2 := stripTrailingMarker(page2.Stdout)
	want2 := stream[page1.StdoutOffset:]
	if !strings.HasPrefix(want2, body2) {
		t.Fatalf("resuming from the returned offset did not continue the stream")
	}

	// 错误用法：用 len(stdout) 推算 ⇒ 游标越过 marker 长度，中间那些字节永久丢失。
	naive := int64(len(page1.Stdout))
	if naive <= page1.StdoutOffset {
		t.Fatalf("marker made len(stdout) (%d) not larger than the true cursor (%d); test would be vacuous",
			naive, page1.StdoutOffset)
	}
	gap := naive - page1.StdoutOffset
	if gap <= 0 || naive > int64(len(stream)) {
		t.Fatalf("unexpected gap: naive=%d correct=%d", naive, page1.StdoutOffset)
	}
	page3 := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": id, "stdout_offset": naive, "block": false,
	})
	body3 := stripTrailingMarker(page3.Stdout)
	if strings.HasPrefix(stream[page1.StdoutOffset:], body3) && strings.HasPrefix(body3, body2) {
		t.Fatalf("len(stdout)-derived cursor produced the same continuation; the difference is not observable")
	}
	t.Logf("实测差异：正确游标=%d，len(stdout)推算=%d，被跳过的字节=%d（marker 长度）",
		page1.StdoutOffset, naive, gap)
	// 被跳过的那些字节确实既不在 page1 正文里、也不在 page3 里。
	skipped := stream[page1.StdoutOffset:naive]
	if strings.Contains(page1.Stdout, skipped) || strings.Contains(page3.Stdout, skipped) {
		t.Errorf("expected the skipped bytes to be absent from both pages")
	}
}

// TestProcessOutputPaginationSingleHugeLine 单行 1 MB：cursor 必须严格前进、终值 == totalBytes。
func TestProcessOutputPaginationSingleHugeLine(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	// 1048576 个 'x'，中间没有换行 ⇒ 行数上限永远不生效，只能靠字节窗口推进。
	id := startBackground(t, session, `head -c 1048576 /dev/zero | tr '\0' 'x'`)
	waitFor(t, session, id)

	stats := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": id, "include_output": false,
	})
	total := stats.StdoutOffset
	if total != 1048576 {
		t.Fatalf("totalBytes = %d, want 1048576", total)
	}

	offset := int64(0)
	pages := 0
	for offset < total {
		page := mustProcessOutput(t, session, map[string]interface{}{
			"process_id": id, "stdout_offset": offset, "block": false,
		})
		if page.StdoutOffset <= offset {
			t.Fatalf("page %d: cursor must strictly advance for a 1 MB single line: %d -> %d",
				pages, offset, page.StdoutOffset)
		}
		offset = page.StdoutOffset
		pages++
		if pages > 100 {
			t.Fatalf("single 1 MB line took %d pages, window is not bounded", pages)
		}
	}
	if offset != total {
		t.Fatalf("final cursor = %d, want %d", offset, total)
	}
}

// TestProcessOutputOffsetBeyondTotalEchoes 越界 offset 必须原样回显（游标不倒退）。
func TestProcessOutputOffsetBeyondTotalEchoes(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	id := startBackground(t, session, `echo hi`)
	waitFor(t, session, id)

	out := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": id, "stdout_offset": 999999, "block": false,
	})
	if out.StdoutOffset != 999999 {
		t.Errorf("offset beyond totalBytes must be echoed back, got %d", out.StdoutOffset)
	}
	if out.Stdout != "" {
		t.Errorf("offset beyond totalBytes must return empty stdout, got %q", out.Stdout)
	}
	if out.Truncated {
		t.Errorf("nothing was clipped when reading past the end, truncated must be false")
	}
}

// TestProcessOutputOffsetExpired 覆盖 ring 已淘汰区间时的过期语义。
func TestProcessOutputOffsetExpired(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	// 70000 * 8 B = 560000 B > 512 KB ⇒ 必然发生过覆盖。
	id := startBackground(t, session, `i=0; while [ $i -lt 70000 ]; do printf 'L%06d\n' $i; i=$((i+1)); done`)
	waitFor(t, session, id)

	stats := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": id, "include_output": false,
	})
	if !stats.StdoutOverflow {
		t.Fatalf("expected stdout_overflow for a >512KB stream, got %+v", stats)
	}
	if !stats.Truncated {
		t.Errorf("include_output:false must still report truncated when the buffer overflowed")
	}

	out := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": id, "stdout_offset": 0, "block": false,
	})
	if !out.StdoutOffsetExpired {
		t.Errorf("offset 0 must be reported as expired after 560 KB of output")
	}
	if !strings.Contains(out.Stdout, "evicted by buffer before this point") {
		t.Errorf("expired pagination must explain the eviction, got head %.120s", out.Stdout)
	}
	if out.StdoutOffset <= 0 {
		t.Errorf("expired read must still return a usable cursor, got %d", out.StdoutOffset)
	}
}

// --------------------------------------------------------------------------- //
// 尾部模式 / tail_lines
// --------------------------------------------------------------------------- //

// TestProcessOutputDefaultTailMode 默认模式返回尾部，而不是 20% 头部。
func TestProcessOutputDefaultTailMode(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	id := startBackground(t, session, `i=1; while [ $i -le 3000 ]; do echo "line $i"; i=$((i+1)); done; echo FINAL_RESULT`)
	waitFor(t, session, id)

	out := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": id, "block": false,
	})
	if !strings.Contains(out.Stdout, "FINAL_RESULT") {
		t.Fatalf("default mode must return the tail (FINAL_RESULT missing), tail=%.200s", tailOf(out.Stdout, 200))
	}
	if strings.Contains(out.Stdout, "line 1\n") {
		t.Errorf("default mode must not include the head of the stream")
	}
	if len(out.Stdout) > maxOutputBytesForTest {
		t.Errorf("default mode must stay within 50 KB, got %d", len(out.Stdout))
	}
}

// TestProcessOutputTailLinesAndCarriageReturns tail_lines 只按 \n 分行，\r 进度条不拆行。
func TestProcessOutputTailLinesAndCarriageReturns(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	// 进度条：三行内容用 \r 覆盖，最后再给两个正常行。
	id := startBackground(t, session, `printf 'progress 10%%\rprogress 55%%\rprogress 100%%\n'; echo keep1; echo keep2`)
	waitFor(t, session, id)

	out := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": id, "tail_lines": 2, "block": false,
	})
	if !strings.HasSuffix(out.Stdout, "keep1\nkeep2\n") {
		t.Errorf("tail_lines=2 must end with the last two \\n-lines, got %q", out.Stdout)
	}
	if strings.Contains(out.Stdout, "progress 100%") {
		t.Errorf("tail_lines=2 must not include the dropped earlier lines, got %q", out.Stdout)
	}
	if !strings.Contains(out.Stdout, "earlier output omitted/evicted") {
		t.Errorf("dropped earlier bytes must be summarised by a marker, got %q", out.Stdout)
	}

	out3 := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": id, "tail_lines": 3, "block": false,
	})
	if !strings.Contains(out3.Stdout, "progress 100%") {
		t.Errorf("the \\r progress line must stay one line, got %q", out3.Stdout)
	}
	if n := strings.Count(out3.Stdout, "\n"); n > 3 {
		t.Errorf("\\r must not create extra lines, got %d newlines in %q", n, out3.Stdout)
	}

	// tail_lines 超过上限被静默钳制到 1000（不报错）。
	outBig := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": id, "tail_lines": 5000, "block": false,
	})
	if !strings.Contains(outBig.Stdout, "keep2") {
		t.Errorf("clamped tail_lines must still return the tail, got %q", outBig.Stdout)
	}
}

// --------------------------------------------------------------------------- //
// include_output: false 零拷贝
// --------------------------------------------------------------------------- //

// TestProcessOutputIncludeOutputFalseIsZeroCopy 用 AllocsPerRun 给数字，不口头声称。
func TestProcessOutputIncludeOutputFalseIsZeroCopy(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	id := startBackground(t, session, `i=0; while [ $i -lt 5000 ]; do printf 'L%06d\n' $i; i=$((i+1)); done`)
	waitFor(t, session, id)

	paramsOff := map[string]interface{}{"process_id": id, "block": false, "include_output": false}
	rawOff, err := json.Marshal(paramsOff)
	if err != nil {
		t.Fatal(err)
	}
	out := mustProcessOutput(t, session, paramsOff)
	if out.Stdout != "" || out.Stderr != "" {
		t.Fatalf("include_output:false must return no text, got %d/%d bytes", len(out.Stdout), len(out.Stderr))
	}

	allocsStatus := testing.AllocsPerRun(50, func() {
		if _, err := tools.ProcessOutputHandler(session, rawOff); err != nil {
			t.Fatalf("handler failed: %v", err)
		}
	})

	paramsOn := map[string]interface{}{"process_id": id, "block": false}
	rawOn, err := json.Marshal(paramsOn)
	if err != nil {
		t.Fatal(err)
	}
	allocsWithText := testing.AllocsPerRun(50, func() {
		if _, err := tools.ProcessOutputHandler(session, rawOn); err != nil {
			t.Fatalf("handler failed: %v", err)
		}
	})

	// 状态查询不复制输出：随输出量增长的分配必须没有。
	// 阈值取 40：一次 40 KB 文本拷贝 + marker 组装的分配数远高于此。
	if allocsStatus > 40 {
		t.Errorf("include_output:false allocated %.1f times per run, want <= 40 (no output copying)", allocsStatus)
	}
	if allocsWithText <= allocsStatus {
		t.Errorf("text mode should allocate more than status mode (%.1f vs %.1f); test would be vacuous",
			allocsWithText, allocsStatus)
	}
	t.Logf("AllocsPerRun: include_output:false = %.1f, 默认尾部模式 = %.1f", allocsStatus, allocsWithText)

	// 分配字节数才是零拷贝的硬证据：状态查询不应随输出量线性增长。
	perRunBytes := func(raw json.RawMessage) float64 {
		var before, after runtime.MemStats
		const runs = 20
		runtime.GC()
		runtime.ReadMemStats(&before)
		for i := 0; i < runs; i++ {
			if _, err := tools.ProcessOutputHandler(session, raw); err != nil {
				t.Fatalf("handler failed: %v", err)
			}
		}
		runtime.ReadMemStats(&after)
		return float64(after.TotalAlloc-before.TotalAlloc) / runs
	}
	statusBytes := perRunBytes(rawOff)
	textBytes := perRunBytes(rawOn)
	t.Logf("每次调用分配字节数：include_output:false = %.0f B，默认尾部模式 = %.0f B", statusBytes, textBytes)
	if statusBytes > 8*1024 {
		t.Errorf("include_output:false allocated %.0f B per call, want < 8 KB (no output copying)", statusBytes)
	}
	if textBytes < statusBytes {
		t.Errorf("tail mode should allocate at least as much as status mode (%.0f vs %.0f)", textBytes, statusBytes)
	}
}

// --------------------------------------------------------------------------- //
// 参数校验
// --------------------------------------------------------------------------- //

func TestProcessOutputInvalidParams(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	id := startBackground(t, session, `echo ok`)
	waitFor(t, session, id)

	cases := []struct {
		name   string
		params map[string]interface{}
	}{
		{"empty_process_id", map[string]interface{}{"process_id": ""}},
		{"negative_stdout_offset", map[string]interface{}{"process_id": id, "stdout_offset": -1}},
		{"negative_stderr_offset", map[string]interface{}{"process_id": id, "stderr_offset": -5}},
		{"tail_lines_zero", map[string]interface{}{"process_id": id, "tail_lines": 0}},
		{"tail_lines_negative", map[string]interface{}{"process_id": id, "tail_lines": -3}},
		{"tail_lines_with_stdout_offset", map[string]interface{}{"process_id": id, "tail_lines": 5, "stdout_offset": 0}},
		{"tail_lines_with_stderr_offset", map[string]interface{}{"process_id": id, "tail_lines": 5, "stderr_offset": 0}},
		{"stdout_offset_wrong_type", map[string]interface{}{"process_id": id, "stdout_offset": "abc"}},
		{"block_wrong_type", map[string]interface{}{"process_id": id, "block": "yes"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := callProcessOutput(t, session, tc.params)
			if err == nil {
				t.Fatalf("expected an error for %v", tc.params)
			}
			tspErr, ok := err.(*api.TSPError)
			if !ok {
				t.Fatalf("expected *api.TSPError, got %T: %v", err, err)
			}
			if tspErr.Code != api.ErrInvalidParams {
				t.Errorf("error code = %q, want %q", tspErr.Code, api.ErrInvalidParams)
			}
		})
	}
}

// --------------------------------------------------------------------------- //
// 绝对 deadline
// --------------------------------------------------------------------------- //

// TestProcessOutputBlockHonorsAbsoluteDeadlineWithLeftover
// handover 场景下 block:true 的等待必须服从同一个绝对 deadline，
// 不能因为「先等主进程、再等后代」而把 timeout 重置一次。
func TestProcessOutputBlockHonorsAbsoluteDeadlineWithLeftover(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	raw, err := json.Marshal(map[string]interface{}{
		"command":      "sleep 25 & echo handed_over",
		"task_timeout": 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tools.ExecuteBashHandler(session, raw)
	if err != nil {
		t.Fatalf("execute_bash failed: %v", err)
	}
	handover, ok := res.(tools.ExecuteBashResult)
	if !ok || handover.ProcessID == "" {
		t.Fatalf("expected a handover result with process_id, got %T", res)
	}
	t.Cleanup(func() {
		_, _ = tools.ProcessStopHandler(session, json.RawMessage(fmt.Sprintf(`{"process_id":%q}`, handover.ProcessID)))
	})

	start := time.Now()
	out := mustProcessOutput(t, session, map[string]interface{}{
		"process_id": handover.ProcessID, "block": true, "timeout": 700,
	})
	elapsed := time.Since(start)

	if elapsed > 4*time.Second {
		t.Errorf("block:true ignored the absolute deadline: waited %v for timeout=700ms with a 25s leftover", elapsed)
	}
	if !out.Running {
		t.Errorf("the leftover descendant is still alive, running must be true")
	}
	if out.ExitCode != nil {
		t.Errorf("exit_code must be null while descendants are alive, got %d", *out.ExitCode)
	}
	if !strings.Contains(out.Stdout, "handed_over") {
		t.Errorf("handover output must be readable, got %q", out.Stdout)
	}
}

// --------------------------------------------------------------------------- //
// execute_bash 新字段
// --------------------------------------------------------------------------- //

// TestExecuteBashHandoverAndBackgroundOffsets 覆盖 handover / 转后台的续读游标与 overflow 字段。
func TestExecuteBashHandoverAndBackgroundOffsets(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	t.Run("handover", func(t *testing.T) {
		raw, _ := json.Marshal(map[string]interface{}{
			"command":      "sleep 20 & echo handed_over",
			"task_timeout": 10,
		})
		res, err := tools.ExecuteBashHandler(session, raw)
		if err != nil {
			t.Fatalf("execute_bash failed: %v", err)
		}
		got, ok := res.(tools.ExecuteBashResult)
		if !ok || got.ProcessID == "" {
			t.Fatalf("expected handover ExecuteBashResult, got %T", res)
		}
		t.Cleanup(func() {
			_, _ = tools.ProcessStopHandler(session, json.RawMessage(fmt.Sprintf(`{"process_id":%q}`, got.ProcessID)))
		})
		if got.StdoutOffset == nil || got.StderrOffset == nil {
			t.Fatalf("handover must expose stdout_offset/stderr_offset cursors")
		}
		if *got.StdoutOffset != int64(len(got.Stdout)) && !strings.Contains(got.Stdout, "omitted") {
			t.Logf("stdout=%q cursor=%d (cursor is bytes-absolute, marker-aware)", got.Stdout, *got.StdoutOffset)
		}
	})

	t.Run("timeout_promotion_reports_offsets_and_truncation", func(t *testing.T) {
		raw, _ := json.Marshal(map[string]interface{}{
			"command":      `i=0; while [ $i -lt 3000 ]; do printf 'L%06d\n' $i; i=$((i+1)); done; sleep 20`,
			"task_timeout": 1,
		})
		res, err := tools.ExecuteBashHandler(session, raw)
		if err != nil {
			t.Fatalf("execute_bash failed: %v", err)
		}
		bg, ok := res.(tools.BashBackgroundResult)
		if !ok {
			t.Fatalf("expected BashBackgroundResult, got %T", res)
		}
		t.Cleanup(func() {
			_, _ = tools.ProcessStopHandler(session, json.RawMessage(fmt.Sprintf(`{"process_id":%q}`, bg.ProcessID)))
		})
		if len(bg.Stdout) > maxOutputBytesForTest {
			t.Errorf("promotion stdout = %d bytes, want <= 50 KB", len(bg.Stdout))
		}
		if !bg.Truncated {
			t.Errorf("promotion of a truncated stream must set truncated")
		}
		if bg.StdoutOffset == 0 {
			t.Errorf("promotion must report a continuation cursor, got 0")
		}
		if !strings.Contains(bg.Stdout, "omitted") {
			t.Errorf("promotion output must carry an omission marker, tail=%.120s", tailOf(bg.Stdout, 120))
		}
	})

	t.Run("run_in_background_returns_zero_cursor", func(t *testing.T) {
		raw, _ := json.Marshal(map[string]interface{}{"command": "sleep 5", "run_in_background": true})
		res, err := tools.ExecuteBashHandler(session, raw)
		if err != nil {
			t.Fatalf("execute_bash failed: %v", err)
		}
		bg, ok := res.(tools.BashBackgroundResult)
		if !ok {
			t.Fatalf("expected BashBackgroundResult, got %T", res)
		}
		t.Cleanup(func() {
			_, _ = tools.ProcessStopHandler(session, json.RawMessage(fmt.Sprintf(`{"process_id":%q}`, bg.ProcessID)))
		})
		if bg.Stdout != "" || bg.Stderr != "" || bg.Truncated {
			t.Errorf("immediate background start must return empty untruncated output, got %+v", bg)
		}
		if bg.StdoutOffset != 0 || bg.StderrOffset != 0 {
			t.Errorf("immediate background start must start cursors at 0, got %d/%d", bg.StdoutOffset, bg.StderrOffset)
		}
	})
}

// --------------------------------------------------------------------------- //
// schema / description
// --------------------------------------------------------------------------- //

func TestProcessOutputSchemaAndDescription(t *testing.T) {
	schema, ok := tools.ProcessOutputSchema.InputSchema.(map[string]interface{})
	if !ok {
		t.Fatalf("schema is not an object")
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema properties missing")
	}
	for _, name := range []string{"process_id", "block", "timeout", "stdout_offset", "stderr_offset", "tail_lines", "include_output"} {
		if _, ok := props[name]; !ok {
			t.Errorf("schema missing parameter %q", name)
		}
	}

	checkInt := func(name string, wantMin, wantMax interface{}) {
		spec, _ := props[name].(map[string]interface{})
		if spec == nil {
			t.Fatalf("parameter %q has no spec", name)
		}
		if spec["type"] != "integer" {
			t.Errorf("%s type = %v, want integer", name, spec["type"])
		}
		if wantMin != nil && spec["minimum"] != wantMin {
			t.Errorf("%s minimum = %v, want %v", name, spec["minimum"], wantMin)
		}
		if wantMax != nil && spec["maximum"] != wantMax {
			t.Errorf("%s maximum = %v, want %v", name, spec["maximum"], wantMax)
		}
	}
	checkInt("stdout_offset", 0, nil)
	checkInt("stderr_offset", 0, nil)
	checkInt("tail_lines", 1, 1000)

	required, _ := schema["required"].([]string)
	if len(required) != 1 || required[0] != "process_id" {
		t.Errorf("required = %v, want [process_id]", required)
	}

	desc := tools.ProcessOutputSchema.Description
	for _, want := range []string{"tail", "stdout_offset", "stderr_offset", "tail_lines", "include_output", "50 KB", "len(stdout)"} {
		if !strings.Contains(desc, want) {
			t.Errorf("process_output description must mention %q", want)
		}
	}

	bashDesc := tools.ExecuteBashSchema.Description
	for _, want := range []string{"tail", "omitted", "stdout_offset", "leftover"} {
		if !strings.Contains(bashDesc, want) {
			t.Errorf("execute_bash description must mention %q", want)
		}
	}
}

// TestProcessOutput_LosslessUTF8PaginationDoesNotMarkTruncated verifies that
// lossless prefix pagination across multi-byte UTF-8 boundaries keeps
// truncated=false instead of falsely tripping on ReadFrom's rune extension.
func TestProcessOutput_LosslessUTF8PaginationDoesNotMarkTruncated(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	// ~60 KB of 3-byte UTF-8 runes across 200 lines (< 1000 lines, < 512 KB ring).
	cmd := `awk 'BEGIN{s=""; for(j=0;j<100;j++) s=s "中"; for(i=0;i<200;i++) printf "%03d-%s\n", i, s}'`
	reqBytes, _ := json.Marshal(map[string]interface{}{"command": cmd, "run_in_background": true})
	raw, err := tools.ExecuteBashHandler(session, reqBytes)
	if err != nil {
		t.Fatal(err)
	}
	pid := raw.(tools.BashBackgroundResult).ProcessID

	// Wait for completion without reading output.
	waitRes, err := callProcessOutput(t, session, map[string]interface{}{
		"process_id":     pid,
		"block":          true,
		"timeout":        5000,
		"include_output": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if waitRes.Running || waitRes.StdoutOverflow {
		t.Fatalf("unexpected state: running=%v overflow=%v", waitRes.Running, waitRes.StdoutOverflow)
	}

	// Page 1 from offset 0: must not mark truncated=true just because a UTF-8 rune
	// straddled the page budget boundary.
	p1, err := callProcessOutput(t, session, map[string]interface{}{
		"process_id":    pid,
		"block":         false,
		"stdout_offset": 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p1.Truncated {
		t.Errorf("lossless UTF-8 pagination page 1 should have Truncated=false, got true (offset=%d)", p1.StdoutOffset)
	}
	if p1.StdoutOffset <= 0 || p1.StdoutOffset >= waitRes.StdoutOffset {
		t.Fatalf("expected multi-page split, got p1.StdoutOffset=%d total=%d", p1.StdoutOffset, waitRes.StdoutOffset)
	}

	// Page 2 from p1.StdoutOffset: finishes cleanly with Truncated=false.
	p2, err := callProcessOutput(t, session, map[string]interface{}{
		"process_id":    pid,
		"block":         false,
		"stdout_offset": p1.StdoutOffset,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p2.Truncated {
		t.Errorf("lossless UTF-8 pagination page 2 should have Truncated=false, got true")
	}
	if p2.StdoutOffset != waitRes.StdoutOffset {
		t.Errorf("p2.StdoutOffset = %d, want %d", p2.StdoutOffset, waitRes.StdoutOffset)
	}
}
