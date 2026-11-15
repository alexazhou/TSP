package unit_test

// 本文件钉住 `gtsp/doc/design/process-output-optimization.md` §1 列出的四条原缺陷。
//
// 断言一律按**修复后的期望行为**书写，因此在 `efe6b3a`（改前）上必须**全红**，
// 改后必须全绿 —— 这是任务 #260「改前必红」的承重证据，不是「改后绿」的自证。
//
//   缺陷 1：ProcBuffer 写满即丢弃新数据（上限 256 KB，尾部结果在源头永久丢失）
//   缺陷 2：truncateOutput 纯保头丢尾（只留前 1000 行 / 50 KB）
//   缺陷 3：按字节切 s[:maxOutputBytes] 会切断多字节 UTF-8 字符
//   缺陷 4：超时转后台时 BashBackgroundResult 直接吐原始缓冲区（可达 ~512 KB）
//
// 用唯一 marker 判定，不依赖时间与行号，避免「恰好相等」式恒真断言。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"gTSP/src/api"
	"gTSP/src/tools"
)

// 缺陷 1：缓冲写满后必须保留**最新**字节，而不是把后来的数据全丢掉。
//
// 600 KB 写入量 > 新旧两版上限（旧 256 KB / 新 512 KB），所以：
//   - 改前：保留最前 256 KB，尾 marker 缺席 ⇒ 红
//   - 改后：保留最后 512 KB，头 marker 被覆盖、尾 marker 在 ⇒ 绿
func TestDefect1_BufferKeepsNewestDataNotOldest(t *testing.T) {
	b := &api.ProcBuffer{}

	if _, err := b.Write([]byte("HEAD_MARKER\n")); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	// 填充到远超任何一版上限的总量。
	chunk := bytes.Repeat([]byte("x"), 64*1024)
	for i := 0; i < 10; i++ {
		if _, err := b.Write(chunk); err != nil {
			t.Fatalf("filler write %d failed: %v", i, err)
		}
	}
	if _, err := b.Write([]byte("\nTAIL_MARKER\n")); err != nil {
		t.Fatalf("last write failed: %v", err)
	}

	got := b.String()
	if !strings.Contains(got, "TAIL_MARKER") {
		t.Errorf("缺陷 1 未修复：最新写入的 TAIL_MARKER 被丢弃（缓冲只保留最前数据），保留长度 %d", len(got))
	}
	if strings.Contains(got, "HEAD_MARKER") {
		t.Errorf("缺陷 1 未修复：最老的 HEAD_MARKER 仍在缓冲里，说明写满后没有按环形覆盖淘汰旧数据")
	}
	if !b.Truncated() {
		t.Errorf("写满并覆盖后 Truncated() 应为 true")
	}
}

// 缺陷 2：行数/字节截断必须同时保住头部与尾部（Head+Tail），而不是只保头。
func TestDefect2_TruncationKeepsTail(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	// 1200 行（> maxOutputLines=1000），最后一行是 END_MARKER。
	params := json.RawMessage(`{
		"command": "i=1; while [ $i -le 1200 ]; do echo \"line $i\"; i=$((i+1)); done; echo END_MARKER"
	}`)
	res, err := tools.ExecuteBashHandler(session, params)
	if err != nil {
		t.Fatalf("ExecuteBashHandler failed: %v", err)
	}
	result, ok := res.(tools.ExecuteBashResult)
	if !ok {
		t.Fatalf("expected ExecuteBashResult, got %T", res)
	}
	if !result.Truncated {
		t.Errorf("1200 行 / 末行 END_MARKER 的场景应标记 truncated")
	}
	if !strings.Contains(result.Stdout, "END_MARKER") {
		t.Errorf("缺陷 2 未修复：尾部结果 END_MARKER 丢失（纯保头丢尾），stdout 末尾 120 字节=%q",
			tailBytes(result.Stdout, 120))
	}
}

// 缺陷 3：截断边界不得切断多字节 UTF-8 字符。
func TestDefect3_TruncationNeverSplitsUTF8(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	// 20000 个 '中'（3 字节）= 60000 B > 50 KB；旧实现在 51200 字节处硬切，
	// 51200 % 3 != 0 ⇒ 必然切断一个 rune。
	params := json.RawMessage(`{
		"command": "i=0; while [ $i -lt 20000 ]; do printf '中'; i=$((i+1)); done; echo; echo TAIL_MARKER"
	}`)
	res, err := tools.ExecuteBashHandler(session, params)
	if err != nil {
		t.Fatalf("ExecuteBashHandler failed: %v", err)
	}
	result, ok := res.(tools.ExecuteBashResult)
	if !ok {
		t.Fatalf("expected ExecuteBashResult, got %T", res)
	}
	if !utf8.ValidString(result.Stdout) {
		t.Errorf("缺陷 3 未修复：stdout 不是合法 UTF-8（存在被切断的多字节字符），长度 %d，末尾字节=%q",
			len(result.Stdout), tailBytes(result.Stdout, 12))
	}
	if !strings.Contains(result.Stdout, "TAIL_MARKER") {
		t.Errorf("缺陷 3 场景下尾部 marker 也应保留，stdout 末尾=%q", tailBytes(result.Stdout, 120))
	}
	if len(result.Stdout) > 50*1024 {
		t.Errorf("截断后 stdout 必须 <= 50 KB，实际 %d 字节", len(result.Stdout))
	}
}

// 缺陷 4：超时转后台时返回的 stdout/stderr 必须同样经过截断，不能整坨吐出。
func TestDefect4_TimeoutBackgroundOutputIsTruncated(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	// 2000 行 * ~40 B ≈ 80 KB（> 50 KB 上限，< 256 KB 旧缓冲上限），然后挂住等超时。
	params := json.RawMessage(`{
		"command": "i=0; while [ $i -lt 2000 ]; do echo \"-------------------------------- padding line $i\"; i=$((i+1)); done; sleep 30",
		"task_timeout": 1
	}`)
	res, err := tools.ExecuteBashHandler(session, params)
	if err != nil {
		t.Fatalf("ExecuteBashHandler failed: %v", err)
	}
	bg, ok := res.(tools.BashBackgroundResult)
	if !ok {
		t.Fatalf("expected BashBackgroundResult, got %T", res)
	}
	defer func() {
		if _, err := tools.ProcessStopHandler(session, json.RawMessage(fmt.Sprintf(`{"process_id":%q}`, bg.ProcessID))); err != nil {
			t.Logf("cleanup process_stop failed: %v", err)
		}
	}()

	if len(bg.Stdout) > 50*1024 {
		t.Errorf("缺陷 4 未修复：超时转后台返回 %d 字节 stdout（上限应为 50 KB），未做截断",
			len(bg.Stdout))
	}
	if !strings.Contains(bg.Stdout, "omitted") && !strings.Contains(bg.Stdout, "truncated") {
		t.Errorf("超时转后台的截断输出应包含省略提示 marker，实际末尾=%q", tailBytes(bg.Stdout, 200))
	}
}

func tailBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
