//go:build windows

package pal

// ShellDescription 供工具描述使用：告诉调用方命令是被哪个 shell 解释的。
//
// 为什么要暴露这段文字：Windows 上执行的是 busybox 的 ash（POSIX sh），
// 不是 bash。模型/用户按 bash 习惯写 `[[ ]]`、`{1..10}`、数组会得不到预期结果，
// 而这类差异**不会报错**（ash 把 `{1..10}` 原样当字面量），最容易变成
// 「命令跑了、输出是空的、没人知道为什么」。所以在工具契约里写明。
func ShellDescription() string {
	return "Windows: commands are interpreted by busybox `ash` (POSIX sh), not bash"
}

// PathNotes 供工具描述使用：说明 cwd 与路径写法。
//
// 只写有依据的内容：
//   - cwd 继承自 gtsp 进程（execute_bash 不设 cmd.Dir，见 execute_bash.go）；
//   - busybox 是原生 Windows 程序，与 read_file/write_file 同一套 C:\ 路径空间；
//   - 反斜杠默认被转成正斜杠、`-X` 可关闭 —— 出自 busybox-w32 官方 README；
//   - 绝对路径官方写法是 `c:/path`。
//
// 刻意不写 `/c/path` 这种 Cygwin/MSYS 形式：官方 README 未列出，社区文章说法
// 互相矛盾，实测（test/integration/busybox_windows_probe_test.go）结论出来前
// 不写进契约 —— 写错了比不写更糟。
func PathNotes() string {
	return "Working directory: the directory gtsp was started from, or --workdir if given " +
		"(execute_bash does not change it per call).\n" +
		"Paths: this is a native Windows process, so it sees the same filesystem and the same " +
		"`C:\\...` paths as read_file/write_file.\n" +
		" - Prefer forward slashes: `c:/work/file.txt`.\n" +
		" - Backslashes in commands are converted to forward slashes by default; " +
		"set GTSP_BUSYBOX_X=1 to keep them literal if a native Windows program rejects `c:/` form.\n" +
		" - To switch drives use `cd d:/`, not `d:`."
}
