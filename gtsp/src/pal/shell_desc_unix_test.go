//go:build !windows

package pal

import (
	"strings"
	"testing"
)

// TestShellDescriptionIsBashOnUnix 钉住 Unix 上的 shell 说明。
//
// 只写「必须提到某个解释器」是不够的：早先 tools 里的判据是
// `strings.Contains(desc, "bash")` 命中任一关键字即通过，
// 于是把 Unix 的说明改成「interpreted by Windows cmd.exe / powershell」
// 测试照样绿 —— 而那正是这段文案要防的错误（在 Unix 上谎称 cmd.exe，
// 等价于在 Windows 上谎称 bash）。
//
// 反向词里刻意不含 "ash"：`bash` 本身就包含 `ash` 子串，用它做否定判断会误伤。
func TestShellDescriptionIsBashOnUnix(t *testing.T) {
	desc := ShellDescription()

	if !strings.Contains(desc, "bash -c") {
		t.Errorf("Unix 的 shell 说明应包含 `bash -c`，实际: %q", desc)
	}

	wrong := []string{"busybox", "cmd.exe", "powershell", "wsl"}
	for _, w := range wrong {
		if strings.Contains(strings.ToLower(desc), w) {
			t.Errorf("Unix 的 shell 说明不该提到 %q（那是别的平台的解释器）: %q", w, desc)
		}
	}
}

// TestPathNotesOnUnix 钉住 Unix 的路径说明不该混入 Windows 概念。
func TestPathNotesOnUnix(t *testing.T) {
	notes := PathNotes()

	if !strings.Contains(notes, "Working directory") {
		t.Errorf("路径说明应包含工作目录信息: %q", notes)
	}

	wrong := []string{"c:/", `c:\`, "busybox", "drive"}
	for _, w := range wrong {
		if strings.Contains(strings.ToLower(notes), w) {
			t.Errorf("Unix 的路径说明不该提到 %q（Windows 概念）: %q", w, notes)
		}
	}
}
