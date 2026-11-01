//go:build windows

package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pinTestShell 在 Windows 上把 shell 钉到仓库内自带的 busybox。
//
// 不钉的话 ResolveShell 会回退到 Git for Windows 的 bash —— 那也是 POSIX shell，
// 命令照样跑得起来、测试照样能绿，于是「busybox 这条路可用」就完全没被测到。
// 这是典型的假绿：测试通过只证明「有个 shell」，不证明「用的是我们要的那个」。
func pinTestShell(t *testing.T) {
	t.Helper()
	busybox, err := filepath.Abs(busyboxRelPath)
	if err != nil {
		t.Fatalf("resolve busybox path: %v", err)
	}
	if _, err := os.Stat(busybox); err != nil {
		t.Fatalf("busybox 未就位: %v（release 需要它随 gtsp.exe 分发）", err)
	}
	t.Setenv("GTSP_BUSYBOX", busybox)
}

// backgroundChildProbe 是一段会留下可追踪子进程 PID 的后台命令模板。
//
// 结构说明（每一处都有用途）：
//   - `sleep 30 &`        造一个真正的子进程，成为「进程树」的孙辈；
//   - `echo $! > 文件`     把子进程 PID 落盘，事后可精确检查它是否还活着；
//   - `wait`              让 ash 保持为父进程并阻塞等待，PID 在清理那一刻仍然有效
//     （若不 wait，ash 可能被 exec 替换成 sleep，PID 直指 sleep，就测不出「树」）。
//
// 为什么不用「扫 busybox64u.exe 进程名」判断残留：go test 会并行跑不同包，
// test/unit 里也有 execute_bash 用例会起 busybox。按进程名全局扫描会把别人
// 的进程算到自己头上 —— 既可能假绿（自己的没清干净、别人的刚好退出）也可能
// 假红。按 PID 精确检查没有这个歧义。
//
// 子进程的 stdout/stderr 重定向到 NUL：否则它会继承 execute_bash 的管道写端，
// 父进程被杀后 os/exec 的 Wait 仍要等管道关闭，于是「子孙是否残留」这一层
// 永远走不到，实际触发的是 WaitChan 超时那条断言（断言形同虚设）。
//
// 注意 %s 已经带了引号（调用方传的是 jsonQuote 结果），这里**不能再加一对**：
// `> ""C:\path""` 在遇到空格时会提前闭合引号，把文件写到错误的路径上。
const backgroundChildProbe = `sleep 30 >NUL 2>&1 & echo $! > %s; wait`

// assertBackgroundChildAlive 在关闭 stdin **之前**确认子进程真的活着。
//
// 这是「清理断言」的正向控制（positive control）：如果子进程压根没起来，
// 后面的 assertBackgroundChildGone 会因为在进程表里找不到它而**恒真通过**。
func assertBackgroundChildAlive(t *testing.T, pidFile string) {
	t.Helper()

	pid := readTrimmed(t, pidFile)
	if pid == "" {
		t.Fatalf("子进程 PID 未落盘，无法确认它是否真的启动: %s", pidFile)
	}

	out, _ := exec.Command("tasklist", "/FI", "PID eq "+pid, "/FO", "CSV", "/NH").Output()
	if !containsPID(string(out), pid) {
		t.Fatalf("关闭 stdin 前子进程 PID %s 就已不存在，后续清理断言将恒真通过:\n%s", pid, string(out))
	}
}

// assertBackgroundChildGone 在 KillAll 之后确认指定 PID 的子进程已经消失。
//
// 为什么要单独查：WaitChan 只反映被注册的那条 cmd 是否退出。Windows 上没有
// 进程组，清理走 taskkill /T /F 逐 PID 递归，父进程退了而子进程留下是常态 ——
// 而孤儿正是这条测试要防的东西（容器里会 reparent 到 PID 1 并变成僵尸）。
func assertBackgroundChildGone(t *testing.T, pidFile string) {
	t.Helper()
	// taskkill 是异步投递信号的，给它一点时间再查。
	time.Sleep(500 * time.Millisecond)

	pid := readTrimmed(t, pidFile)
	if pid == "" {
		// 落盘失败时不能静默通过：那样「没有残留」可能只是「压根没查到」。
		t.Fatalf("子进程 PID 未落盘，无法验证清理结果（命令没按预期执行）: %s", pidFile)
	}

	// /FO CSV /NH 让输出稳定为 `"busybox64u.exe","1234"` 形式；
	// 无匹配时 tasklist 输出的提示文本里不含该 PID 数字。
	out, _ := exec.Command("tasklist", "/FI", "PID eq "+pid, "/FO", "CSV", "/NH").Output()
	if containsPID(string(out), pid) {
		t.Errorf("KillAll 后子进程 PID %s 仍存活（taskkill /T 未清掉进程树）:\n%s", pid, string(out))
	}
}

// containsPID 判断 tasklist 输出里是否真的包含该 PID。
//
// 不能按「输出非空」判断：无匹配时 tasklist 也会打印一句提示
// （中文版「信息: 没有运行的任务匹配指定标准。」）。提示文本不含 PID 数字，
// 所以用 PID 子串判断是安全的。
func containsPID(out, pid string) bool {
	for _, field := range strings.FieldsFunc(out, func(r rune) bool {
		return r == ',' || r == '"' || r == ' ' || r == '\r' || r == '\n' || r == '\t'
	}) {
		if field == pid {
			return true
		}
	}
	return false
}

// readTrimmed 读取一个文件并去掉首尾空白。
func readTrimmed(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
