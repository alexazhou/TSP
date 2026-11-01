//go:build windows

package integration_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestProbeBusyboxPathSemantics 探针：把 busybox ash 在 Windows 上的路径行为
// 原样打出来，供文档与工具描述引用。
//
// 存在的理由：工具描述里要告诉用户「路径怎么写」，这类断言不能靠推断。
// busybox-w32 官方 README 只说绝对路径是 `c:/path` 或 `//host/share/path`，
// 对 `/c/path` 形式是否可用没有明说，而社区文章到处在传 `cd /c/` ——
// 两者冲突，必须以实测为准。
//
// 本测试只打日志、不做断言：它的产出是「事实」，断言等事实确定后再补，
// 避免把猜测写成断言。
func TestProbeBusyboxPathSemantics(t *testing.T) {
	busybox, err := filepath.Abs(busyboxRelPath)
	if err != nil {
		t.Fatalf("resolve busybox path: %v", err)
	}

	cases := []struct {
		name   string
		script string
	}{
		{"pwd_default", `pwd`},
		{"PWD_var", `echo "$PWD"`},
		{"cd_c_slash_form", `cd /c/ && pwd`},
		{"cd_c_colon_form", `cd c:/ && pwd`},
		{"cd_backslash_form", `cd c:\ && pwd`},
		{"cd_drive_only", `cd C: && pwd`},
		{"cd_drive_only_rc", `cd C:`},
		{"list_c_via_slashc", `cd /c/ && ls -d Windows 2>&1 | head -1`},
		{"list_c_via_cslash", `cd c:/ && ls -d Windows 2>&1 | head -1`},
		{"backslash_in_echo", `echo 'C:\Windows\System32'`},
		{"backslash_unquoted_arg", `printf '%s\n' C:\Windows\System32`},
		{"abs_path_test_e", `test -d 'c:/Windows' && echo YES || echo NO`},
		{"abs_path_test_slashc", `test -d '/c/Windows' && echo YES || echo NO`},
		{"abs_path_test_dslashslash", `test -d '//c/Windows' && echo YES || echo NO`},
		{"temp_dir", `echo "$TMPDIR|$TEMP|$TMP"`},
		{"cygpath_exists", `command -v cygpath >/dev/null && echo YES || echo NO`},
		{"realpath_c", `realpath c:/Windows 2>&1 | head -1`},
		{"colon_in_path_arg", `ls -d c:/Windows >/dev/null 2>&1 && echo OK || echo FAIL`},
	}

	for _, tc := range cases {
		out, err := exec.Command(busybox, "sh", "-c", tc.script).CombinedOutput()
		rc := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				rc = ee.ExitCode()
			} else {
				rc = -1
			}
		}
		t.Logf("PROBE %-28s rc=%d out=%q", tc.name, rc, strings.TrimRight(string(out), "\r\n"))
	}

	// -X 关闭反斜杠转换后的行为，同样要摸清（工具暴露了 GTSP_BUSYBOX_X）。
	for _, tc := range []struct{ name, script string }{
		{"X_backslash_arg", `printf '%s\n' C:\Windows\System32`},
		{"X_echo_quoted", `echo 'C:\Windows\System32'`},
	} {
		out, err := exec.Command(busybox, "sh", "-X", "-c", tc.script).CombinedOutput()
		t.Logf("PROBE %-28s err=%v out=%q", tc.name, err, strings.TrimRight(string(out), "\r\n"))
	}
}
