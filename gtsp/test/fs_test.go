package handlers_test

import (
	"gTSP/src/api"
	"gTSP/src/tools"
	"encoding/json"
	"fmt"
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
		params := json.RawMessage(`{"dir_path": "` + tmpDir + `"}`)
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
		params := json.RawMessage(`{"dir_path": "` + tmpDir + `", "recursive": true, "depth": 1}`)
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

	t.Run("limit truncation", func(t *testing.T) {
		// Create 10 extra files to exceed default limit of 50 - use a fresh dir with 60 files
		limitDir, _ := os.MkdirTemp("", "gt-limit-*")
		defer os.RemoveAll(limitDir)
		for i := 0; i < 60; i++ {
			name := fmt.Sprintf("file%02d.txt", i)
			os.WriteFile(filepath.Join(limitDir, name), []byte("x"), 0644)
		}
		params := json.RawMessage(`{"dir_path": "` + limitDir + `"}`)
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
		params := json.RawMessage(`{"dir_path": "` + tmpDir + `", "recursive": true, "depth": 1, "limit": 2}`)
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

		params := json.RawMessage(`{"dir_path": "` + ignoreDir + `", "recursive": true, "depth": 2}`)
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
	content := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"
	os.WriteFile(tmpFile.Name(), []byte(content), 0644)
	defer os.Remove(tmpFile.Name())

	session := setupTestSession(os.TempDir())

	t.Run("full read", func(t *testing.T) {
		params := json.RawMessage(`{"file_path": "` + tmpFile.Name() + `"}`)
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
		params := json.RawMessage(`{"file_path": "` + tmpFile.Name() + `", "start_line": 2, "end_line": 4}`)
		res, err := tools.ReadFileHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.(tools.ReadFileResult)
		expected := "Line 2\nLine 3\nLine 4\n"
		if result.Content != expected {
			t.Errorf("expected %q, got %q", expected, result.Content)
		}
	})

	t.Run("binary protection", func(t *testing.T) {
		binFile := tmpFile.Name() + ".bin"
		os.WriteFile(binFile, []byte{0, 1, 2, 3, 0xFF, 0x00}, 0644)
		defer os.Remove(binFile)

		params := json.RawMessage(`{"file_path": "` + binFile + `"}`)
		_, err := tools.ReadFileHandler(session, params)
		if err == nil || !strings.Contains(err.Error(), "binary") {
			t.Errorf("expected binary protection error, got %v", err)
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
		params := json.RawMessage(`{"file_path": "` + filePath + `", "content": "hello atomic"}`)
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
		params := json.RawMessage(`{"file_path": "` + filePath + `", "content": "` + largeContent + `"}`)
		_, err := tools.WriteFileHandler(session, params)
		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Errorf("expected size limit error, got %v", err)
		}
	})
}

func TestEditHandler(t *testing.T) {
	tmpFile, _ := os.CreateTemp("", "edit-test-*.txt")
	initial := "apple\nbanana\norange\napple"
	os.WriteFile(tmpFile.Name(), []byte(initial), 0644)
	defer os.Remove(tmpFile.Name())

	session := setupTestSession(os.TempDir())
	// Also allow access to the parent of the temp file just in case of resolving differences
	session.SetPathRules([]api.PathRule{{Action: "allow", Path: "/"}}, []api.PathRule{{Action: "allow", Path: "/"}})

	t.Run("single replace", func(t *testing.T) {
		params := json.RawMessage(`{"file_path": "` + tmpFile.Name() + `", "old_string": "banana", "new_string": "grape"}`)
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
		params := json.RawMessage(`{"file_path": "` + tmpFile.Name() + `", "old_string": "apple", "new_string": "pear"}`)
		_, err := tools.EditHandler(session, params)
		if err == nil || !strings.Contains(err.Error(), "found 2 occurrences") {
			t.Errorf("expected multiple match error, got %v", err)
		}
	})

	t.Run("allow multiple", func(t *testing.T) {
		params := json.RawMessage(`{"file_path": "` + tmpFile.Name() + `", "old_string": "apple", "new_string": "pear", "allow_multiple": true}`)
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
		params := json.RawMessage(`{"pattern": "fmt.Println", "dir_path": "` + tmpDir + `", "fixed_strings": true}`)
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
		params := json.RawMessage(`{"pattern": "*.go", "path": "` + tmpDir + `"}`)
		res, err := tools.GlobHandler(session, params)
		if err != nil {
			t.Fatal(err)
		}
		result := res.([]string)
		if len(result) != 2 {
			t.Errorf("expected 2 files, got %d", len(result))
		}
	})
}
