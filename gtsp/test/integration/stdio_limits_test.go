package integration_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gTSP/src/api"
	"gTSP/src/tools"
)

// testSession 返回一个已初始化的 session，便于直接调用 tool。
func testSession() api.Session {
	s := api.NewSession()
	s.SetInitialized(true)
	rule := api.PathRule{Action: "allow", Path: "/"}
	s.SetPathRules([]api.PathRule{rule}, []api.PathRule{rule})
	return s
}

// runStdioSession 把 dispatcher 跑在 io.Pipe 上，返回请求通道与响应读取函数。
// 用于验证 ServeStdio 的真实行为（bufio.Scanner 上限、EOF 后的清理）。
func runStdioSession(t *testing.T, dispatcher *api.Dispatcher, session api.Session) (*io.PipeWriter, *bufio.Reader, chan struct{}) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	client := api.NewStdioClient(inR, outW)

	done := make(chan struct{})
	go func() {
		defer close(done)
		dispatcher.ServeStdio(session, client)
		outW.Close()
	}()
	return inW, bufio.NewReader(outR), done
}

// sendLine 写入一行请求（含换行），返回写入是否成功。
func sendLine(w io.Writer, line []byte) error {
	_, err := w.Write(append(line, '\n'))
	return err
}

// waitForFile 轮询等待文件出现且非空。
//
// 后台命令是异步的：工具返回 process_id 时 shell 可能还没跑起来。
// 直接关 stdin 会测到「启动前就被清理」，看起来通过但没测到进程树。
func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("等待 %s 出现超时（%v）：后台命令可能根本没执行", path, timeout)
}

// jsonQuote 把 s 编码成 JSON 字符串字面量（含引号）。
//
// 直接拼 `"` + s + `"` 在 Windows 上会出问题：路径里的 `\` 是 JSON 转义符，
// `C:\Users\...` 会让请求变成非法 JSON，测试直接挂在一个与主题无关的解析错误上。
func jsonQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		// json.Marshal 对 string 永不失败，这里只是防御。
		return `""`
	}
	return string(b)
}

// TestStdioAcceptsRequestLargerThan64KB 覆盖回归：单行请求超过 bufio.Scanner
// 默认 64KB 上限时，旧实现会让 Scan 返回 false、Sample 退出，调用方只看到
// stdout EOF（无任何错误响应）。write_file 允许 100KB content，正常请求行
// 必然超过 64KB，因此该上限必须放宽。
func TestStdioAcceptsRequestLargerThan64KB(t *testing.T) {
	cases := []struct {
		name         string
		contentBytes int
	}{
		{"just_above_64KB", 70 * 1024},
		{"max_write_file_size", 100 * 1024},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dispatcher := api.NewDispatcher()
			dispatcher.Register("big_echo", func(s api.Session, params json.RawMessage) (interface{}, error) {
				var p struct {
					Content string `json:"content"`
				}
				if err := json.Unmarshal(params, &p); err != nil {
					return nil, err
				}
				return map[string]int{"len": len(p.Content)}, nil
			})
			session := testSession()
			session.SetInitialized(false) // 真实流程要求先 initialize

			inW, out, done := runStdioSession(t, dispatcher, session)
			defer inW.Close()

			initReq := []byte(`{"id":"0","method":"initialize","input":{"protocolVersion":"0.3"}}`)
			if err := sendLine(inW, initReq); err != nil {
				t.Fatalf("写入 initialize 失败: %v", err)
			}
			if line, err := out.ReadBytes('\n'); err != nil || len(line) == 0 {
				t.Fatalf("读取 initialize 响应失败: %v", err)
			}

			payload, _ := json.Marshal(map[string]interface{}{
				"id":     "1",
				"method": "tool",
				"tool":   "big_echo",
				"input":  map[string]string{"content": strings.Repeat("a", tc.contentBytes)},
			})
			if len(payload)+1 <= 64*1024 {
				t.Fatalf("用例无效：请求行 %d 字节没有超过 64KB 上限", len(payload)+1)
			}
			if err := sendLine(inW, payload); err != nil {
				t.Fatalf("写入 %d 字节请求行失败（说明读端已退出）: %v", len(payload)+1, err)
			}

			line, err := out.ReadBytes('\n')
			if err != nil {
				t.Fatalf("读取 %d 字节请求的响应失败（读端已关闭 = 进程退出）: %v", len(payload)+1, err)
			}
			var resp api.Response
			if err := json.Unmarshal(line, &resp); err != nil {
				t.Fatalf("响应不是合法 JSON: %v (%q)", err, string(line))
			}
			if resp.Type != "result" {
				t.Fatalf("期望 result，实际 %q", resp.Type)
			}
			result, ok := resp.Result.(map[string]interface{})
			if !ok {
				t.Fatalf("期望 map 结果，实际 %T", resp.Result)
			}
			if got := int(result["len"].(float64)); got != tc.contentBytes {
				t.Errorf("content 长度不匹配: got %d, want %d", got, tc.contentBytes)
			}

			inW.Close()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("ServeStdio 未在 stdin 关闭后退出")
			}
		})
	}
}

// TestStdioRejectsRequestLargerThanConfiguredMax 覆盖上限仍然存在（防止放宽成无上限）。
// 超限时的行为是「该行被丢弃 + 循环结束」，因此表现为无响应并退出，而不是崩溃或无限等待。
func TestStdioRejectsRequestLargerThanConfiguredMax(t *testing.T) {
	dispatcher := api.NewDispatcher()
	called := false
	dispatcher.Register("noop", func(s api.Session, params json.RawMessage) (interface{}, error) {
		called = true
		return map[string]bool{"ok": true}, nil
	})
	session := testSession()

	inW, out, done := runStdioSession(t, dispatcher, session)
	defer inW.Close()

	initReq := []byte(`{"id":"0","method":"initialize","input":{"protocolVersion":"0.3"}}`)
	if err := sendLine(inW, initReq); err != nil {
		t.Fatalf("写入 initialize 失败: %v", err)
	}
	if _, err := out.ReadBytes('\n'); err != nil {
		t.Fatalf("读取 initialize 响应失败: %v", err)
	}

	// 超过 10MB 上限
	huge := fmt.Sprintf(`{"id":"1","method":"tool","tool":"noop","input":{"pad":"%s"}}`,
		strings.Repeat("a", 11*1024*1024))
	// 读端在超限后会立即停止读取，同步写 11MB 会永久阻塞，故异步写。
	go func() {
		_, _ = inW.Write([]byte(huge + "\n"))
		inW.Close()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("超限请求应导致 ServeStdio 退出，实际仍在运行")
	}
	if called {
		t.Error("超限请求不应被派发到 handler")
	}
}

// TestServeStdioKillsBackgroundProcessesOnEOF 覆盖回归：stdin EOF 后
// ServeStdio 必须清理后台进程，否则它们变成调用方看不见的孤儿
// （容器内会 reparent 到 PID 1 并最终变成僵尸）。
// 信号路径本来就有 KillAll，这里对齐 EOF 路径。
//
// 本测试**在所有平台**执行。早先版本用 isUnix() 在 Windows 上 skip，
// 而 Windows 恰恰是唯一真正会漏的地方 —— skip 掉的正是要测的平台。
// 平台差异收在 pinTestShell / backgroundChildProbe / assertBackgroundChildGone 里。
func TestServeStdioKillsBackgroundProcessesOnEOF(t *testing.T) {
	// Windows 上把 shell 钉到自带 busybox，否则会回退到 Git Bash，
	// 绿了也不代表我们分发的那条路可用。
	pinTestShell(t)

	// 子进程 PID 落盘到临时文件，事后按 PID 精确验证「树」是否清干净。
	pidFile := filepath.Join(t.TempDir(), "child.pid")

	dispatcher := api.NewDispatcher()
	tools.RegisterAll(dispatcher) // 需要真实的 execute_bash
	session := testSession()
	session.SetInitialized(false)

	inW, out, done := runStdioSession(t, dispatcher, session)

	initReq := []byte(`{"id":"0","method":"initialize","input":{"protocolVersion":"0.3"}}`)
	if err := sendLine(inW, initReq); err != nil {
		t.Fatalf("写入 initialize 失败: %v", err)
	}
	if _, err := out.ReadBytes('\n'); err != nil {
		t.Fatalf("读取 initialize 响应失败: %v", err)
	}

	// 用 backgroundChildProbe 而不是直接写 `sleep 30; true`：前者把子进程
	// PID 落盘，事后能精确确认孙辈是否也被清掉，而不仅仅是父进程退了没有。
	probe := fmt.Sprintf(backgroundChildProbe, jsonQuote(pidFile))
	bgReq := []byte(fmt.Sprintf(
		`{"id":"1","method":"tool","tool":"execute_bash","input":{"command":%s,"run_in_background":true}}`,
		jsonQuote(probe)))
	if err := sendLine(inW, bgReq); err != nil {
		t.Fatalf("写入 execute_bash 失败: %v", err)
	}
	line, err := out.ReadBytes('\n')
	if err != nil {
		t.Fatalf("读取 execute_bash 响应失败: %v", err)
	}
	var resp api.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("期望 background 结果，实际 %T（%s）", resp.Result, string(line))
	}
	if result["process_id"] == nil {
		t.Fatalf("后台任务未返回 process_id: %s", string(line))
	}

	// 按响应里的 process_id 精确取回，而不是 List() 取最后一个：
	// GlobalProcessRegistry 是进程级单例，前面用例可能留下条目，
	// 取「最后一个」在用例顺序变化时会指向错误的进程（假绿/假红）。
	procID, _ := result["process_id"].(string)
	target, found := api.GlobalProcessRegistry.Get(procID)
	if !found {
		t.Fatalf("registry 里找不到 process_id=%q（后台任务未注册）", procID)
	}

	// 必须先等子进程真的起来再关 stdin，否则测的是「进程还没启动就被清理」，
	// 恰好绕开了「进程树」这件事。这里等 PID 文件出现（最多 10s）。
	waitForFile(t, pidFile, 10*time.Second)

	// 正向控制：确认子进程此刻确实活着。少了这一条，如果子进程压根没起来，
	// 后面的「清理后应消失」会恒真通过 —— 测试绿，但什么都没验证。
	assertBackgroundChildAlive(t, pidFile)

	// 关闭 stdin → ServeStdio 走 EOF 路径
	inW.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ServeStdio 未在 stdin 关闭后退出")
	}

	// 后台进程应已被 Kill（等待其退出）
	select {
	case <-target.WaitChan():
	case <-time.After(5 * time.Second):
		target.Kill()
		t.Fatal("stdin EOF 后后台进程仍在运行（未被 KillAll 清理）")
	}

	// WaitChan 只说明被注册的那个进程退了，不代表子孙也清了。
	assertBackgroundChildGone(t, pidFile)
}
