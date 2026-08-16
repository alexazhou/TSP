package unit_test

import (
	"gTSP/src/tools"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestGlobHandler_DoubleStar(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// Create files
	files := []string{
		"codex.md",
		"dir1/codex.md",
		"dir1/dir2/codex.md",
		"other.md",
		"dir1/other.md",
	}

	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		err := os.MkdirAll(filepath.Dir(path), 0755)
		if err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		err = os.WriteFile(path, []byte("test"), 0644)
		if err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
	}

	tests := []struct {
		name          string
		pattern       string
		caseSensitive bool
		expected      []string
	}{
		{
			name:          "match **/codex.md (case-insensitive)",
			pattern:       "**/codex.md",
			caseSensitive: false,
			expected: []string{
				filepath.Join(tmpDir, "codex.md"),
				filepath.Join(tmpDir, "dir1/codex.md"),
				filepath.Join(tmpDir, "dir1/dir2/codex.md"),
			},
		},
		{
			name:          "match **/codex.md (case-sensitive)",
			pattern:       "**/codex.md",
			caseSensitive: true,
			expected: []string{
				filepath.Join(tmpDir, "codex.md"),
				filepath.Join(tmpDir, "dir1/codex.md"),
				filepath.Join(tmpDir, "dir1/dir2/codex.md"),
			},
		},
	}

	session := setupTestSession(tmpDir)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := tools.GlobParams{
				Pattern:       tt.pattern,
				Path:          tmpDir,
				CaseSensitive: tt.caseSensitive,
			}
			paramBytes, _ := json.Marshal(params)

			res, err := tools.GlobHandler(session, paramBytes)
			if err != nil {
				t.Fatalf("GlobHandler error: %v", err)
			}

			globRes, ok := res.(tools.GlobResult)
			if !ok {
				t.Fatalf("expected GlobResult, got %T", res)
			}

			// Sort both slices to compare easily
			var actual []string = globRes.Matches
			sort.Strings(actual)
			sort.Strings(tt.expected)

			if len(actual) != len(tt.expected) {
				t.Errorf("expected %d matches, got %d: %v", len(tt.expected), len(actual), actual)
				return
			}

			for i := range actual {
				if actual[i] != tt.expected[i] {
					t.Errorf("at index %d: expected %s, got %s", i, tt.expected[i], actual[i])
				}
			}
		})
	}
}
