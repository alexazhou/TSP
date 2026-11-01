//go:build windows

package unit_test

import (
	"os"
	"path/filepath"
	"testing"
)

// busyboxRelPath 是 release 分发的 busybox 相对本包目录的位置。
// 本包目录是 gtsp/test/unit，故用 ../.. 回到 gtsp/。
const busyboxRelPath = "../../third_party/busybox/busybox64u.exe"

// pinTestShell 在 Windows 上把 shell 钉到仓库内自带的 busybox。
//
// 不钉的话：`go test` 下 os.Executable() 指向 $TMPDIR/go-buildXXXX/b001/pkg.test，
// 那个目录里没有 busybox64u.exe，于是 ResolveShell() 回退到 PATH 上的 bash ——
// 在 windows-latest runner 上就是 Git for Windows 的 bash。
// 后果是这些用例「绿了也不代表 busybox 这条路可用」，而且它们的断言（尤其
// `xyz` 的 stderr 文案）会随着实际用到哪个 shell 而变，红绿都不说明问题。
func pinTestShell(t *testing.T) {
	t.Helper()
	p, err := filepath.Abs(busyboxRelPath)
	if err != nil {
		t.Fatalf("resolve busybox path: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("busybox 未就位: %v（release 需要它随 gtsp.exe 分发）", err)
	}
	t.Setenv("GTSP_BUSYBOX", p)
}
