//go:build !windows

package integration_test

import (
	"os"
	"testing"
)

// TestMain 在 Unix 上是直通：execute_bash 本来就用 PATH 里的 bash，
// 没有「回退到另一个 shell」的歧义，不需要钉任何东西。
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
