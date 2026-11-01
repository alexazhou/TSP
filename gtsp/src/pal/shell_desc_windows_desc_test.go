//go:build windows

package pal

import (
	"strings"
	"testing"
)

// TestShellDescriptionIsAshOnWindows 钉住 Windows 上的 shell 说明。
//
// 与 Unix 侧对称：这里必须提到 busybox/ash（POSIX sh），且不得声称是 bash。
// 早先的判据「命中 bash/ash/POSIX sh/cmd.exe/powershell 任意一个即通过」太宽，
// 把 `bash` 也算数 —— 而在 Windows 上写 `bash` 恰恰是这次要修的假话。
func TestShellDescriptionIsAshOnWindows(t *testing.T) {
	desc := ShellDescription()

	// 用共享判据（定义在 desc_check_test.go，跨平台可测）。
	// 注意这里曾用 strings.Contains(desc, "ash") 直接判断，而 `bash` 里就含
	// `ash` —— 于是「busybox `bash`」这种恰恰要禁止的描述会通过。
	// 判据已抽成纯函数，并在所有平台上穷举验证过（TestDescribesAshPredicate）。
	if !describesAsh(desc) {
		t.Errorf("Windows 的 shell 说明应点明 busybox ash 且不得声称 bash，实际: %q", desc)
	}

	lower := strings.ToLower(desc)

	for _, w := range []string{"cmd.exe", "powershell", "wsl"} {
		if strings.Contains(lower, w) {
			t.Errorf("Windows 的 shell 说明不该提到 %q（不是我们用的解释器）: %q", w, desc)
		}
	}
}

// TestPathNotesOnWindows 钉住 Windows 的路径说明包含已实测的三种写法。
//
// 依据：TestBusyboxPathSemantics（test/integration）在 windows-latest 上实测。
// 这三条若从说明里消失，使用者就会照着社区文章写 /c/ 然后失败。
func TestPathNotesOnWindows(t *testing.T) {
	notes := PathNotes()
	lower := strings.ToLower(notes)

	if !strings.Contains(notes, "c:/") {
		t.Errorf("Windows 路径说明应给出 c:/ 形式: %q", notes)
	}
	if !strings.Contains(notes, "/c/") {
		t.Errorf("Windows 路径说明应明确劝退 /c/ 形式（实测不可用）: %q", notes)
	}
	if !strings.Contains(lower, "working directory") {
		t.Errorf("Windows 路径说明应包含工作目录信息: %q", notes)
	}

	// 不该把 -X 说成通用解药：实测 `sh -X -c 'printf ... C:\Windows'` 与不加 -X 输出相同。
	if strings.Contains(notes, "GTSP_BUSYBOX_X") {
		t.Errorf("PathNotes 不应把 GTSP_BUSYBOX_X 当路径问题的解法（实测无效，说明在 README）: %q", notes)
	}
}
