package unit_test

import (
	"gTSP/src/api"
	"gTSP/src/tools"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestSession(allowPath string) api.Session {
	s := api.NewSession()
	s.SetInitialized(true)
	// Ensure server-level workdir root is broad for tests
	_ = api.SetWorkdirRoot("/")
	// For functional tests, we allow root access to avoid issues with symlinks (e.g. /var on macOS)
	rule := api.PathRule{Action: "allow", Path: "/"}
	s.SetPathRules([]api.PathRule{rule}, []api.PathRule{rule})
	s.SetNetworkAllowed(true)
	return s
}

func TestListDirHandler(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gt-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create some files and dirs
	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("test"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "subdir", "file2.txt"), []byte("test2"), 0644)

	session := setupTestSession(tmpDir)

	t.Run("basic list", func(t *testing.T) {
		params := json.RawMessage(`{"dir_path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `"}`)
		res, err := tools.ListDirHandler(session, params)
		if err != nil {
			t.Fatalf("ListDirHandler failed: %v", err)
		}
		result := res.(tools.ListDirResult)
		// file1.txt, subdir (excluding default ignores like .git)
		if len(result.Items) != 2 {
			t.Errorf("expected 2 items, got %d", len(result.Items))
		}
	})

	t.Run("recursive list", func(t *testing.T) {
		params := json.RawMessage(`{"dir_path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `", "recursive": true, "depth": 1}`)
		res, err := tools.ListDirHandler(session, params)
		if err != nil {
			t.Fatalf("ListDirHandler failed: %v", err)
		}
		result := res.(tools.ListDirResult)
		// file1.txt, subdir, subdir/file2.txt
		if len(result.Items) != 3 {
			t.Errorf("expected 3 items, got %d", len(result.Items))
		}
	})

	t.Run("ignore_patterns filters files", func(t *testing.T) {
		params := json.RawMessage(`{"dir_path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `", "ignore_patterns": ["*.txt"]}`)
		res, err := tools.ListDirHandler(session, params)
		if err != nil {
			t.Fatalf("ListDirHandler failed: %v", err)
		}
		result := res.(tools.ListDirResult)
		// file1.txt excluded, only subdir remains
		if len(result.Items) != 1 {
			t.Errorf("expected 1 item (subdir only), got %d", len(result.Items))
		}
		if result.Items[0].Path != "subdir" {
			t.Errorf("expected subdir, got %s", result.Items[0].Path)
		}
	})

	t.Run("ignore_patterns filters subdirs recursively", func(t *testing.T) {
		params := json.RawMessage(`{"dir_path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `", "recursive": true, "depth": 1, "ignore_patterns": ["subdir"]}`)
		res, err := tools.ListDirHandler(session, params)
		if err != nil {
			t.Fatalf("ListDirHandler failed: %v", err)
		}
		result := res.(tools.ListDirResult)
		// subdir excluded: only file1.txt
		if len(result.Items) != 1 {
			t.Errorf("expected 1 item, got %d", len(result.Items))
		}
		for _, item := range result.Items {
			if strings.Contains(item.Path, "subdir") {
				t.Errorf("subdir should be excluded, got %s", item.Path)
			}
		}
	})


	t.Run("limit truncation", func(t *testing.T) {
		// Create 10 extra files to exceed default limit of 50 - use a fresh dir with 60 files
		limitDir, _ := os.MkdirTemp("", "gt-limit-*")
		defer os.RemoveAll(limitDir)
		for i := 0; i < 60; i++ {
			name := fmt.Sprintf("file%02d.txt", i)
			os.WriteFile(filepath.Join(limitDir, name), []byte("x"), 0644)
		}
		params := json.RawMessage(`{"dir_path": "` + filepath.ToSlash(limitDir) + `"}`)
		res, err := tools.ListDirHandler(session, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		result := res.(tools.ListDirResult)
		if len(result.Items) != 50 {
			t.Errorf("expected 50 items (default limit), got %d", len(result.Items))
		}
		if !result.Truncated {
			t.Error("expected truncated=true")
		}
	})

	t.Run("custom limit", func(t *testing.T) {
		params := json.RawMessage(`{"dir_path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `", "recursive": true, "depth": 1, "limit": 2}`)
		res, err := tools.ListDirHandler(session, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		result := res.(tools.ListDirResult)
		if len(result.Items) != 2 {
			t.Errorf("expected 2 items, got %d", len(result.Items))
		}
		if !result.Truncated {
			t.Error("expected truncated=true")
		}
	})

	t.Run("default ignore dirs hidden marker", func(t *testing.T) {
		ignoreDir, _ := os.MkdirTemp("", "gt-ignore-*")
		defer os.RemoveAll(ignoreDir)
		os.WriteFile(filepath.Join(ignoreDir, "main.go"), []byte("x"), 0644)
		os.MkdirAll(filepath.Join(ignoreDir, ".venv", "lib"), 0755)
		os.WriteFile(filepath.Join(ignoreDir, ".venv", "lib", "secret.py"), []byte("x"), 0644)
		os.MkdirAll(filepath.Join(ignoreDir, "__pycache__"), 0755)
		os.WriteFile(filepath.Join(ignoreDir, "__pycache__", "mod.pyc"), []byte("x"), 0644)

		params := json.RawMessage(`{"dir_path": "` + filepath.ToSlash(ignoreDir) + `", "recursive": true, "depth": 2}`)
		res, err := tools.ListDirHandler(session, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		result := res.(tools.ListDirResult)

		hiddenPaths := map[string]bool{}
		for _, item := range result.Items {
			if item.Hidden {
				hiddenPaths[item.Path] = true
			}
		}

		// .venv and __pycache__ should appear as hidden
		if !hiddenPaths[".venv"] {
			t.Error("expected .venv to be marked hidden")
		}
		if !hiddenPaths["__pycache__"] {
			t.Error("expected __pycache__ to be marked hidden")
		}

		// Their contents must not appear
		for _, item := range result.Items {
			if strings.HasPrefix(item.Path, ".venv/") || strings.HasPrefix(item.Path, "__pycache__/") {
				t.Errorf("hidden dir contents should not appear: %s", item.Path)
			}
		}
	})
}

func TestReadFileHandler(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "testfile-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()
	content := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"
	os.WriteFile(tmpFile.Name(), []byte(content), 0644)
	defer os.Remove(tmpFile.Name())

	session := setupTestSession(os.TempDir())

	t.Run("full read", func(t *testing.T) {
		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(tmpFile.Name()) + `"}`)
		res, err := tools.ReadFileHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.ReadFileResult)
		if !strings.Contains(result.Content, "Line 1") || result.TotalLines != 5 {
			t.Errorf("unexpected content or line count: %d lines", result.TotalLines)
		}
	})

	t.Run("line slicing", func(t *testing.T) {
		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(tmpFile.Name()) + `", "start_line": 2, "end_line": 4}`)
		res, err := tools.ReadFileHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.ReadFileResult)
		expected := "   2│Line 2\n   3│Line 3\n   4│Line 4\n"
		if result.Content != expected {
			t.Errorf("expected %q, got %q", expected, result.Content)
		}
	})

	t.Run("line number prefixes", func(t *testing.T) {
		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(tmpFile.Name()) + `", "start_line": 1, "end_line": 5}`)
		res, err := tools.ReadFileHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.ReadFileResult)
		want := "   1│Line 1\n   2│Line 2\n   3│Line 3\n   4│Line 4\n   5│Line 5\n"
		if result.Content != want {
			t.Errorf("prefix format mismatch:\nwant %q\ngot  %q", want, result.Content)
		}
	})

	t.Run("binary protection", func(t *testing.T) {
		binFile := tmpFile.Name() + ".bin"
		os.WriteFile(binFile, []byte{0, 1, 2, 3, 0xFF, 0x00}, 0644)
		defer os.Remove(binFile)

		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(binFile) + `"}`)
		_, err := tools.ReadFileHandler(session, params)
		if err == nil || !strings.Contains(err.Error(), "binary") {
			t.Errorf("expected binary protection error, got %v", err)
		}
	})

	t.Run("full read truncation", func(t *testing.T) {
		// ~30KB of text (>25KB threshold): 3000 lines of 10 bytes each.
		bigFile := tmpFile.Name() + ".big.txt"
		var big bytes.Buffer
		for i := 1; i <= 3000; i++ {
			fmt.Fprintf(&big, "Line %04d\n", i)
		}
		os.WriteFile(bigFile, big.Bytes(), 0644)
		defer os.Remove(bigFile)

		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(bigFile) + `"}`)
		res, err := tools.ReadFileHandler(session, params)
		if err != nil {
			t.Fatalf("ReadFileHandler failed: %v", err)
		}
		result := res.(tools.ReadFileResult)

		if !result.Truncated {
			t.Errorf("expected truncated=true for >25KB full read")
		}
		if len(result.Content) > 2048 {
			t.Errorf("truncated content too large: %d bytes", len(result.Content))
		}
		if !strings.Contains(result.Content, "file truncated") {
			t.Errorf("expected truncation notice, got: %.200s", result.Content)
		}
		if !strings.HasPrefix(result.Content, "   1│") {
			t.Errorf("content should start with line 1 prefix, got: %.100s", result.Content)
		}
		if result.TotalLines != 3000 {
			t.Errorf("total_lines: got %d, want 3000", result.TotalLines)
		}
	})

	t.Run("range read not truncated", func(t *testing.T) {
		bigFile := tmpFile.Name() + ".big.txt"
		var big bytes.Buffer
		for i := 1; i <= 3000; i++ {
			fmt.Fprintf(&big, "Line %04d\n", i)
		}
		os.WriteFile(bigFile, big.Bytes(), 0644)
		defer os.Remove(bigFile)

		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(bigFile) + `", "start_line": 100, "end_line": 105}`)
		res, err := tools.ReadFileHandler(session, params)
		if err != nil {
			t.Fatalf("ReadFileHandler failed: %v", err)
		}
		result := res.(tools.ReadFileResult)
		if result.Truncated {
			t.Errorf("explicit range read should not be truncated")
		}
		want := " 100│Line 0100\n 101│Line 0101\n 102│Line 0102\n 103│Line 0103\n 104│Line 0104\n 105│Line 0105\n"
		if result.Content != want {
			t.Errorf("range content mismatch:\nwant %q\ngot  %q", want, result.Content)
		}
	})
}

// TestReadFile_Truncation exercises the boundary conditions and edge cases of
// the 25KB full-read truncation.
func TestReadFile_Truncation(t *testing.T) {
	session := setupTestSession(t.TempDir())

	// writeLines writes lineCount lines formatted by lineFmt and returns the path.
	writeLines := func(name, lineFmt string, lineCount int) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		var buf bytes.Buffer
		for i := 1; i <= lineCount; i++ {
			fmt.Fprintf(&buf, lineFmt, i)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		return path
	}

	// fullRead runs ReadFileHandler with no range and returns the result.
	fullRead := func(path string) tools.ReadFileResult {
		t.Helper()
		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(path) + `"}`)
		res, err := tools.ReadFileHandler(session, params)
		if err != nil {
			t.Fatalf("ReadFileHandler failed: %v", err)
		}
		return res.(tools.ReadFileResult)
	}

	t.Run("exactly at 25KB threshold is not truncated", func(t *testing.T) {
		// 2560 lines * 10 bytes = 25600 bytes = exactly 25KB.
		path := writeLines("exact.txt", "Line %04d\n", 2560)
		if got := fullRead(path); got.Truncated {
			t.Error("exactly 25KB should not be truncated (limit is >25KB)")
		}
	})

	t.Run("just over 25KB is truncated", func(t *testing.T) {
		// 2561 lines * 10 bytes = 25610 bytes, just over 25KB.
		path := writeLines("over.txt", "Line %04d\n", 2561)
		result := fullRead(path)
		if !result.Truncated {
			t.Error("file just over 25KB should be truncated")
		}
		if len(result.Content) > 2048 {
			t.Errorf("truncated content too large: %d bytes", len(result.Content))
		}
		// The notice carries the file size and the returned amount.
		if !strings.Contains(result.Content, "first 1024 of 25610 bytes shown") {
			t.Errorf("notice should state 'first 1024 of 25610 bytes shown', got: %.200s", result.Content)
		}
	})

	t.Run("huge single line is cut at the byte cap", func(t *testing.T) {
		// A 30KB line with no newlines must be cut, not returned in full.
		path := filepath.Join(t.TempDir(), "oneliner.txt")
		if err := os.WriteFile(path, bytes.Repeat([]byte("a"), 30*1024), 0644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		result := fullRead(path)
		if !result.Truncated {
			t.Fatal("30KB single line should be truncated")
		}
		if len(result.Content) > 2048 {
			t.Errorf("single-line truncated content too large: %d bytes", len(result.Content))
		}
		if !strings.HasPrefix(result.Content, "   1│") {
			t.Errorf("content should start with the line-1 prefix, got: %.80s", result.Content)
		}
		if !strings.HasSuffix(result.Content, "file truncated") && !strings.Contains(result.Content, "file truncated") {
			t.Errorf("expected truncation notice, got: %.200s", result.Content)
		}
	})

	t.Run("explicit start_line is the escape hatch, not truncated", func(t *testing.T) {
		// Passing start_line (even 1) opts out of truncation: range reads are
		// the intended way to page through a large file.
		path := writeLines("ranged.txt", "Line %04d\n", 3000)
		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(path) + `", "start_line": 1}`)
		res, err := tools.ReadFileHandler(session, params)
		if err != nil {
			t.Fatalf("ReadFileHandler failed: %v", err)
		}
		if result := res.(tools.ReadFileResult); result.Truncated {
			t.Error("explicit start_line should not be truncated")
		}
	})

	t.Run("small file full read is not truncated", func(t *testing.T) {
		path := writeLines("small.txt", "Line %d\n", 5)
		if got := fullRead(path); got.Truncated {
			t.Error("small file should not be truncated")
		}
	})
}

func TestWriteFileHandler(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gt-write-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	session := setupTestSession(tmpDir)

	t.Run("atomic write and mkdir", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "new_sub_dir", "out.txt")
		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(filePath) + `", "content": "hello atomic"}`)
		_, err := tools.WriteFileHandler(session, params)
		if err != nil {
			t.Fatalf("WriteFileHandler failed: %v", err)
		}
		data, _ := os.ReadFile(filePath)
		if string(data) != "hello atomic" {
			t.Errorf("got %q", string(data))
		}
	})

	t.Run("size_limit", func(t *testing.T) {
		largeContent := strings.Repeat("a", 101*1024) // > 100KB
		filePath := filepath.Join(tmpDir, "too_large.txt")
		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(filePath) + `", "content": "` + filepath.ToSlash(largeContent) + `"}`)
		_, err := tools.WriteFileHandler(session, params)
		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Errorf("expected size limit error, got %v", err)
		}
	})
}

func TestEditHandler(t *testing.T) {
	tmpFile, _ := os.CreateTemp("", "edit-test-*.txt")
	tmpFile.Close()
	initial := "apple\nbanana\norange\napple"
	os.WriteFile(tmpFile.Name(), []byte(initial), 0644)
	defer os.Remove(tmpFile.Name())

	session := setupTestSession(os.TempDir())
	// Also allow access to the parent of the temp file just in case of resolving differences
	session.SetPathRules([]api.PathRule{{Action: "allow", Path: "/"}}, []api.PathRule{{Action: "allow", Path: "/"}})

	t.Run("single replace", func(t *testing.T) {
		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(tmpFile.Name()) + `", "old_string": "banana", "new_string": "grape"}`)
		_, err := tools.EditHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(tmpFile.Name())
		if !strings.Contains(string(data), "grape") || strings.Contains(string(data), "banana") {
			t.Errorf("replace failed: %s", string(data))
		}
	})

	t.Run("multiple match error", func(t *testing.T) {
		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(tmpFile.Name()) + `", "old_string": "apple", "new_string": "pear"}`)
		_, err := tools.EditHandler(session, params)
		if err == nil || !strings.Contains(err.Error(), "found 2 occurrences") {
			t.Errorf("expected multiple match error, got %v", err)
		}
	})

	t.Run("allow multiple", func(t *testing.T) {
		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(tmpFile.Name()) + `", "old_string": "apple", "new_string": "pear", "allow_multiple": true}`)
		_, err := tools.EditHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(tmpFile.Name())
		if strings.Count(string(data), "pear") != 2 {
			t.Errorf("expected 2 pears, got %d", strings.Count(string(data), "pear"))
		}
	})
}

func TestSearchHandlers(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "search-test-*")
	defer os.RemoveAll(tmpDir)
	
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main() { fmt.Println(\"hello\") }"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "util.go"), []byte("package main\nfunc Util() {}"), 0644)

	session := setupTestSession("/") // Allow root for simple testing with absolute paths

	t.Run("grep_search fixed", func(t *testing.T) {
		params := json.RawMessage(`{"pattern": "fmt.Println", "path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `", "fixed_strings": true}`)
		res, err := tools.GrepSearchHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.GrepSearchResult)
		if len(result.Matches) != 1 || !strings.Contains(result.Matches[0].FilePath, "main.go") {
			t.Errorf("grep failed: %+v", result)
		}
	})

	t.Run("glob", func(t *testing.T) {
		params := json.RawMessage(`{"pattern": "*.go", "path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `"}`)
		res, err := tools.GlobHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result, ok := res.(tools.GlobResult)
		if !ok {
			t.Fatalf("expected GlobResult, got %T", res)
		}
		if len(result.Matches) != 2 {
			t.Errorf("expected 2 files, got %d", len(result.Matches))
		}
		for _, m := range result.Matches {
			if !strings.HasSuffix(m, ".go") {
				t.Errorf("expected .go file, got %s", m)
			}
		}
		// Verify sorted by mod time (file1 was created before file2)
		if len(result.Matches) == 2 {
			fi1, _ := os.Stat(result.Matches[0])
			fi2, _ := os.Stat(result.Matches[1])
			if !fi1.ModTime().After(fi2.ModTime()) && !fi1.ModTime().Equal(fi2.ModTime()) {
				t.Errorf("expected matches sorted by mod time (newest first), but %s is older than %s",
					result.Matches[0], result.Matches[1])
			}
		}
	})

	t.Run("grep_search include_pattern", func(t *testing.T) {
		// Only search *.go files — should find pattern in both files
		params := json.RawMessage(`{"pattern": "package main", "path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `", "include_pattern": "*.go"}`)
		res, err := tools.GrepSearchHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.GrepSearchResult)
		if len(result.Matches) != 2 {
			t.Errorf("expected 2 matches, got %d", len(result.Matches))
		}
	})

	t.Run("grep_search exclude_pattern", func(t *testing.T) {
		// Exclude util.go — should only match main.go
		params := json.RawMessage(`{"pattern": "package main", "path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `", "exclude_pattern": "util.go"}`)
		res, err := tools.GrepSearchHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.GrepSearchResult)
		if len(result.Matches) != 1 {
			t.Errorf("expected 1 match, got %d", len(result.Matches))
		}
		if len(result.Matches) > 0 && !strings.Contains(result.Matches[0].FilePath, "main.go") {
			t.Errorf("expected match in main.go, got %s", result.Matches[0].FilePath)
		}
	})

	t.Run("grep_search exclude_pattern glob wildcard", func(t *testing.T) {
		// Exclude all .go files — should find no matches
		params := json.RawMessage(`{"pattern": "package main", "path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `", "exclude_pattern": "*.go"}`)
		res, err := tools.GrepSearchHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.GrepSearchResult)
		if len(result.Matches) != 0 {
			t.Errorf("expected 0 matches, got %d", len(result.Matches))
		}
	})

	t.Run("grep_search include and exclude combined", func(t *testing.T) {
		// Include *.go but exclude util.go — only main.go remains
		params := json.RawMessage(`{"pattern": "func", "path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `", "include_pattern": "*.go", "exclude_pattern": "util.go"}`)
		res, err := tools.GrepSearchHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.GrepSearchResult)
		for _, m := range result.Matches {
			if strings.Contains(m.FilePath, "util.go") {
				t.Errorf("util.go should have been excluded, got match: %s", m.FilePath)
			}
		}
	})

	t.Run("grep_search regex", func(t *testing.T) {
		params := json.RawMessage(`{"pattern": "func \\w+\\(", "path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `"}`)
		res, err := tools.GrepSearchHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.GrepSearchResult)
		if len(result.Matches) == 0 {
			t.Error("expected at least one regex match")
		}
	})

	t.Run("grep_search case insensitive default", func(t *testing.T) {
		params := json.RawMessage(`{"pattern": "PACKAGE MAIN", "path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `", "fixed_strings": true}`)
		res, err := tools.GrepSearchHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.GrepSearchResult)
		if len(result.Matches) != 2 {
			t.Errorf("expected 2 case-insensitive matches, got %d", len(result.Matches))
		}
	})

	t.Run("grep_search case sensitive", func(t *testing.T) {
		params := json.RawMessage(`{"pattern": "PACKAGE MAIN", "path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `", "fixed_strings": true, "case_sensitive": true}`)
		res, err := tools.GrepSearchHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.GrepSearchResult)
		if len(result.Matches) != 0 {
			t.Errorf("expected 0 case-sensitive matches, got %d", len(result.Matches))
		}
	})

	t.Run("grep_search total_max_matches truncation", func(t *testing.T) {
		params := json.RawMessage(`{"pattern": "package main", "path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `", "total_max_matches": 1}`)
		res, err := tools.GrepSearchHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.GrepSearchResult)
		if len(result.Matches) != 1 {
			t.Errorf("expected 1 match due to limit, got %d", len(result.Matches))
		}
		if !result.Truncated {
			t.Error("expected truncated=true")
		}
	})

	t.Run("grep_search invalid regex error", func(t *testing.T) {
		params := json.RawMessage(`{"pattern": "[invalid"}`)
		_, err := tools.GrepSearchHandler(session, params)
		if err == nil || !strings.Contains(err.Error(), "invalid regex") {
			t.Errorf("expected invalid regex error, got %v", err)
		}
	})

	t.Run("grep_search path not found", func(t *testing.T) {
		nonExistent := filepath.Join(tmpDir, "does_not_exist")
		params := json.RawMessage(`{"pattern": "func", "path": "` + filepath.ToSlash(nonExistent) + `"}`)
		_, err := tools.GrepSearchHandler(session, params)
		if err == nil {
			t.Fatal("expected error for non-existent path, got nil")
		}
		if !strings.Contains(err.Error(), "path not found") {
			t.Errorf("expected 'path not found' error, got: %v", err)
		}
	})

	t.Run("glob case insensitive default", func(t *testing.T) {
		// Default is case-insensitive: "*.GO" should still match main.go, util.go
		params := json.RawMessage(`{"pattern": "*.GO", "path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `"}`)
		res, err := tools.GlobHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.GlobResult)
		if len(result.Matches) != 2 {
			t.Errorf("expected 2 case-insensitive matches, got %d", len(result.Matches))
		}
	})

	t.Run("glob case sensitive", func(t *testing.T) {
		// case_sensitive: true — "*.GO" should not match lowercase .go files
		params := json.RawMessage(`{"pattern": "*.GO", "path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `", "case_sensitive": true}`)
		res, err := tools.GlobHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.GlobResult)
		// On a case-sensitive FS (Linux), this returns 0; on macOS it may still match — just verify no crash
		_ = result
	})

	t.Run("glob empty result", func(t *testing.T) {
		params := json.RawMessage(`{"pattern": "nonexistent*.xyz", "path": "` + strings.ReplaceAll(tmpDir, "\\", "\\\\") + `"}`)
		res, err := tools.GlobHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result, ok := res.(tools.GlobResult)
		if !ok {
			t.Fatalf("expected GlobResult, got %T", res)
		}
		if len(result.Matches) != 0 {
			t.Errorf("expected 0 matches, got %d", len(result.Matches))
		}
	})

	t.Run("glob path not found", func(t *testing.T) {
		nonExistent := filepath.Join(tmpDir, "does_not_exist")
		params := json.RawMessage(`{"pattern": "*.go", "path": "` + filepath.ToSlash(nonExistent) + `"}`)
		_, err := tools.GlobHandler(session, params)
		if err == nil {
			t.Fatal("expected error for non-existent path, got nil")
		}
		if !strings.Contains(err.Error(), "path not found") {
			t.Errorf("expected 'path not found' error, got: %v", err)
		}
	})
}

// makeMiniPNG writes a small valid PNG (2x2) to path and returns its bytes.
func makeMiniPNG(t *testing.T, path string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	img.Set(1, 0, color.RGBA{0, 255, 0, 255})
	img.Set(0, 1, color.RGBA{0, 0, 255, 255})
	img.Set(1, 1, color.RGBA{255, 255, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode png: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write png: %v", err)
	}
	return buf.Bytes()
}

func TestReadImageHandler(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gt-readimage-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	imgPath := filepath.Join(tmpDir, "shot.png")
	original := makeMiniPNG(t, imgPath)

	session := setupTestSession(tmpDir)

	t.Run("returns base64 and metadata", func(t *testing.T) {
		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(imgPath) + `"}`)
		res, err := tools.ReadImageHandler(session, params)
		if err != nil {
			t.Fatalf("ReadImageHandler failed: %v", err)
		}
		result, ok := res.(tools.ReadImageResult)
		if !ok {
			t.Fatalf("expected ReadImageResult, got %T", res)
		}
		if result.Format != "png" || result.MimeType != "image/png" {
			t.Errorf("format/mime: got %q/%q", result.Format, result.MimeType)
		}
		if result.Width != 2 || result.Height != 2 {
			t.Errorf("dimensions: got %dx%d, want 2x2", result.Width, result.Height)
		}
		if result.SizeBytes != int64(len(original)) {
			t.Errorf("size_bytes: got %d, want %d", result.SizeBytes, len(original))
		}
		decoded, err := base64.StdEncoding.DecodeString(result.Base64)
		if err != nil {
			t.Fatalf("base64 decode failed: %v", err)
		}
		if !bytes.Equal(decoded, original) {
			t.Errorf("base64 content mismatch")
		}
		// Absolute path is returned
		if !filepath.IsAbs(result.FilePath) {
			t.Errorf("file_path should be absolute, got %q", result.FilePath)
		}
	})

	t.Run("rejects text file", func(t *testing.T) {
		txtPath := filepath.Join(tmpDir, "notes.txt")
		os.WriteFile(txtPath, []byte("not an image"), 0644)
		params := json.RawMessage(`{"file_path": "` + filepath.ToSlash(txtPath) + `"}`)
		_, err := tools.ReadImageHandler(session, params)
		if err == nil || !strings.Contains(err.Error(), "unsupported image format") {
			t.Errorf("expected unsupported format error, got %v", err)
		}
	})

	t.Run("rejects path outside sandbox", func(t *testing.T) {
		// Narrow the server-level workdir root to tmpDir and enable sandbox,
		// then any path outside must be rejected by ValidatePath.
		oldRoot := api.GetWorkdirRoot()
		oldEnabled := api.IsSandboxEnabled()
		defer func() {
			_ = api.SetWorkdirRoot(oldRoot)
			api.SetSandboxEnabled(oldEnabled)
		}()
		if err := api.SetWorkdirRoot(tmpDir); err != nil {
			t.Fatalf("SetWorkdirRoot failed: %v", err)
		}
		api.SetSandboxEnabled(true)

		params := json.RawMessage(`{"file_path": "/etc/hosts"}`)
		_, err := tools.ReadImageHandler(session, params)
		if err == nil {
			t.Fatal("expected sandbox error, got nil")
		}
		if !strings.Contains(err.Error(), "security error") {
			t.Errorf("expected security error, got: %v", err)
		}
	})
}
