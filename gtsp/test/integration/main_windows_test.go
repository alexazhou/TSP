//go:build windows

package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain 在 Windows 上把整个测试二进制用的 shell 钉到自带 busybox。
//
// 为什么用 TestMain 而不是逐个测试加 pinTestShell：integration 包里有多处
// 真实 execute_bash 调用（并发测试、WebSocket 测试等），逐个加必然漏。
// 漏掉的那个会静默回退到 Git for Windows 的 bash —— 命令照样跑得起来、
// 测试照样绿，于是「busybox 这条路可用」在这些用例上完全没被覆盖。
//
// 与 pinTestShell 的区别：pinTestShell 用 t.Setenv（测试结束自动还原），
// 适合单测；TestMain 跑在任何测试之前、拿不到 *testing.T，只能用 os.Setenv。
// 两者不冲突：pinTestShell 会覆盖成同样的值。
func TestMain(m *testing.M) {
	p, err := filepath.Abs(busyboxRelPath)
	if err == nil {
		if _, statErr := os.Stat(p); statErr == nil {
			os.Setenv("GTSP_BUSYBOX", p)
		} else {
			// 不在这里 Fatal：让真正需要 busybox 的用例各自报出更具体的错误，
			// 而 TestBusyboxBinaryUsable 会明确说「文件不在」。
			fmt.Fprintf(os.Stderr, "integration: busybox 未就位，未设置 GTSP_BUSYBOX: %v\n", statErr)
		}
	}
	os.Exit(m.Run())
}
