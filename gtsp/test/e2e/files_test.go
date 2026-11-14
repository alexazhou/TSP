package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFiles_RoundtripAndExploration(t *testing.T) {
	s := startGTSP(t)

	t.Run("F1_write_and_read_roundtrip", func(t *testing.T) {
		content := "line1\nline2\nline3\nline4\n"
		wRes := s.callTool("write_file", map[string]any{
			"file_path": "hello.txt",
			"content":   content,
		})
		if int(wRes["written"].(float64)) != len(content) {
			t.Errorf("written=%v, want %d", wRes["written"], len(content))
		}

		rRes := s.callTool("read_file", map[string]any{
			"file_path": "hello.txt",
		})
		wantAll := "   1│line1\n   2│line2\n   3│line3\n   4│line4\n"
		if got, _ := rRes["content"].(string); got != wantAll {
			t.Errorf("read content=%q, want %q", got, wantAll)
		}
		if int(rRes["total_lines"].(float64)) != 4 {
			t.Errorf("total_lines=%v, want 4", rRes["total_lines"])
		}

		subRes := s.callTool("read_file", map[string]any{
			"file_path":  "hello.txt",
			"start_line": 2,
			"end_line":   3,
		})
		wantSub := "   2│line2\n   3│line3\n"
		if got, _ := subRes["content"].(string); got != wantSub {
			t.Errorf("line range content=%q, want %q", got, wantSub)
		}
	})

	t.Run("F2_edit_and_error", func(t *testing.T) {
		s.callTool("write_file", map[string]any{
			"file_path": "edit_me.txt",
			"content":   "alpha\nbeta\ngamma\n",
		})
		s.callTool("edit", map[string]any{
			"file_path":  "edit_me.txt",
			"old_string": "beta",
			"new_string": "BETA_UPDATED",
		})
		rRes := s.callTool("read_file", map[string]any{"file_path": "edit_me.txt"})
		wantEdited := "   1│alpha\n   2│BETA_UPDATED\n   3│gamma\n"
		if got, _ := rRes["content"].(string); got != wantEdited {
			t.Errorf("after edit content=%q, want %q", got, wantEdited)
		}

		errResp, err := s.callToolRaw("edit", map[string]any{
			"file_path":  "edit_me.txt",
			"old_string": "NOT_PRESENT",
			"new_string": "X",
		})
		if err != nil {
			t.Fatalf("transport error: %v", err)
		}
		if typ, _ := errResp["type"].(string); typ != "error" {
			t.Errorf("expected error when old_string missing, got %v", errResp)
		}
	})

	t.Run("F3_read_file_boundaries", func(t *testing.T) {
		resp, err := s.callToolRaw("read_file", map[string]any{"file_path": "does_not_exist.txt"})
		if err != nil {
			t.Fatalf("transport error: %v", err)
		}
		if typ, _ := resp["type"].(string); typ != "error" {
			t.Errorf("expected error for missing file, got %v", resp)
		}

		resp2, err := s.callToolRaw("read_file", map[string]any{
			"file_path":  "hello.txt",
			"start_line": 999,
		})
		if err != nil {
			t.Fatalf("transport error: %v", err)
		}
		if typ, _ := resp2["type"].(string); typ != "error" {
			t.Errorf("expected error for out-of-range start_line, got %v", resp2)
		}
	})

	t.Run("F4_list_dir", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Join(s.workdir, ".git"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(s.workdir, "__pycache__"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(s.workdir, "sub", "nested"), 0755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(s.workdir, ".git", "config"), []byte("x"), 0644)
		_ = os.WriteFile(filepath.Join(s.workdir, "__pycache__", "a.pyc"), []byte("x"), 0644)
		_ = os.WriteFile(filepath.Join(s.workdir, "sub", "nested", "deep.go"), []byte("package sub\n"), 0644)

		res := s.callTool("list_dir", map[string]any{
			"dir_path":  ".",
			"recursive": true,
			"depth":     3,
		})
		items, _ := res["items"].([]any)
		var paths []string
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				p, _ := m["path"].(string)
				paths = append(paths, filepath.ToSlash(p))
			}
		}
		joined := strings.Join(paths, "\n")
		if !strings.Contains(joined, "sub/nested/deep.go") {
			t.Errorf("expected sub/nested/deep.go in recursive list_dir, got:\n%s", joined)
		}
		if strings.Contains(joined, ".git/config") || strings.Contains(joined, "__pycache__/a.pyc") {
			t.Errorf("default ignores should hide .git and __pycache__ contents, got:\n%s", joined)
		}

		limRes := s.callTool("list_dir", map[string]any{
			"dir_path":        ".",
			"recursive":       true,
			"depth":           3,
			"limit":           1,
			"ignore_patterns": []string{"*.txt"},
		})
		if limItems, _ := limRes["items"].([]any); len(limItems) != 1 {
			t.Errorf("expected limit=1 to return 1 item, got %d", len(limItems))
		}
	})

	t.Run("F5_glob", func(t *testing.T) {
		res := s.callTool("glob", map[string]any{
			"pattern": "**/*.go",
			"path":    ".",
		})
		matches, _ := res["matches"].([]any)
		if len(matches) == 0 {
			t.Fatalf("expected at least 1 .go match, got %v", res)
		}
	})

	t.Run("F6_grep_search", func(t *testing.T) {
		s.callTool("write_file", map[string]any{
			"file_path": "grep_target.txt",
			"content":   "first\nNEEDLE_REGEX_123\nlast\n",
		})
		// Write a binary file that should be skipped
		_ = os.WriteFile(filepath.Join(s.workdir, "bin.dat"), []byte{0x00, 0x01, 'N', 'E', 'E', 'D', 'L', 'E'}, 0644)

		res := s.callTool("grep_search", map[string]any{
			"pattern": "NEEDLE_REGEX_[0-9]+",
			"path":    ".",
			"context": 1,
		})
		matches, _ := res["matches"].([]any)
		if len(matches) != 1 {
			t.Fatalf("expected 1 match (binary skipped), got %d: %v", len(matches), matches)
		}
		m := matches[0].(map[string]any)
		if fp, _ := m["file_path"].(string); !strings.HasSuffix(filepath.ToSlash(fp), "grep_target.txt") {
			t.Errorf("unexpected matched file: %q", fp)
		}
		if ln := int(m["line_number"].(float64)); ln != 2 {
			t.Errorf("expected line_number=2, got %d", ln)
		}
		if content, _ := m["content"].(string); content != "NEEDLE_REGEX_123" {
			t.Errorf("expected content=NEEDLE_REGEX_123, got %q", content)
		}

		// FixedStrings search
		res2 := s.callTool("grep_search", map[string]any{
			"pattern":       "NEEDLE_REGEX_[0-9]+",
			"path":          ".",
			"fixed_strings": true,
		})
		if m2, _ := res2["matches"].([]any); len(m2) != 0 {
			t.Errorf("expected 0 matches for literal regex pattern with fixed_strings=true, got %d", len(m2))
		}
	})
}
