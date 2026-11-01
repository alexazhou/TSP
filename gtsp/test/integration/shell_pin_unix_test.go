//go:build !windows

package integration_test

import (
	"bytes"
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
	cmd := exec.Command("kill", "-0", pid)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
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
