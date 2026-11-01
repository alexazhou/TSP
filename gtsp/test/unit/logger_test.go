package unit_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gTSP/src/api"
)

// TestInitLoggerErrorIsActionable 钉住 #108 的报错文案。
//
// 背景：`--read-only` 下 gtsp 默认 logPath 是 /p/logs，根文件系统只读 →
// MkdirAll 失败。旧文案只说「use --log-path to choose a writable directory」，
// 用户照做仍然 EROFS（镜像里没有第二个可写路径），等于给了一条
// **在只读根文件系统下不可执行**的建议。
//
// 实测 v0.4.18 的各场景：
//
//	--read-only --log-path /dev/shm/logs  → rc=0  ✅ 例外成立
//	--read-only（默认 /p/logs）            → rc=1  ❌ EROFS
//	--read-only --tmpfs /tmp（不改路径）   → rc=1  ❌
//	--read-only --log-path /tmp（已存在）  → rc=1  ❌ 且**无任何建议**
//
// 所以文案必须同时提到：可写的 --log-path，以及只读根文件系统下要挂 tmpfs。
// 这条测试防的是「文案被改回不够可执行」的回归。
func TestInitLoggerErrorIsActionable(t *testing.T) {
	// 用一个**必然不可创建**的路径：拿一个已存在的普通文件当父目录，
	// 任何平台都会失败（跨平台稳定，不依赖权限或只读挂载）。
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "a_file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("准备阻塞文件失败: %v", err)
	}
	badPath := filepath.Join(blocker, "logs") // 父「目录」是文件 → MkdirAll 必失败

	err := api.InitLogger(badPath)
	if err == nil {
		t.Fatalf("InitLogger(%q) 应失败（父路径是文件），实际成功", badPath)
	}
	msg := err.Error()

	// 1) 必须带出实际失败的路径 —— 否则用户不知道是哪个目录出的问题。
	if !strings.Contains(msg, badPath) {
		t.Errorf("报错未包含失败的路径 %q:\n%s", badPath, msg)
	}

	// 2) 必须给出可执行的方向：--log-path。
	if !strings.Contains(msg, "--log-path") {
		t.Errorf("报错未提到 --log-path:\n%s", msg)
	}

	// 3) 必须点明只读根文件系统的额外一步（--tmpfs）。这是 #108 的核心，
	//    少了这句，--read-only 用户永远走不通。
	if !strings.Contains(msg, "--read-only") {
		t.Errorf("报错未提到 --read-only 场景:\n%s", msg)
	}
	if !strings.Contains(msg, "--tmpfs") {
		t.Errorf("报错未提到 --tmpfs（只读根文件系统下唯一可行的解法）:\n%s", msg)
	}

	// 4) 必须带底层原因，便于区分 EROFS / EACCES / 路径不存在。
	if !strings.Contains(msg, "not a directory") && !strings.Contains(msg, "not a dir") {
		t.Logf("提示：底层原因里未出现 'not a directory'，实际为:\n%s", msg)
	}
}

// TestInitLoggerOpenFileErrorIsActionable 覆盖第二个分支：
// 目录**存在**但写入失败（MkdirAll 成功、OpenFile 失败）时同样要给可执行建议。
//
// 早前这个分支只写 "failed to open log file: %v"，在 --read-only 下
// （例如 --log-path 指向已存在的 /tmp）用户得不到任何提示。
func TestInitLoggerOpenFileErrorIsActionable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("依赖把目录设为只读的语义，Windows 上 chmod 不生效")
	}
	if os.Geteuid() == 0 {
		t.Skip("root 不受目录权限限制，测不出 OpenFile 失败")
	}

	dir := filepath.Join(t.TempDir(), "readonly")
	if err := os.MkdirAll(dir, 0o555); err != nil {
		t.Fatalf("创建只读目录失败: %v", err)
	}
	// 确保清理时能删掉（权限不足会让 TempDir 清理失败）。
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := api.InitLogger(dir)
	if err == nil {
		t.Fatalf("InitLogger(%q) 应失败（目录只读），实际成功", dir)
	}
	msg := err.Error()

	if !strings.Contains(msg, "--log-path") {
		t.Errorf("OpenFile 失败的报错未提到 --log-path:\n%s", msg)
	}
	if !strings.Contains(msg, "--tmpfs") {
		t.Errorf("OpenFile 失败的报错未提到 --tmpfs（应与 MkdirAll 分支一致）:\n%s", msg)
	}
}
