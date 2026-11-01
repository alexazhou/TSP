//go:build !windows

package pal

// ShellDescription 供工具描述使用。
//
// Unix 上一律是 bash，与引入 pal 之前的行为一致，不需要额外提示。
func ShellDescription() string {
	return "Unix: commands are interpreted by `bash -c`"
}

// PathNotes 供工具描述使用：说明 cwd。
//
// 只写有依据的内容：execute_bash 不设 cmd.Dir，子进程继承 gtsp 进程的 cwd
// （gtsp 启动目录，或 --workdir 指定的目录）。
func PathNotes() string {
	return "Working directory: the directory gtsp was started from, or --workdir if given " +
		"(execute_bash does not change it per call).\n" +
		"Paths: ordinary POSIX paths, the same ones read_file/write_file use."
}
