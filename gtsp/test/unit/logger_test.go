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

// TestInitLoggerErrorIsActionable 钉住 InitLogger 失败时的报错：必须带出
// 实际失败的路径与底层原因，调用方据此判断是哪个目录、为什么建不出来。
//
// 不承载特定部署环境（例如只读根文件系统）的建议 —— 那不是 gtsp 的职责。
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

	// 2) 必须带出底层原因，便于区分 EROFS / EACCES / 路径不存在。
	//
	//    判据不能写死措辞，也不能只认 "mkdir" 字样：
	//    - 只断 "mkdir" 会**假绿**：实测变异「保留 PathError.Op、丢掉底层 Errno」
	//      时报文仍含 "mkdir"，测试照样 PASS，只能验「进了这个分支」，
	//      验不了「带了原因」。
	//    - 锁死 "not a directory" 会在 **Windows 上红**：该原因来自
	//      os.MkdirAll 的 `&PathError{Err: syscall.ENOTDIR}`，而 Windows 上
	//      ENOTDIR == ERROR_PATH_NOT_FOUND(3)，文案是系统消息表的
	//      "The system cannot find the path specified."，根本没有 "not a directory"。
	//
	//    所以改成「拿同样的输入自己跑一遍 MkdirAll，要求报错里包含它返回的原文」。
	//    这样与平台、与语言环境、与措辞都无关，且对上面两种变异都会红。
	wantInner := os.MkdirAll(badPath, 0o755)
	if wantInner == nil {
		t.Fatalf("前提不成立：自己调 MkdirAll(%q) 竟然成功，本用例的构造已失效", badPath)
	}
	if !strings.Contains(msg, wantInner.Error()) {
		t.Errorf("报错未带出底层原因:\n  期望包含: %s\n  实际:     %s", wantInner.Error(), msg)
	}
}

// TestInitLoggerOpenFileErrorIsActionable 覆盖第二个分支：
// 目录存在但日志文件打不开（MkdirAll 成功、OpenFile 失败）时报错同样带出
// 文件路径与底层原因。
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
}
