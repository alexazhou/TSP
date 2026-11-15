package unit_test

// 流首字节为 UTF-8 continuation byte（0x80-0xBF）时，工具层三条读取路径
// （同步 Head+Tail 结果 / prefix 第一页 / tail 模式）都不许丢掉开头字节，
// 也不许把「对齐跳过」报成 "bytes evicted by buffer"。
//
// 背景（#265 / backlog B22）：流的首字节可以是非法的 UTF-8 起始字节——
// 二进制输出、GBK 日志、或上游只写了半个 rune。旧实现从 offset 0 起就做
// `alignCeil`（跳过 continuation byte），把流开头 1-3 字节当成「某个不存在
// 字符的后半截」永久丢掉，且因为 overflow 未置位，marker 文案与实际原因不符。

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"gTSP/src/tools"
)

// invalidUTF8HeadCmd 输出 0xB8 0xAD（两个 continuation byte）+ ASCII 正文。
// 注意：这里刻意用八进制转义，避免在源码里写裸非法字节。
const invalidUTF8HeadCmd = `printf '\270\255abcdef\n'`

// invalidUTF8Head 是上面命令的前两个字节。
const invalidUTF8Head = "\xb8\xad"

func TestInvalidUTF8StreamHeadIsNotDropped(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	t.Run("sync_result", func(t *testing.T) {
		req, err := json.Marshal(map[string]interface{}{"command": invalidUTF8HeadCmd})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := tools.ExecuteBashHandler(session, req)
		if err != nil {
			t.Fatalf("ExecuteBashHandler failed: %v", err)
		}
		res, ok := raw.(tools.ExecuteBashResult)
		if !ok {
			t.Fatalf("expected ExecuteBashResult, got %T", raw)
		}
		if !strings.HasPrefix(res.Stdout, invalidUTF8Head) {
			t.Fatalf("stream head lost: stdout starts with %q, want prefix %q", firstRunes(res.Stdout, 8), invalidUTF8Head)
		}
		if strings.Contains(res.Stdout, "evicted by buffer") {
			t.Fatalf("head bytes were NOT evicted by the ring buffer; marker is misleading: %q", res.Stdout)
		}
		if !strings.Contains(res.Stdout, "abcdef") {
			t.Fatalf("body lost: %q", res.Stdout)
		}
	})

	t.Run("prefix_first_page", func(t *testing.T) {
		id := startBackground(t, session, invalidUTF8HeadCmd)
		waitFor(t, session, id)

		page, err := callProcessOutput(t, session, map[string]interface{}{
			"process_id":    id,
			"block":         false,
			"stdout_offset": 0,
		})
		if err != nil {
			t.Fatalf("process_output failed: %v", err)
		}
		if strings.Contains(page.Stdout, "skipped for UTF-8 alignment") {
			t.Fatalf("offset 0 must not be treated as mid-character: %q", page.Stdout)
		}
		if !strings.HasPrefix(page.Stdout, invalidUTF8Head) {
			t.Fatalf("page 1 lost the stream head: %q (want prefix %q)", firstRunes(page.Stdout, 8), invalidUTF8Head)
		}
		if !strings.Contains(page.Stdout, "abcdef") {
			t.Fatalf("body lost: %q", page.Stdout)
		}
	})

	t.Run("tail_mode", func(t *testing.T) {
		id := startBackground(t, session, invalidUTF8HeadCmd)
		waitFor(t, session, id)

		tail, err := callProcessOutput(t, session, map[string]interface{}{
			"process_id": id,
			"block":      false,
		})
		if err != nil {
			t.Fatalf("process_output failed: %v", err)
		}
		if strings.Contains(tail.Stdout, "earlier output omitted/evicted") {
			t.Fatalf("whole stream fits; head must not be reported as omitted: %q", tail.Stdout)
		}
		if !strings.HasPrefix(tail.Stdout, invalidUTF8Head) {
			t.Fatalf("tail lost the stream head: %q (want prefix %q)", firstRunes(tail.Stdout, 8), invalidUTF8Head)
		}
		if !strings.Contains(tail.Stdout, "abcdef") {
			t.Fatalf("body lost: %q", tail.Stdout)
		}
	})
}

// TestMidStreamUTF8AlignmentStillWorks 反向确认：修复只针对流起点，
// 从流中间切页时仍然按 rune 边界对齐（否则上面三个子测试在「把对齐删光」
// 的实现下也会绿）。
func TestMidStreamUTF8AlignmentStillWorks(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	// 400 行 × 185 B ≈ 74 KB，超过单页 50 KB 预算，prefix 模式必然跨页。
	cmd := `awk 'BEGIN{s=""; for(j=0;j<60;j++) s=s "中"; for(i=0;i<400;i++) printf "%03d-%s\n", i, s}'`
	id := startBackground(t, session, cmd)
	total := waitFor(t, session, id).StdoutOffset

	page, err := callProcessOutput(t, session, map[string]interface{}{
		"process_id":    id,
		"block":         false,
		"stdout_offset": 0,
	})
	if err != nil {
		t.Fatalf("process_output failed: %v", err)
	}
	if page.StdoutOffset <= 0 || page.StdoutOffset >= total {
		t.Fatalf("expected a multi-page split, got offset=%d total=%d", page.StdoutOffset, total)
	}

	// 第二页的起点落在字符边界上：不许出现 "skipped for UTF-8 alignment"。
	page2, err := callProcessOutput(t, session, map[string]interface{}{
		"process_id":    id,
		"block":         false,
		"stdout_offset": page.StdoutOffset,
	})
	if err != nil {
		t.Fatalf("process_output failed: %v", err)
	}
	if strings.Contains(page2.Stdout, "skipped for UTF-8 alignment") {
		t.Fatalf("a rune-aligned page start must not skip anything: %q", firstRunes(page2.Stdout, 60))
	}
	if !utf8.ValidString(page2.Stdout) {
		t.Fatalf("page 2 is not valid UTF-8: %q", firstRunes(page2.Stdout, 60))
	}
}

func firstRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
