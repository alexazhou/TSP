package tools

import (
	"gTSP/src/api"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileParams defines input for write_file according to doc/tool_spec/write_file.md
type WriteFileParams struct {
	Path     string `json:"file_path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding,omitempty"` // default utf-8
}

// WriteFileResult defines output for write_file
type WriteFileResult struct {
	Path    string `json:"file_path"`
	Written int64  `json:"written"`
}

var WriteFileSchema = api.ToolDefinition{
	Name:        "write_file",
	Description: "- Writes complete content to a specified file\n- Automatically creates missing parent directories\n- Uses atomic writing (temp file then rename) for safety\n- Enforces a size limit to prevent accidental large writes\n- Use this tool when you need to create a new file or fully overwrite an existing one",
	InputSchema: map[string]interface{}{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]interface{}{
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "The path to the file to write.",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The complete content to write. Provide the full file; do not use placeholders.",
			},
			"encoding": map[string]interface{}{
				"type":        "string",
				"description": "Optional: The file encoding to use. Defaults to utf-8.",
			},
		},
		"required":             []string{"file_path", "content"},
		"additionalProperties": false,
	},
}

const maxWriteFileSize = 100 * 1024 // 100KB limit for safety

// WriteFileHandler implements write_file with atomic writing and dir creation
func WriteFileHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	var p WriteFileParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}

	// Validate path against sandbox
	absPath, err := api.ValidatePath(p.Path)
	if err != nil {
		return nil, err
	}
	if err := session.CheckWrite(absPath); err != nil {
		return nil, err
	}

	// 1. Safety check: file size
	if len(p.Content) > maxWriteFileSize {
		return nil, fmt.Errorf("content is too large (%d bytes). Maximum allowed is %d bytes. Consider splitting or using 'edit' for partial updates", len(p.Content), maxWriteFileSize)
	}

	// 2. Ensure parent directory exists (recursive creation)
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %v", dir, err)
	}

	// 3. Atomic write: write to temp file first
	tmpPath := absPath + ".tmp"
	err = os.WriteFile(tmpPath, []byte(p.Content), 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to write temporary file: %v", err)
	}

	// Rename temp file to target path
	if err := os.Rename(tmpPath, absPath); err != nil {
		// Clean up temp file on failure
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to commit file write: %v", err)
	}

	return WriteFileResult{
		Path:    p.Path,
		Written: int64(len(p.Content)),
	}, nil
}
