package tools

import (
	"gTSP/src/api"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// EditParams defines input for edit according to doc/tool_spec/edit.md
type EditParams struct {
	FilePath      string `json:"file_path"`
	OldString     string `json:"old_string"`
	NewString     string `json:"new_string"`
	AllowMultiple bool   `json:"allow_multiple,omitempty"`
	Instruction   string `json:"instruction,omitempty"`
}

// EditResult defines output for edit
type EditResult struct {
	FilePath string `json:"file_path"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

var EditSchema = api.ToolDefinition{
	Name:        "edit",
	Description: "- Performs precision string replacement in a file\n- Requires an exact, unique match for the 'old_string' unless 'allow_multiple' is set\n- Preserves indentation and line endings\n- Use this tool when you need to make surgical changes to specific parts of a file without rewriting it entirely",
	InputSchema: map[string]interface{}{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]interface{}{
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "The path to the file to modify.",
			},
			"old_string": map[string]interface{}{
				"type":        "string",
				"description": "The exact literal text to replace. Must match exactly including whitespace.",
			},
			"new_string": map[string]interface{}{
				"type":        "string",
				"description": "The exact literal text to replace 'old_string' with.",
			},
			"allow_multiple": map[string]interface{}{
				"type":        "boolean",
				"description": "Optional: If true, replace all occurrences of 'old_string'. Defaults to false.",
			},
			"instruction": map[string]interface{}{
				"type":        "string",
				"description": "Optional: A brief description of the change.",
			},
		},
		"required":             []string{"file_path", "old_string", "new_string"},
		"additionalProperties": false,
	},
}

// EditHandler implements precision editing via string replacement
func EditHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	var p EditParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}

	// Validate path against sandbox
	absPath, err := api.ValidatePath(p.FilePath)
	if err != nil {
		return nil, err
	}
	if err := session.CheckWrite(absPath); err != nil {
		return nil, err
	}

	if p.OldString == p.NewString {
		return nil, fmt.Errorf("no changes to apply: old_string and new_string are identical")
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %v", p.FilePath, err)
	}

	fileContent := string(content)
	count := strings.Count(fileContent, p.OldString)

	if count == 0 {
		return nil, fmt.Errorf("could not find old_string in file. Ensure exact match including whitespace")
	}

	if count > 1 && !p.AllowMultiple {
		return nil, fmt.Errorf("found %d occurrences of old_string. Please provide more context or set 'allow_multiple' to true", count)
	}

	// Perform replacement
	var newContent string
	if p.AllowMultiple {
		newContent = strings.ReplaceAll(fileContent, p.OldString, p.NewString)
	} else {
		newContent = strings.Replace(fileContent, p.OldString, p.NewString, 1)
	}

	// Atomic write back
	tmpPath := absPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write temp file for editing: %v", err)
	}

	if err := os.Rename(tmpPath, absPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to commit edits: %v", err)
	}

	return EditResult{
		FilePath: p.FilePath,
		Status:   "success",
		Message:  fmt.Sprintf("Successfully replaced %d occurrence(s)", count),
	}, nil
}
