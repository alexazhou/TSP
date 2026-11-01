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
// 每条都有实测依据（test/integration/busybox_windows_probe_test.go 在
// GitHub 托管 windows-latest 上执行，run 41281531655）：
//   - cwd 继承自 gtsp 进程（execute_bash 不设 cmd.Dir）；
//   - busybox 是原生 Windows 程序，与 read_file/write_file 同一套 C:\ 路径空间；
//   - `c:/path` 可用，`/c/path` 确定不可用（rc=2 can't cd）—— 这条最关键，
//     因为社区文章普遍在传 `/c/`，照着写会直接失败；
//   - `cd C:`（只有盘符）实测可用，不是「被当程序执行」。
//
// 刻意不把 `-X` 说成「关掉它路径就对了」：实测 `sh -X -c 'printf ... C:\Windows'`
// 与不加 -X 输出一致，因为反斜杠在进 busybox 之前就被 shell 当转义吃掉了。
// 把它写成通用解药会让用户按错误方向排查。
func PathNotes() string {
	return "Working directory: the directory gtsp was started from, or --workdir if given " +
		"(execute_bash does not change it per call).\n" +
		"Paths: this is a native Windows process, so it sees the same filesystem and the same " +
		"`C:\\...` paths as read_file/write_file.\n" +
		" - Use drive-letter form with forward slashes: `c:/work/file.txt`.\n" +
		" - Do NOT use the Cygwin-style `/c/work` form: it fails (no such file or directory).\n" +
		" - Unquoted backslashes are eaten by the shell (`C:\\Windows` becomes `C:Windows`), " +
		"so quote them or use forward slashes.\n" +
		" - To switch drives use `cd d:/`."
}
