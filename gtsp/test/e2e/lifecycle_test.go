package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLifecycle_InitializeAndCapabilities(t *testing.T) {
	t.Run("L1_full_initialize_matches_schema", func(t *testing.T) {
		s := startGTSP(t)
		result, _ := s.initResp["result"].(map[string]any)
		if result == nil {
			t.Fatalf("missing result in initialize: %v", s.initResp)
		}
		serverInfo, _ := result["serverInfo"].(map[string]any)
		if name, _ := serverInfo["name"].(string); name != "gTSP" {
			t.Errorf("expected serverInfo.name=gTSP, got %q", name)
		}
		if ver, _ := serverInfo["version"].(string); ver == "" {
			t.Error("expected non-empty serverInfo.version")
		}
		initNames := toolNamesFromInit(t, s.initResp)

		out, err := exec.Command(gtspBinPath, "schema").Output()
		if err != nil {
			t.Fatalf("gtsp schema failed: %v", err)
		}
		var schemaTools []map[string]any
		if err := json.Unmarshal(out, &schemaTools); err != nil {
			t.Fatalf("unmarshal schema: %v", err)
		}
		if len(initNames) != len(schemaTools) {
			t.Fatalf("initialize tool count (%d) != schema tool count (%d)", len(initNames), len(schemaTools))
		}
		for _, st := range schemaTools {
			n, _ := st["name"].(string)
			if !containsStr(initNames, n) {
				t.Errorf("tool %q from schema missing in initialize", n)
			}
		}
	})

	t.Run("L2_capabilities_include_filter", func(t *testing.T) {
		s := startGTSP(t, withInclude("read_file"))
		names := toolNamesFromInit(t, s.initResp)
		if len(names) != 1 || names[0] != "read_file" {
			t.Fatalf("expected only [read_file], got %v", names)
		}
		resp, err := s.callToolRaw("write_file", map[string]any{"file_path": "a.txt", "content": "x"})
		if err != nil {
			t.Fatalf("transport error: %v", err)
		}
		if typ, _ := resp["type"].(string); typ != "error" {
			t.Fatalf("expected error calling excluded tool, got %v", resp)
		}
	})

	t.Run("L3_schema_subcommand_to_file", func(t *testing.T) {
		outFile := filepath.Join(t.TempDir(), "schema.json")
		if out, err := exec.Command(gtspBinPath, "schema", "-o", outFile).CombinedOutput(); err != nil {
			t.Fatalf("gtsp schema -o failed: %v\n%s", err, out)
		}
		data, err := os.ReadFile(outFile)
		if err != nil {
			t.Fatalf("read schema file: %v", err)
		}
		var arr []map[string]any
		if err := json.Unmarshal(data, &arr); err != nil || len(arr) == 0 {
			t.Fatalf("invalid schema JSON array: err=%v len=%d", err, len(arr))
		}
	})
}

func TestLifecycle_ErrorResilience(t *testing.T) {
	s := startGTSP(t)

	t.Run("L4_unknown_tool", func(t *testing.T) {
		resp, err := s.callToolRaw("nonexistent_tool", map[string]any{})
		if err != nil {
			t.Fatalf("transport error: %v", err)
		}
		if typ, _ := resp["type"].(string); typ != "error" {
			t.Fatalf("expected error for unknown tool, got %v", resp)
		}
	})

	t.Run("L5_malformed_json_and_invalid_params", func(t *testing.T) {
		resp, err := s.callRawLine([]byte(`{"id":"bad","method":`))
		if err != nil {
			t.Fatalf("transport error on malformed json: %v", err)
		}
		if typ, _ := resp["type"].(string); typ != "error" {
			t.Fatalf("expected error for malformed json, got %v", resp)
		}

		// Missing required field for read_file
		resp2, err := s.callToolRaw("read_file", map[string]any{})
		if err != nil {
			t.Fatalf("transport error on missing required field: %v", err)
		}
		if typ, _ := resp2["type"].(string); typ != "error" {
			t.Fatalf("expected error for missing file_path, got %v", resp2)
		}

		// Invalid param type
		resp3, err := s.callToolRaw("read_file", map[string]any{"file_path": 12345})
		if err != nil {
			t.Fatalf("transport error on wrong type: %v", err)
		}
		if typ, _ := resp3["type"].(string); typ != "error" {
			t.Fatalf("expected error for wrong param type, got %v", resp3)
		}
	})

	// Session must still be usable after L4 and L5 errors
	res := s.callTool("execute_bash", map[string]any{"command": "echo still_alive"})
	if ok, _ := res["success"].(bool); !ok {
		t.Fatalf("session broken after errors: %v", res)
	}
}

func spawnTrackedBackgroundChild(t *testing.T, s *TSPSession, pidFile string) []int {
	t.Helper()
	absPIDFile := filepath.Join(s.workdir, pidFile)
	cmd := "sleep 30 >/dev/null 2>&1 & echo $! > " + pidFile
	s.callTool("execute_bash", map[string]any{
		"command":           cmd,
		"run_in_background": true,
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(absPIDFile); err == nil && len(data) > 0 {
			pids := readPIDFile(t, absPIDFile)
			if len(pids) > 0 {
				return pids
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for pid file %s", absPIDFile)
	return nil
}

func TestLifecycle_ExitPathsCleanUpProcesses(t *testing.T) {
	t.Run("L6_shutdown_rpc", func(t *testing.T) {
		s := startGTSP(t)
		pids := spawnTrackedBackgroundChild(t, s, "l6.pid")
		if len(osSurvivors(t, pids)) == 0 {
			t.Fatalf("expected child %v to be alive before shutdown", pids)
		}
		resp, err := s.callMethod("shutdown", map[string]any{})
		if err != nil {
			t.Fatalf("shutdown rpc failed: %v", err)
		}
		if typ, _ := resp["type"].(string); typ == "error" {
			t.Fatalf("shutdown returned error: %v", resp)
		}
		deadline := time.Now().Add(2 * time.Second)
		for len(osSurvivors(t, pids)) > 0 && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if surv := osSurvivors(t, pids); len(surv) > 0 {
			t.Fatalf("background child survived shutdown: %v", surv)
		}
		s.closeStdin()
		if err := s.waitExit(5 * time.Second); err != nil {
			t.Fatalf("gtsp did not exit after shutdown + stdin EOF: %v", err)
		}
	})

	t.Run("L7_stdin_eof", func(t *testing.T) {
		s := startGTSP(t)
		pids := spawnTrackedBackgroundChild(t, s, "l7.pid")
		if len(osSurvivors(t, pids)) == 0 {
			t.Fatalf("expected child %v to be alive before EOF", pids)
		}
		s.closeStdin()
		if err := s.waitExit(5 * time.Second); err != nil {
			t.Fatalf("gtsp did not exit on stdin EOF: %v", err)
		}
		if surv := osSurvivors(t, pids); len(surv) > 0 {
			t.Fatalf("background child survived stdin EOF: %v", surv)
		}
	})

	t.Run("L8_sigterm", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("SIGTERM signal cleanup is Unix-specific (Windows hard-kill relies on Job Object KILL_ON_JOB_CLOSE)")
		}
		s := startGTSP(t)
		pids := spawnTrackedBackgroundChild(t, s, "l8.pid")
		if len(osSurvivors(t, pids)) == 0 {
			t.Fatalf("expected child %v to be alive before SIGTERM", pids)
		}
		s.killSignal()
		if err := s.waitExit(5 * time.Second); err != nil {
			t.Fatalf("gtsp did not exit on SIGTERM: %v", err)
		}
		if surv := osSurvivors(t, pids); len(surv) > 0 {
			t.Fatalf("background child survived SIGTERM: %v", surv)
		}
	})
}
