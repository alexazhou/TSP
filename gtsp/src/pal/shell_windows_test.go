//go:build windows

package pal

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsWSLBash 锁定 WSL 检测规则。
//
// WSL 的 bash.exe 会把命令交给 Linux 虚拟机执行：进程不在 Windows 内核里、
// 文件系统与 Windows 分离。gtsp 若用它，read_file/write_file 与 execute_bash
// 会看到两个不同的根目录，属于「静默跑错地方」，必须拒绝。
func TestIsWSLBash(t *testing.T) {
	wsl := []string{
		`C:\Windows\System32\bash.exe`,
		`c:\windows\system32\bash.exe`,
		`C:/Windows/System32/bash.exe`,
		`C:\Windows\Sysnative\bash.exe`,
		`C:\Windows\System32\bash.exe `,
	}
	for _, p := range wsl {
		if !IsWSLBash(p) {
			t.Errorf("IsWSLBash(%q) = false, want true", p)
		}
	}

	notWSL := []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
		`D:\tools\msys64\usr\bin\bash.exe`,
		`C:\gtsp\busybox64u.exe`,
		`bash.exe`,
		``,
	}
	for _, p := range notWSL {
		if IsWSLBash(p) {
			t.Errorf("IsWSLBash(%q) = true, want false", p)
		}
	}
}

// TestIsBusybox 确认「是不是 busybox」的判定只看文件名，
// 因为 ShellCommand 要据此决定是否插入 `sh` applet 参数。
func TestIsBusybox(t *testing.T) {
	yes := []string{`C:\gtsp\busybox64u.exe`, `C:\gtsp\busybox64.exe`, `C:\gtsp\busybox.exe`, `busybox64u.exe`}
	for _, p := range yes {
		if !IsBusybox(p) {
			t.Errorf("IsBusybox(%q) = false, want true", p)
		}
	}
	no := []string{`C:\Program Files\Git\bin\bash.exe`, `C:\Windows\System32\bash.exe`}
	for _, p := range no {
		if IsBusybox(p) {
			t.Errorf("IsBusybox(%q) = true, want false", p)
		}
	}
}

// TestResolveShellHonorsEnv 验证 GTSP_BUSYBOX 覆盖确实生效。
//
// 这条测试守护一个具体的历史坑：ResolveShell 早期用 sync.Once 缓存结果，
// 导致 t.Setenv 设置的 GTSP_BUSYBOX 被忽略、静默回退到 Git Bash ——
// 于是「busybox 可用」的断言变成假绿。缓存已移除，这里把它钉住。
func TestResolveShellHonorsEnv(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable 不可用: %v", err)
	}
	t.Setenv("GTSP_BUSYBOX", self)

	got, err := ResolveShell()
	if err != nil {
		t.Fatalf("ResolveShell: %v", err)
	}
	if got != self {
		t.Errorf("ResolveShell() = %q, want %q（GTSP_BUSYBOX 覆盖未生效）", got, self)
	}
}

// TestResolveShellRejectsMissingEnv 验证 GTSP_BUSYBOX 指向不存在的文件时报错，
// 而不是静默回退（静默回退会让测试在错误的 shell 上跑出假绿）。
func TestResolveShellRejectsMissingEnv(t *testing.T) {
	t.Setenv("GTSP_BUSYBOX", `C:\definitely\not\here\busybox64u.exe`)
	if _, err := ResolveShell(); err == nil {
		t.Fatal("ResolveShell 应拒绝不存在的 GTSP_BUSYBOX，实际成功回退")
	}
}

// TestShellCommandXFlag 验证 GTSP_BUSYBOX_X 正确接线到 `sh -X -c` 的参数位置。
//
// 这条测试之前完全缺位（`grep GTSP_BUSYBOX_X --include=*_test.go` = 0 命中），
// 而 -X 的位置有硬约束：它必须出现在 `sh` **之后**、`-c` **之前**。
// 写成 `busybox -X sh -c ...` 会让 busybox 把 -X 当 applet 名 → 127。
// 这里直接检查构造出的 argv，不依赖能否真正执行。
//
// 注意：GTSP_BUSYBOX 必须指向一个**文件名以 busybox 开头**的路径，
// 否则 IsBusybox() 为 false，ShellCommand 走非 busybox 分支、不会插入 `sh`
// applet，断言就测不到 -X。用 os.Executable()（pal.test.exe）会踩这个坑。
func TestShellCommandXFlag(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "busybox64u.exe")
	if err := os.WriteFile(fake, []byte("not a real binary"), 0o644); err != nil {
		t.Fatalf("创建假 busybox 失败: %v", err)
	}
	t.Setenv("GTSP_BUSYBOX", fake)

	// 先确认前提成立：这个路径必须被判定为 busybox，否则下面的断言毫无意义。
	if !IsBusybox(fake) {
		t.Fatalf("IsBusybox(%q) = false，-X 断言将测不到（ShellCommand 走非 busybox 分支）", fake)
	}

	cases := []struct {
		name string
		env  string
		want []string // 期望的 argv（含程序名）
	}{
		{"x_off", "", []string{fake, "sh", "-c", "echo hi"}},
		{"x_on_1", "1", []string{fake, "sh", "-X", "-c", "echo hi"}},
		{"x_on_true", "true", []string{fake, "sh", "-X", "-c", "echo hi"}},
		{"x_on_yes", "yes", []string{fake, "sh", "-X", "-c", "echo hi"}},
		{"x_on_spaces", "  1  ", []string{fake, "sh", "-X", "-c", "echo hi"}},
		{"x_off_zero", "0", []string{fake, "sh", "-c", "echo hi"}},
		{"x_off_garbage", "maybe", []string{fake, "sh", "-c", "echo hi"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GTSP_BUSYBOX_X", tc.env)
			cmd, err := ShellCommand("echo hi")
			if err != nil {
				t.Fatalf("ShellCommand: %v", err)
			}
			if got, want := len(cmd.Args), len(tc.want); got != want {
				t.Fatalf("argv 长度 = %d, want %d（argv=%q）", got, want, cmd.Args)
			}
			for i := range tc.want {
				if cmd.Args[i] != tc.want[i] {
					t.Errorf("argv[%d] = %q, want %q（完整 argv=%q）", i, cmd.Args[i], tc.want[i], cmd.Args)
				}
			}
		})
	}
}
