//go:build windows

package integration_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 这些常量是 busybox-w32 在 Windows 上的**实测**路径行为，
// 由 TestBusyboxPathSemantics 断言。
//
// 来源：GitHub 托管 windows-latest runner 上的真实执行（run 41281531655，
// busybox64u.exe / 683,520 B）。不是从文档或社区文章推断的 ——
// 事实上社区文章普遍在传 `cd /c/`，实测是**失败**的（rc=2）。
const (
	// cygwinStylePath 是 Cygwin/MSYS 的盘符形式。busybox-w32 **不支持**。
	cygwinStylePath = "/c/"
	// driveStylePath 是 busybox-w32 认可的盘符形式。
	driveStylePath = "c:/"
)

// TestBusyboxPathSemantics 锁定 Windows 上的路径写法。
//
// 为什么值得一条测试：工具描述直接告诉使用者「路径怎么写」。若哪天 busybox
// 换版本后 `/c/` 变得可用（或 `c:/` 失效），描述就变成假话，而使用者会照着
// 错误的方向去排查 —— 这类错误不会报「语法错」，只会报「找不到文件」。
func TestBusyboxPathSemantics(t *testing.T) {
	busybox := busyboxPath(t)

	run := func(script string) (string, int) {
		t.Helper()
		out, err := exec.Command(busybox, "sh", "-c", script).CombinedOutput()
		rc := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				rc = ee.ExitCode()
			} else {
				t.Fatalf("执行 %q 失败: %v", script, err)
			}
		}
		return strings.TrimRight(string(out), "\r\n"), rc
	}

	t.Run("cygwin_form_fails", func(t *testing.T) {
		// 这条是**负面断言**：`/c/` 不可用。文档与工具描述据此明确劝退这种写法。
		// 若哪天它变成可用，本测试会红，提醒同步改描述（而不是让描述继续骗人）。
		out, rc := run("cd " + cygwinStylePath)
		if rc == 0 {
			t.Fatalf("cd %s 竟然成功了（输出 %q）：busybox 行为已变，需同步更新 README 与 PathNotes", cygwinStylePath, out)
		}
		if !strings.Contains(strings.ToLower(out), "no such file") {
			t.Errorf("期望 \"no such file\" 类错误，实际 rc=%d out=%q", rc, out)
		}
	})

	t.Run("drive_form_works", func(t *testing.T) {
		out, rc := run("cd " + driveStylePath + " && pwd")
		if rc != 0 {
			t.Fatalf("cd %s && pwd 失败 rc=%d out=%q", driveStylePath, rc, out)
		}
		// pwd 应回报正斜杠 + 盘符形式，而非 /c/
		if !strings.HasPrefix(strings.ToLower(out), "c:/") {
			t.Errorf("pwd 输出 %q，期望以 c:/ 开头（盘符形式）", out)
		}
	})

	t.Run("drive_letter_alone_works", func(t *testing.T) {
		// 旧文档曾说 `cd C:` 会被当成「执行名为 C: 的程序」。实测是能切的，
		// 这里钉住实测结论，避免有人照旧文档去"修"一个不存在的问题。
		if _, rc := run("cd C:"); rc != 0 {
			t.Errorf("cd C: 失败 rc=%d（实测应为 0）", rc)
		}
	})

	t.Run("unquoted_backslash_eaten_by_shell", func(t *testing.T) {
		// 这是最容易误诊的一条：反斜杠消失**不是** busybox 的转换造成的，
		// 是 shell 的转义规则。因此 -X 也救不了它。工具描述据此不把 -X 当解药。
		out, rc := run(`printf '%s\n' C:\Windows\System32`)
		if rc != 0 {
			t.Fatalf("printf 失败 rc=%d", rc)
		}
		if out != `C:WindowsSystem32` {
			t.Errorf("未加引号的反斜杠实测为 %q，期望 %q（shell 吃掉反斜杠）", out, `C:WindowsSystem32`)
		}

		quoted, _ := run(`printf '%s\n' 'C:\Windows\System32'`)
		if quoted != `C:\Windows\System32` {
			t.Errorf("加引号后实测为 %q，期望原样 %q", quoted, `C:\Windows\System32`)
		}
	})

	// -X 的**位置**：busybox 的 -X 是「shell 自己的选项」，必须出现在 `sh`
	// 之后、`-c` 之前。写成 `busybox -X sh -c ...` 会被当成 applet 名，
	// 结果是 127 / applet not found。ShellCommand 里用的是前者（正确），
	// 这条测试把位置要求钉住，避免以后有人「顺手」把它挪到最前面。
	t.Run("x_flag_position", func(t *testing.T) {
		correct := exec.Command(busybox, "sh", "-X", "-c", "echo ok")
		if out, err := correct.CombinedOutput(); err != nil {
			t.Errorf("`busybox sh -X -c` 应成功，实际 err=%v out=%q", err, string(out))
		}

		wrong := exec.Command(busybox, "-X", "sh", "-c", "echo ok")
		out, err := wrong.CombinedOutput()
		if err == nil {
			t.Errorf("`busybox -X sh -c` 竟然成功了（输出 %q）：-X 位置约束已变，需复核 ShellCommand", string(out))
		}
	})
}

// busyboxPath 返回仓库内 busybox 的绝对路径。
func busyboxPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(busyboxRelPath)
	if err != nil {
		t.Fatalf("resolve busybox path: %v", err)
	}
	return p
}
