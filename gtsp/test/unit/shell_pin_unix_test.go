//go:build !windows

package unit_test

import "testing"

// pinTestShell 在 Unix 上是空操作：execute_bash 本来就用 PATH 里的 bash，
// 不存在「回退到另一个 shell」的歧义。
func pinTestShell(t *testing.T) {
	t.Helper()
}
