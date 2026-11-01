package unit_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gTSP/src/api"
)

// mentionsPath 判断报错文本是否提到了 path。
//
// 必须同时接受「原样」和「%q 转义后」两种形式：报错里用的是 %q，
// 而 %q 在 Windows 上会把 `C:\a\b` 转成 `C:\\a\\b`（反斜杠加倍）。
// 只比对原样路径会在 Unix 上过、在 Windows 上红 —— 本测试第一版就踩了这个坑。
func mentionsPath(msg, path string) bool {
	return strings.Contains(msg, path) || strings.Contains(msg, strconv.Quote(path))
}

// TestInitLoggerErrorIsActionable 钉住 #108 的报错文案。
//
// 背景：`--read-only` 下 gtsp 默认 logPath 是 /logs，根文件系统只读 →
// MkdirAll 失败。旧文案只说「use --log-path to choose a writable directory」，
// 用户照做仍然 EROFS（镜像里没有第二个可写路径），等于给了一条
// **在只读根文件系统下不可执行**的建议。
//
// 实测 v0.4.18 的各场景：
//
//	--read-only --log-path /dev/shm/logs  → rc=0  ✅ 例外成立
//	--read-only（默认 /logs）              → rc=1  ❌ EROFS
//	--read-only --tmpfs /tmp（不改路径）   → rc=1  ❌
//	--read-only --log-path /tmp（已存在）  → rc=1  ❌ 且**无任何建议**
//
// 所以文案必须同时提到：可写的 --log-path，以及只读根文件系统下要挂 tmpfs。
// 这条测试防的是「文案被改回不够可执行」的回归。
func TestInitLoggerErrorIsActionable(t *testing.T) {
	// 造一个必然让 MkdirAll 失败的路径：拿一个已存在的**普通文件**当父目录。
	// 跨平台稳定，不依赖权限位或只读挂载。
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
	if !mentionsPath(msg, badPath) {
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
	//    不同平台的措辞不同（Unix: "not a directory"，Windows: "cannot find the path"），
	//    所以只要求非空原因，不锁具体措辞。
	if !strings.Contains(msg, "mkdir") && !strings.Contains(msg, "Mkdir") {
		t.Errorf("报错未带出底层原因（缺少 mkdir 相关描述）:\n%s", msg)
	}
}

// TestInitLoggerOpenFileErrorIsActionable 覆盖第二个分支：
// 目录**存在**但日志文件写入失败（MkdirAll 成功、OpenFile 失败）时同样要给可执行建议。
//
// 早前这个分支只写 "failed to open log file: %v"，在 --read-only 下
// （例如 --log-path 指向已存在的 /tmp）用户得不到任何提示。
//
// 造失败的方式：在目录里先建一个**同名目录** gt-agent.log。于是 MkdirAll(dir)
// 成功，而 OpenFile(dir/gt-agent.log) 必然失败（目标是目录）。
// 这样不依赖 chmod，跨平台一致 —— 之前用 chmod 0o555 + Skip Windows 的写法
// 会让这个分支在 Windows 上永远测不到。
func TestInitLoggerOpenFileErrorIsActionable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建日志目录失败: %v", err)
	}
	// 用目录占住日志文件名，让 OpenFile 必失败。
	if err := os.Mkdir(filepath.Join(dir, "gt-agent.log"), 0o755); err != nil {
		t.Fatalf("占位目录创建失败: %v", err)
	}

	err := api.InitLogger(dir)
	if err == nil {
		t.Fatalf("InitLogger(%q) 应失败（gt-agent.log 是目录），实际成功", dir)
	}
	msg := err.Error()

	if !mentionsPath(msg, filepath.Join(dir, "gt-agent.log")) {
		t.Errorf("OpenFile 失败的报错未带出日志文件路径:\n%s", msg)
	}
	if !strings.Contains(msg, "--log-path") {
		t.Errorf("OpenFile 失败的报错未提到 --log-path:\n%s", msg)
	}
	if !strings.Contains(msg, "--tmpfs") {
		t.Errorf("OpenFile 失败的报错未提到 --tmpfs（应与 MkdirAll 分支一致）:\n%s", msg)
	}
}
