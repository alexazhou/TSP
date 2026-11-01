package tools

import (
	"strings"
	"testing"
)

// TestExecuteBashSchemaDescribesRealShell 确认 command 描述点明了是 shell 命令。
//
// 注意这里**不判断具体是哪个 shell**：那需要按平台断言，放在 src/pal 里做
// （shell_desc_unix_test.go / shell_desc_windows_desc_test.go）。
// 本测试早先用「命中 bash/ash/POSIX sh/cmd.exe/powershell 任一关键字即过」的判据，
// 结果把 Unix 的说明改成「Windows cmd.exe / powershell」也照样绿 ——
// 判据太宽，等于没测。跨平台一致的只断言这句话，平台相关的交给平台测试。
func TestExecuteBashSchemaDescribesRealShell(t *testing.T) {
	cmdDesc := commandParamDescription(t)

	if !strings.Contains(cmdDesc, "shell command") {
		t.Errorf("command 描述未点明是 shell 命令: %q", cmdDesc)
	}
	// 描述必须由 pal 提供平台文案拼接而来，因此必然含平台段的前缀。
	// 若有人把描述改回硬编码的固定文本，这里会红。
	if !strings.Contains(cmdDesc, "commands are interpreted by") {
		t.Errorf("command 描述未包含平台解释器说明（应由 pal.ShellDescription 拼接）: %q", cmdDesc)
	}
}

// TestExecuteBashDescriptionMentionsPaths 确认工具描述里给出了 cwd 与路径说明。
//
// 使用者最常问的两件事就是「命令在哪个目录跑」和「路径该怎么写」。
// 这两条以前完全没写，只能靠试。
func TestExecuteBashDescriptionMentionsPaths(t *testing.T) {
	desc := ExecuteBashSchema.Description

	if !strings.Contains(desc, "Working directory") {
		t.Errorf("工具描述未说明工作目录: %q", desc)
	}
	if !strings.Contains(desc, "--workdir") {
		t.Errorf("工具描述未说明 --workdir 会改变工作目录: %q", desc)
	}
	if !strings.Contains(desc, "Paths:") {
		t.Errorf("工具描述未说明路径写法: %q", desc)
	}
}

// TestExecuteBashDescriptionKeepsExecutionModes 确认原有内容没被这次改动丢掉。
//
// 上面两条是新增断言；这条防止「为了加新内容而删掉旧内容」的回归。
func TestExecuteBashDescriptionKeepsExecutionModes(t *testing.T) {
	desc := ExecuteBashSchema.Description

	for _, want := range []string{
		"task_timeout",
		"timeout_action",
		"run_in_background",
		"process_id",
		"process_output",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("工具描述丢失了 %q:\n%s", want, desc)
		}
	}
}

// TestExecuteBashDescriptionHasThreeRealModes 钉住执行模式列表与实现一致。
//
// 旧文案写的是「Synchronous no timeout: use run_in_background:true ...」：
// 既声称存在一个永不超时的同步模式（实际最长只等 task_timeout），又把异步
// 模式写了两遍（模式 2 与 3 是同一个参数）。这里按实现的三条路径断言：
// 同步+超时 kill、同步+超时转后台、直接异步。
func TestExecuteBashDescriptionHasThreeRealModes(t *testing.T) {
	desc := ExecuteBashSchema.Description

	if strings.Contains(desc, "Synchronous no timeout") {
		t.Errorf("工具描述再次出现不存在的 'Synchronous no timeout' 模式:\n%s", desc)
	}
	for _, want := range []string{
		"Three execution modes",
		`timeout_action: "kill"`,
		`timeout_action: "background", default`,
		"run_in_background: true",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("工具描述缺少模式说明 %q:\n%s", want, desc)
		}
	}
}

// commandParamDescription 从 schema 里取出 command 参数的 description。
func commandParamDescription(t *testing.T) string {
	t.Helper()
	schema, ok := ExecuteBashSchema.InputSchema.(map[string]interface{})
	if !ok {
		t.Fatalf("InputSchema 类型异常: %T", ExecuteBashSchema.InputSchema)
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema.properties 类型异常: %T", schema["properties"])
	}
	cmd, ok := props["command"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema.properties.command 类型异常: %T", props["command"])
	}
	desc, ok := cmd["description"].(string)
	if !ok {
		t.Fatalf("command.description 类型异常: %T", cmd["description"])
	}
	return desc
}
