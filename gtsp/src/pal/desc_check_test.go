package pal

import (
	"strings"
	"testing"
)

// describesAsh 判断一段描述是否「说的是 ash，且没有声称是 bash」。
//
// 抽成纯函数是为了能**跨平台测试判据本身**：Windows 的描述只能在
// windows 构建下取到，但判据里的坑（`bash` 含 `ash` 子串）与平台无关，
// 必须能在 darwin/linux 上验证。否则这条判据的假绿只能靠 windows CI 发现。
//
// 两个子串陷阱都在这里处理：
//   - 正向：`strings.Contains(desc, "ash")` 对 `bash` 成立 → 先抹掉 "bash"
//   - 反向：不能用 "ash" 做否定词（`bash` 含它）→ 用 "bash -c" / "by `bash" 这类
//     明确声称 bash 的短语
func describesAsh(desc string) bool {
	lower := strings.ToLower(desc)

	// 先去掉所有 "bash"，剩下的 "ash" 才是真的 ash。
	// 例：`busybox \`ash\` (POSIX sh), not bash` → 去掉 bash 后仍含 "ash" ✅
	//     `busybox \`bash\``                     → 去掉 bash 后不含 "ash" ❌
	if !strings.Contains(strings.ReplaceAll(lower, "bash", ""), "ash") {
		return false
	}
	// 必须提到 busybox（否则可能是别的 ash 实现，与我们的分发无关）。
	if !strings.Contains(lower, "busybox") {
		return false
	}
	// 不能声称用 bash：`by \`bash` 或 `bash -c` 都是明确声称。
	if strings.Contains(lower, "by `bash") || strings.Contains(lower, "bash -c") {
		return false
	}
	return true
}

// TestDescribesAshPredicate 穷举判据的输入输出。
//
// 前两条就是曾经的假绿：「busybox `bash`」在旧判据（直接 Contains "ash"）
// 下返回 true，而它恰恰是这条判据唯一要禁止的描述。
func TestDescribesAshPredicate(t *testing.T) {
	cases := []struct {
		desc string
		want bool
		why  string
	}{
		// —— 正确描述 ——
		{"Windows: commands are interpreted by busybox `ash` (POSIX sh), not bash", true,
			"当前实际文案"},
		{"busybox ash", true, "最小合法"},
		{"interpreted by busybox `ash`", true, "无 bash 字样"},

		// —— 曾经假绿的两种 ——
		{"Windows: commands are interpreted by busybox `bash`", false,
			"旧判据放过它：bash 含 ash 子串"},
		{"Windows: commands are interpreted by busybox `bash` (Git Bash)", false,
			"同上，且多一个 Git Bash"},

		// —— 其他骗法 ——
		{"Windows: commands are interpreted by `bash -c` via busybox", false,
			"声称 bash -c"},
		{"busybox `ash` via `bash -c`", false, "同时出现 ash 与 bash -c"},

		// —— 根本不是 ash ——
		{"Windows: commands are interpreted by Git Bash", false, "Git Bash"},
		{"Windows: commands are interpreted by PowerShell", false, "PowerShell"},
		{"Windows: commands are interpreted by cmd.exe", false, "cmd.exe"},
		{"Windows: commands are interpreted by a shell", false, "没有点明解释器"},
		{"ash", false, "没提 busybox"},
		{"", false, "空串"},
	}

	for _, tc := range cases {
		if got := describesAsh(tc.desc); got != tc.want {
			t.Errorf("describesAsh(%q) = %v, want %v（%s）", tc.desc, got, tc.want, tc.why)
		}
	}
}

// TestDescribesAshRejectsBashSubstringTrap 单独钉住 "bash 含 ash" 这个子串陷阱。
//
// 独立成一条是为了让失败信息直白：以后有人把判据简化成
// `strings.Contains(desc, "ash")` 时，这条会立刻红并指出原因。
func TestDescribesAshRejectsBashSubstringTrap(t *testing.T) {
	// 防呆：确认前提成立，否则这条测试本身没意义。
	if !strings.Contains("bash", "ash") {
		t.Fatal("\"bash\" 不再包含 \"ash\"，本陷阱已不存在，可简化判据")
	}
	if describesAsh("busybox `bash`") {
		t.Error("describesAsh 把 `busybox `bash`` 判为 ash —— 正是这条判据要禁止的假绿")
	}
}
