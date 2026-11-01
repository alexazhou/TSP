package integration_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
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
func TestServeStdioKillsBackgroundProcessesOnEOF(t *testing.T) {
	if !isUnix() {
		t.Skip("依赖进程组信号，仅在 Unix 上验证")
	}

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

	bgReq := []byte(`{"id":"1","method":"tool","tool":"execute_bash","input":{"command":"sleep 30; true","run_in_background":true}}`)
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

	registered := api.GlobalProcessRegistry.List()
	if len(registered) == 0 {
		t.Fatal("后台任务未注册到 GlobalProcessRegistry")
	}
	target := registered[len(registered)-1]

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
}

// isUnix 报告当前平台是否支持本测试依赖的进程组语义。
//
// 本文件没有 build tag（另外两个测试是平台无关的），所以必须在函数内判断：
// Windows 上 kill 走 taskkill /T /F，依赖 PID 存在性，且 busybox 的进程树语义
// 与 Unix 进程组不同，无法在此断言。
func isUnix() bool {
	return runtime.GOOS != "windows"
}
