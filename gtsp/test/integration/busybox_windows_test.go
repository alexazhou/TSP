//go:build windows

package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// busyboxRelPath 是 release 分发的 busybox 相对仓库内的位置。
// 测试的工作目录是本包目录（gtsp/test/integration），故用 ../.. 回到 gtsp/。
const busyboxRelPath = "../../third_party/busybox/busybox64u.exe"

// busyboxSHA256 锁定入库的二进制内容。变更该文件必须同步更新此常量，
// 否则「CI 绿」可能对应的是一个被替换过的 busybox。
const busyboxSHA256 = "36b266923fc5765b42f5f981319f39568498fba60b53ccf26e8fc1e575e2a9a9"

// TestBusyboxBinaryUsable 确认随仓库分发的 busybox 真的能在 Windows 上跑起来，
// 而不是「文件在、但坏了 / 架构不对」。
//
// 这个测试存在的理由：其它测试可能回退到 Git Bash 而仍然通过，那样 CI 绿
// 不能证明 busybox 可用。此处直接执行 busybox 本体，绕开任何回退路径。
func TestBusyboxBinaryUsable(t *testing.T) {
	path, err := filepath.Abs(busyboxRelPath)
	if err != nil {
		t.Fatalf("resolve busybox path: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("busybox not found at %s: %v（release 需要它随 gtsp.exe 分发）", path, err)
	}

	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != busyboxSHA256 {
		t.Fatalf("busybox sha256 = %s, want %s（二进制内容变了，需确认来源后同步更新常量）", got, busyboxSHA256)
	}
	if len(data) != 683520 {
		t.Errorf("busybox size = %d, want 683520", len(data))
	}

	t.Run("shell_basics", func(t *testing.T) {
		out, err := exec.Command(path, "sh", "-c", "echo hello; echo oops >&2; exit 7").CombinedOutput()
		if err == nil {
			t.Fatalf("expected non-zero exit, got success (output %q)", out)
		}
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
		}
		if exitErr.ExitCode() != 7 {
			t.Errorf("exit code = %d, want 7", exitErr.ExitCode())
		}
		s := string(out)
		if !strings.Contains(s, "hello") || !strings.Contains(s, "oops") {
			t.Errorf("stdout/stderr not captured as expected: %q", s)
		}
	})

	t.Run("posix_constructs", func(t *testing.T) {
		// execute_bash 的工具契约是 POSIX sh。这里把测试与真实用户都会用到的
		// 构造跑一遍，确认 ash 支持（不含 bash 专属的花括号展开、[[ ]]）。
		script := `i=1; while [ $i -le 3 ]; do echo "n$i"; i=$((i+1)); done; echo done | tr a-z A-Z`
		out, err := exec.Command(path, "sh", "-c", script).Output()
		if err != nil {
			t.Fatalf("posix script failed: %v", err)
		}
		want := "n1\nn2\nn3\nDONE\n"
		if string(out) != want {
			t.Errorf("output = %q, want %q", string(out), want)
		}
	})

	t.Run("builtin_applets_present", func(t *testing.T) {
		// 这些 applet 决定 Windows 上「命令集缺失」问题是否真的解决了。
		for _, applet := range []string{"ls", "cat", "grep", "sed", "awk", "find", "sleep", "ps", "kill", "tr"} {
			if err := exec.Command(path, "sh", "-c", "command -v "+applet).Run(); err != nil {
				t.Errorf("applet %q not available in busybox: %v", applet, err)
			}
		}
	})
}
