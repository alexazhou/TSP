//go:build windows

package pal

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// busyboxCandidates 是随 gtsp.exe 一起分发的 busybox 文件名，按优先级排列。
//
// busybox64u.exe  64 位 + Unicode 支持（Win10 1903+/Win11 首选）
// busybox64.exe   64 位，无 Unicode 支持
// busybox.exe     32 位，可在 64 位系统上运行
var busyboxCandidates = []string{"busybox64u.exe", "busybox64.exe", "busybox.exe"}

// ShellCommand 返回一个通过 busybox sh 执行 script 的命令。
//
// Windows 没有原生 sh，gtsp 自带 busybox（见 third_party/busybox/）随 exe 分发。
// 查找顺序：
//  1. 环境变量 GTSP_BUSYBOX 指定的绝对路径（测试与自定义部署用）
//  2. gtsp.exe / 测试二进制所在目录（release 产物的标准布局）
//  3. PATH
//  4. 回退到 bash —— 但显式拒绝 WSL 的 C:\Windows\System32\bash.exe
//
// 回退到 bash 只是为了兼容「用户已装 Git for Windows、但漏拷了 busybox」的场景；
// 一旦命中 WSL 的 bash.exe 必须报错，因为 WSL 里跑的是另一个文件系统，会让
// read_file/write_file 与 execute_bash 看到两个不同的世界（静默跑错地方）。
func ShellCommand(script string) (*exec.Cmd, error) {
	shell, err := ResolveShell()
	if err != nil {
		return nil, err
	}
	// busybox 用 `sh` applet 进入 ash；非 busybox 的 bash 直接接受 -c。
	if IsBusybox(shell) {
		if xFlag() {
			return exec.Command(shell, "sh", "-X", "-c", script), nil
		}
		return exec.Command(shell, "sh", "-c", script), nil
	}
	return exec.Command(shell, "-c", script), nil
}

// ResolveShell 返回实际会被使用的 shell 路径。
//
// 不做缓存：解析成本是 1 次 os.Executable + 几次 os.Stat，相比启动一个进程
// 可以忽略；而缓存会带来「首次解析结果决定一切」的隐性问题 —— 测试里用
// t.Setenv 指定 GTSP_BUSYBOX 时，缓存会让它静默失效并回退到 Git Bash，
// 于是「busybox 可用」这条断言会变成假绿。
func ResolveShell() (string, error) {
	// 1. 显式覆盖（测试 / 自定义部署）
	if p := strings.TrimSpace(os.Getenv("GTSP_BUSYBOX")); p != "" {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			return "", fmt.Errorf("GTSP_BUSYBOX=%q is not an existing file", p)
		}
		return p, nil
	}

	// 2. 可执行文件同目录
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, name := range busyboxCandidates {
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}

	// 3. PATH
	for _, name := range busyboxCandidates {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}

	// 4. 回退 bash（拒绝 WSL）
	if p, err := exec.LookPath("bash"); err == nil {
		if IsWSLBash(p) {
			return "", fmt.Errorf(
				"found WSL bash at %q, which runs commands inside a Linux VM "+
					"with a different filesystem; refusing to use it. "+
					"Either place busybox64u.exe next to gtsp.exe (see README), "+
					"or install Git for Windows and ensure its bash is used instead", p)
		}
		return p, nil
	}

	return "", errors.New(
		"no shell available: busybox not found next to gtsp.exe and not in PATH, " +
			"and no usable bash found. Place busybox64u.exe in the same directory as gtsp.exe")
}

// LogShellResolution 记录 execute_bash 实际会使用的 shell，以及它是随包
// 分发的 busybox 还是回退的 bash（通常是 Git for Windows）。
//
// 在启动时调用一次。不做缓存，ResolveShell 仍按每次调用重新解析；
// 日志只用于让「命令到底在哪个 shell 里跑」在日志里可查。
func LogShellResolution() {
	shell, err := ResolveShell()
	if err != nil {
		log.Printf("execute_bash: shell resolution failed: %v", err)
		return
	}
	kind := "bash fallback"
	if IsBusybox(shell) {
		kind = "busybox"
	}
	log.Printf("execute_bash: shell resolved: %s (%s)", shell, kind)
}

// IsBusybox 判断路径是否是 busybox（按文件名前缀）。
// ShellCommand 据此决定是否插入 `sh` applet 参数。
func IsBusybox(path string) bool {
	return strings.HasPrefix(strings.ToLower(filepath.Base(path)), "busybox")
}

// IsWSLBash 判断 path 是否指向 WSL 的 bash.exe（System32 或 Sysnative）。
// 该二进制把命令交给 Linux 虚拟机执行，进程与文件系统都不在 Windows 一侧。
func IsWSLBash(path string) bool {
	p := strings.ToLower(filepath.Clean(path))
	return strings.Contains(p, `\system32\bash.exe`) || strings.Contains(p, `\sysnative\bash.exe`)
}

// xFlag 允许用 GTSP_BUSYBOX_X=1 关闭 busybox 的反斜杠→正斜杠转换。
// 某些 Windows 程序不接受正斜杠形式的路径参数时，用它可以救回来。
func xFlag() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("GTSP_BUSYBOX_X")))
	return v == "1" || v == "true" || v == "yes"
}
