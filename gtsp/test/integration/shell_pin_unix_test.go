//go:build !windows

package integration_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// pinTestShell 在 Unix 上是空操作：execute_bash 本来就用 PATH 里的 bash，
// 没有「回退到另一个 shell」的歧义。
func pinTestShell(t *testing.T) {
	t.Helper()
}

// backgroundChildProbe 的 Unix 版本。
//
// Unix 有真正的进程组：清理走 kill(-pgid)，整个组一起收信号。这里同样把
// 子进程 PID 落盘，用同一套断言验证「树」是否清干净 —— 两个平台测同一件事，
// 而不是各测一半。
//
// 子进程的 stdout/stderr 必须重定向到 /dev/null：否则它会继承 execute_bash 的
// 管道写端，父进程被杀后 cmd.Wait() 仍要等管道关闭 → 实际触发的是 WaitChan
// 超时那条断言，「子孙是否残留」这一层就永远走不到（断言形同虚设）。
const backgroundChildProbe = `sleep 30 >/dev/null 2>&1 & echo $! > %s; wait`

// assertBackgroundChildAlive 在关闭 stdin **之前**确认子进程真的活着。
//
// 这是「清理断言」的正向控制（positive control）：如果子进程压根没起来，
// 后面的 assertBackgroundChildGone 会因为在进程表里找不到它而**恒真通过** ——
// 测试看起来绿，实际什么都没验证。
// 实测过这个漏洞：把 probe 改成 `sleep 30 >/nonexistent_zzz_dir/x` 让子进程
// 立即退出，没有本断言时测试仍然 PASS。
func assertBackgroundChildAlive(t *testing.T, pidFile string) {
	t.Helper()

	pid := readTrimmed(t, pidFile)
	if pid == "" {
		t.Fatalf("子进程 PID 未落盘，无法确认它是否真的启动: %s", pidFile)
	}

	// kill -0 只探测存活，不投递信号。
	if err := exec.Command("kill", "-0", pid).Run(); err != nil {
		t.Fatalf("关闭 stdin 前子进程 PID %s 就已不存在，后续清理断言将恒真通过: %v", pid, err)
	}
}

// assertBackgroundChildGone 在 Unix 上用 kill -0 探测 PID 是否还存在。
//
// 不用 syscall.Kill(pid, 0)：需要引入 syscall 并处理 EPERM/EINVAL 的区分，
// 而这里只关心「进程还在不在」。走 `kill -0` 更直白，语义就是探活。
func assertBackgroundChildGone(t *testing.T, pidFile string) {
	t.Helper()

	pid := readTrimmed(t, pidFile)
	if pid == "" {
		// 落盘失败时不能静默通过：那样「没有残留」可能只是「压根没查到」。
		t.Fatalf("子进程 PID 未落盘，无法验证清理结果（命令没按预期执行）: %s", pidFile)
	}

	// kill -0 只探测存活，不投递信号。进程不存在时 rc=1 且 stderr 有 "No such process"。
	if err := exec.Command("kill", "-0", pid).Run(); err == nil {
		t.Errorf("KillAll 后子进程 PID %s 仍存活（进程组未被清干净）", pid)
	}
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
