package tools

import (
	"strings"
	"testing"
)

// TestExecuteBashSchemaDescribesRealShell 钉住工具描述里的 shell 说明。
//
// 为什么需要这条测试：描述是模型/用户唯一的契约来源。Windows 上执行的是
// busybox 的 ash（POSIX sh），若描述里写 "bash"，使用者会写 `[[ ]]`、
// `{1..10}`、数组 —— ash 对 `{1..10}` 不展开也不报错，会静默输出字面量。
// 那种失败最难查：命令跑了、rc=0、结果是空的。
//
// 历史坑：这条描述原本硬编码 "The exact bash command to execute."，
// 在 Windows 上是一句假话。
func TestExecuteBashSchemaDescribesRealShell(t *testing.T) {
	cmdDesc := commandParamDescription(t)

	if !strings.Contains(cmdDesc, "shell command") {
		t.Errorf("command 描述未点明是 shell 命令: %q", cmdDesc)
	}

	// 描述必须来自 pal（按平台编译），不能是硬编码的一 platform-agnostic 文案。
	// 判据：无论哪个平台，描述都必须提到具体的解释器，而不是泛泛的 "shell"。
	platformish := []string{"bash", "ash", "POSIX sh", "cmd.exe", "powershell"}
	found := false
	for _, kw := range platformish {
		if strings.Contains(cmdDesc, kw) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("command 描述没有点明解释器（应为 bash / ash / POSIX sh 之一）: %q", cmdDesc)
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
