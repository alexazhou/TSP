package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSandbox_PathRulesAndPermissions(t *testing.T) {
	t.Run("S1_write_whitelist_cli_and_sandbox_rpc", func(t *testing.T) {
		workdir, _ := filepath.EvalSymlinks(t.TempDir())
		outsideDir, _ := filepath.EvalSymlinks(t.TempDir())
		subDir := filepath.Join(workdir, "sub")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatal(err)
		}

		s := startGTSP(t,
			withWorkdir(workdir),
			withExtraArgs("--sandbox", "--access-root", workdir, "--allow-write", workdir, "--allow-read", workdir),
		)

		// Inside workdir succeeds
		s.callTool("write_file", map[string]any{
			"file_path": filepath.Join(workdir, "ok.txt"),
			"content":   "allowed",
		})

		// Outside workdir is rejected
		resp, err := s.callToolRaw("write_file", map[string]any{
			"file_path": filepath.Join(outsideDir, "escape.txt"),
			"content":   "blocked",
		})
		if err != nil {
			t.Fatalf("transport error: %v", err)
		}
		if typ, _ := resp["type"].(string); typ != "error" {
			t.Fatalf("expected sandbox error writing outside workdir, got %v", resp)
		}

		// Narrow write rule via sandbox RPC to subDir only
		sbResp, err := s.callMethod("sandbox", map[string]any{
			"read":  []map[string]string{{"action": "allow", "path": workdir}},
			"write": []map[string]string{{"action": "allow", "path": subDir}},
		})
		if err != nil {
			t.Fatalf("sandbox rpc failed: %v", err)
		}
		if typ, _ := sbResp["type"].(string); typ == "error" {
			t.Fatalf("sandbox rpc error: %v", sbResp)
		}

		s.callTool("write_file", map[string]any{
			"file_path": filepath.Join(subDir, "in_sub.txt"),
			"content":   "sub ok",
		})
		resp2, err := s.callToolRaw("write_file", map[string]any{
			"file_path": filepath.Join(workdir, "now_denied.txt"),
			"content":   "nope",
		})
		if err != nil {
			t.Fatalf("transport error: %v", err)
		}
		if typ, _ := resp2["type"].(string); typ != "error" {
			t.Fatalf("expected error after narrowing write rule via sandbox RPC, got %v", resp2)
		}
	})

	t.Run("S3_tool_exclude_permission", func(t *testing.T) {
		s := startGTSP(t, withExclude("execute_bash", "write_file"))
		resp, err := s.callToolRaw("execute_bash", map[string]any{"command": "echo nope"})
		if err != nil {
			t.Fatalf("transport error: %v", err)
		}
		if typ, _ := resp["type"].(string); typ != "error" {
			t.Fatalf("expected error calling excluded execute_bash, got %v", resp)
		}
	})
}

func waitForPIDFile(t *testing.T, path string) []int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) > 0 {
			return readPIDFile(t, path)
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for pid file %s", path)
	return nil
}

func TestMultiSession_WebSocketAndLogging(t *testing.T) {
	t.Run("M1_M3_websocket_parallel_sessions_and_disconnect_isolation", func(t *testing.T) {
		workdir := t.TempDir()
		srv := startGTSPWS(t, withWorkdir(workdir))

		ws1 := wsConnect(t, srv.wsURL)
		ws2 := wsConnect(t, srv.wsURL)

		// M3: WebSocket F1 + P1 parity with stdio
		ws1.callTool("write_file", map[string]any{"file_path": "ws.txt", "content": "ws_hello"})
		r := ws1.callTool("read_file", map[string]any{"file_path": "ws.txt"})
		if got, _ := r["content"].(string); got != "   1│ws_hello\n" {
			t.Fatalf("ws read_file=%q, want %q", got, "   1│ws_hello\n")
		}
		b := ws2.callTool("execute_bash", map[string]any{"command": "echo hello_ws"})
		if !strings.Contains(b["stdout"].(string), "hello_ws") {
			t.Fatalf("ws execute_bash stdout=%q", b["stdout"])
		}

		// M1: Each session spawns its own background process; closing ws1 kills only ws1's process
		ws1.callTool("execute_bash", map[string]any{
			"command":           "sleep 30 >/dev/null 2>&1 & echo $! > ws1.pid",
			"run_in_background": true,
		})
		ws2.callTool("execute_bash", map[string]any{
			"command":           "sleep 30 >/dev/null 2>&1 & echo $! > ws2.pid",
			"run_in_background": true,
		})

		ws1PIDs := waitForPIDFile(t, filepath.Join(workdir, "ws1.pid"))
		ws2PIDs := waitForPIDFile(t, filepath.Join(workdir, "ws2.pid"))

		ws1.Close()

		// Wait for ws1's process to be terminated by KillSession
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if len(osSurvivors(t, ws1PIDs)) == 0 {
				break
			}
			time.Sleep(30 * time.Millisecond)
		}
		if surv := osSurvivors(t, ws1PIDs); len(surv) != 0 {
			t.Fatalf("ws1 background process survived WS disconnect: %v", surv)
		}
		if surv := osSurvivors(t, ws2PIDs); len(surv) != len(ws2PIDs) {
			t.Fatalf("ws2 background process was killed when ws1 disconnected: surv=%v want=%v", surv, ws2PIDs)
		}

		// ws2 remains functional
		b2 := ws2.callTool("execute_bash", map[string]any{"command": "echo ws2_still_ok"})
		if !strings.Contains(b2["stdout"].(string), "ws2_still_ok") {
			t.Errorf("ws2 broken after ws1 disconnect: %v", b2)
		}
	})

	t.Run("M2_session_logs_isolated", func(t *testing.T) {
		workdir := t.TempDir()
		logDir := filepath.Join(workdir, "shared_logs")
		srv := startGTSPWS(t, withWorkdir(workdir), withExtraArgs("--log-path", logDir))

		wsA := wsConnect(t, srv.wsURL)
		wsA.callTool("execute_bash", map[string]any{"command": "echo MARKER_SESSION_A"})
		wsA.Close()

		// Brief pause so session IDs (timestamp-based) are distinct
		time.Sleep(20 * time.Millisecond)

		wsB := wsConnect(t, srv.wsURL)
		wsB.callTool("execute_bash", map[string]any{"command": "echo MARKER_SESSION_B"})
		wsB.Close()

		entries, err := filepath.Glob(filepath.Join(logDir, "gt-agent-*.log"))
		if err != nil {
			t.Fatalf("glob logs: %v", err)
		}
		if len(entries) < 2 {
			t.Fatalf("expected at least 2 session log files, got %d (%v)", len(entries), entries)
		}

		sawAOnly, sawBOnly := false, false
		for _, p := range entries {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read log %s: %v", p, err)
			}
			txt := string(data)
			hasA := strings.Contains(txt, "MARKER_SESSION_A")
			hasB := strings.Contains(txt, "MARKER_SESSION_B")
			if hasA && hasB {
				t.Fatalf("session log %s mixed markers from both sessions", p)
			}
			if hasA {
				sawAOnly = true
			}
			if hasB {
				sawBOnly = true
			}
		}
		if !sawAOnly || !sawBOnly {
			t.Errorf("expected separate logs for session A and B (sawA=%v, sawB=%v)", sawAOnly, sawBOnly)
		}
	})
}
