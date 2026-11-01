//go:build windows

package integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gTSP/src/api"
	"gTSP/src/tools"
)

// TestWindowsEOFKillsBackgroundTree 验证 Windows 上 stdin EOF 之后，
// execute_bash 启动的后台进程是否真的被清理干净。
//
// 与 stdio_limits_test.go 里那条 Unix 测试的区别：那条依赖进程组信号，
// 在 Windows 上必须跳过；这条只依赖 busybox + taskkill，因此在这里单独断言。
//
// 为什么这条重要：集成 busybox 之前，Windows 上 `bash` 会命中 WSL 的
// C:\Windows\System32\bash.exe，它把命令交给 Linux 虚拟机后自己立刻退出，
// 记录的 PID 随即失效，taskkill 打空（CI 日志 `process not found`）。
// 换成 busybox 后，ash 在执行复合命令时会作为父进程存活，PID 在清理那一刻
// 仍然有效，`taskkill /T /F` 应当能命中整棵树。
//
// 如果这条测试失败，说明「按 PID 杀」在 Windows 上仍不可靠，需要上 Job Object。
func TestWindowsEOFKillsBackgroundTree(t *testing.T) {
	busybox, err := filepath.Abs(busyboxRelPath)
	if err != nil {
		t.Fatalf("resolve busybox path: %v", err)
	}
	if _, err := os.Stat(busybox); err != nil {
		t.Skipf("busybox 未就位，跳过（先跑 TestBusyboxBinaryUsable 定位问题）: %v", err)
	}
	t.Setenv("GTSP_BUSYBOX", busybox)

	dispatcher := api.NewDispatcher()
	tools.RegisterAll(dispatcher) // 需要真实的 execute_bash
	session := testSession()
	session.SetInitialized(true)

	// 直接调 handler：本测试验证的是进程清理，不涉及 stdio 协议本身。
	// 复合命令（`; true`）让 ash 保持为父进程，如此记录的 PID 才代表一棵树。
	res, err := tools.ExecuteBashHandler(session, json.RawMessage(
		`{"command":"sleep 30; true","run_in_background":true}`))
	if err != nil {
		t.Fatalf("启动后台命令失败: %v", err)
	}
	bg, ok := res.(tools.BashBackgroundResult)
	if !ok {
		t.Fatalf("期望 BashBackgroundResult，实际 %T", res)
	}
	t.Logf("background process_id=%s", bg.ProcessID)

	// 等一会，确保 sleep 子进程真的启动起来（否则杀父进程就够，测不出树）
	time.Sleep(800 * time.Millisecond)

	api.GlobalProcessRegistry.KillAll()

	bp, found := api.GlobalProcessRegistry.Get(bg.ProcessID)
	if !found {
		t.Fatalf("registry 里找不到进程 %s", bg.ProcessID)
	}
	select {
	case <-bp.WaitChan():
	case <-time.After(5 * time.Second):
		t.Fatalf("KillAll 后 5s 内进程未退出：Windows 上按 PID + taskkill /T 清不掉进程树，需要 Job Object")
	}

	// 再确认没有残留的 busybox 子进程（sleep 走的是同一份 busybox）
	time.Sleep(300 * time.Millisecond)
	out, _ := exec.Command("tasklist", "/FI", "IMAGENAME eq busybox64u.exe").Output()
	if s := string(out); s != "" && containsProcEntry(s) {
		t.Errorf("KillAll 后仍有 busybox 子进程存活:\n%s", s)
	}
}

// containsProcEntry 判断 tasklist 输出里是否真有进程条目。
// 没有匹配时 tasklist 也会输出一句「信息: 没有运行的任务…」，不能按非空判断。
func containsProcEntry(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "busybox64u.exe") &&
		!strings.Contains(lower, "no tasks") &&
		!strings.Contains(s, "没有运行的任务")
}
