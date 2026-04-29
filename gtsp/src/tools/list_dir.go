package tools

import (
	"gTSP/src/api"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const defaultListDirLimit = 50

// ListDirParams defines input for list_dir according to doc/tool_spec/list_dir.md
type ListDirParams struct {
	Path           string   `json:"dir_path"`
	Recursive      bool     `json:"recursive,omitempty"`
	Depth          int      `json:"depth,omitempty"`           // 0 means current dir only
	Limit          int      `json:"limit,omitempty"`           // max items to return, default 50
	IgnorePatterns []string `json:"ignore_patterns,omitempty"` // Custom glob patterns to ignore
}

// FileInfo defines info for a single file/dir
type FileInfo struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
	Type    string `json:"type"`   // "file", "dir", "symlink"
	Hidden  bool   `json:"hidden,omitempty"` // true if contents are hidden by default ignore rules
}

// ListDirResult defines output for list_dir
type ListDirResult struct {
	Path     string     `json:"dir_path"`
	Items    []FileInfo `json:"items"`
	Truncated bool      `json:"truncated,omitempty"` // true if results were limited
}

var defaultIgnoreDirs = map[string]bool{
	".git":        true,
	".DS_Store":   true,
	".mypy_cache": true,
	"__pycache__": true,
	".venv":       true,
	"venv":        true,
	"node_modules": true,
	".next":       true,
	"dist":        true,
	"build":       true,
	".cache":      true,
}

var ListDirSchema = api.ToolDefinition{
	Name:        "list_dir",
	Description: "- Efficiently explores file system structure\n- Supports recursive listing with depth control\n- Default ignores common build/cache directories: .git, .venv, node_modules, __pycache__, .mypy_cache, dist, build, etc.\n- Pass ignore_patterns to add extra exclusions, or leave empty to use defaults only\n- Returns metadata like size, modification time, and type\n- Results are limited to 50 items by default; set limit to get more. If truncated=true, narrow your query\n- Use this tool when you need to understand the directory structure or find files in a specific path",
	InputSchema: map[string]interface{}{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]interface{}{
			"dir_path": map[string]interface{}{
				"type":        "string",
				"description": "The path to the directory to list.",
			},
			"recursive": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether to list subdirectories recursively. Defaults to false.",
			},
			"depth": map[string]interface{}{
				"type":        "integer",
				"description": "The maximum recursion depth. 0 means current directory only. Defaults to 0.",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of items to return. Defaults to 50. Increase if results are truncated.",
			},
			"ignore_patterns": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "Optional: Custom glob patterns to ignore (e.g., ['*.tmp', 'node_modules']).",
			},
		},
		"required":             []string{"dir_path"},
		"additionalProperties": false,
	},
}

// ListDirHandler implements list_dir with recursion and metadata
func ListDirHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	var p ListDirParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}

	// Validate path against sandbox
	absPath, vErr := api.ValidatePath(p.Path)
	if vErr != nil {
		return nil, vErr
	}
	if vErr = session.CheckRead(absPath); vErr != nil {
		return nil, vErr
	}
	p.Path = absPath

	result := ListDirResult{
		Path:  p.Path,
		Items: []FileInfo{},
	}

	maxDepth := p.Depth
	if p.Recursive && maxDepth <= 0 {
		maxDepth = 1 // Default recursive depth
	}

	limit := p.Limit
	if limit <= 0 {
		limit = defaultListDirLimit
	}

	err := walkDir(p.Path, p.Path, 0, maxDepth, limit, &result.Items)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("directory not found: %s", p.Path)
		}
		return nil, fmt.Errorf("error accessing directory: %v", err)
	}

	if len(result.Items) == limit {
		result.Truncated = true
	}

	return result, nil
}

func walkDir(root, current string, currentDepth, maxDepth, limit int, items *[]FileInfo) error {
	if len(*items) >= limit {
		return nil
	}

	entries, err := os.ReadDir(current)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if len(*items) >= limit {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileType := "file"
		if entry.IsDir() {
			fileType = "dir"
		} else if info.Mode()&os.ModeSymlink != 0 {
			fileType = "symlink"
		}

		relPath, _ := filepath.Rel(root, filepath.Join(current, entry.Name()))

		if defaultIgnoreDirs[entry.Name()] {
			*items = append(*items, FileInfo{
				Path:   relPath,
				Type:   fileType,
				Hidden: true,
			})
			continue
		}

		*items = append(*items, FileInfo{
			Path:    relPath,
			Size:    info.Size(),
			ModTime: info.ModTime().Format(time.RFC3339),
			Type:    fileType,
		})

		if entry.IsDir() && currentDepth < maxDepth {
			_ = walkDir(root, filepath.Join(current, entry.Name()), currentDepth+1, maxDepth, limit, items)
		}
	}
	return nil
}
