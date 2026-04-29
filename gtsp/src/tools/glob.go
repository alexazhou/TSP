package tools

import (
	"gTSP/src/api"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// GlobParams defines input for glob
type GlobParams struct {
	Pattern       string `json:"pattern"`
	Path          string `json:"path,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
}

var GlobSchema = api.ToolDefinition{
	Name:        "glob",
	Description: "- Fast file pattern matching tool that works with any codebase size\n- Supports glob patterns like \"**/*.js\" or \"src/**/*.ts\"\n- Returns matching file paths sorted by modification time\n- Use this tool when you need to find files by name patterns\n- When you are doing an open ended search that may require multiple rounds of globbing and grepping, use the Agent tool instead\n- You can call multiple tools in a single response. It is always better to speculatively perform multiple searches in parallel if they are potentially useful.",
	InputSchema: map[string]interface{}{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "The glob pattern to match against (e.g., 'src/**/*.go').",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The directory to search in. If not specified, the current working directory will be used. IMPORTANT: Omit this field to use the default directory. DO NOT enter \"undefined\" or \"null\" - simply omit it for the default behavior. Must be a valid directory path if provided.",
			},
			"case_sensitive": map[string]interface{}{
				"type":        "boolean",
				"description": "Optional: Whether the glob search should be case-sensitive. Defaults to false.",
			},
		},
		"required":             []string{"pattern"},
		"additionalProperties": false,
	},
}


// GlobHandler implements path matching
func GlobHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	var p GlobParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}

	searchDir := p.Path
	if searchDir == "" {
		searchDir = "."
	}

	// Validate path against sandbox
	absSearchDir, err := api.ValidatePath(searchDir)
	if err != nil {
		return nil, err
	}
	if err := session.CheckRead(absSearchDir); err != nil {
		return nil, err
	}

	searchPattern := filepath.Join(absSearchDir, p.Pattern)

	matches, err := filepath.Glob(searchPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern: %v", err)
	}

	// Filter empty results
	if matches == nil {
		return []string{}, nil
	}

	return matches, nil
}
