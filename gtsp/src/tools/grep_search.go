package tools

import (
	"gTSP/src/api"
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GrepSearchParams defines input for grep_search according to doc/tool_spec/grep_search.md
type GrepSearchParams struct {
	Pattern           string `json:"pattern"`
	DirPath           string `json:"dir_path,omitempty"`
	IncludePattern    string `json:"include_pattern,omitempty"`
	ExcludePattern    string `json:"exclude_pattern,omitempty"`
	FixedStrings      bool   `json:"fixed_strings,omitempty"`
	CaseSensitive     bool   `json:"case_sensitive,omitempty"`
	Context           int    `json:"context,omitempty"`
	TotalMaxMatches   int    `json:"total_max_matches,omitempty"`
	MaxMatchesPerFile int    `json:"max_matches_per_file,omitempty"`
}

// MatchInfo defines a single search result
type MatchInfo struct {
	FilePath   string   `json:"file_path"`
	LineNumber int      `json:"line_number"`
	Content    string   `json:"content"`
	Context    []string `json:"context,omitempty"`
}

// GrepSearchResult defines output for grep_search
type GrepSearchResult struct {
	Matches   []MatchInfo `json:"matches"`
	Truncated bool        `json:"truncated"`
}

var GrepSearchSchema = api.ToolDefinition{
	Name:        "grep_search",
	Description: "- High-performance code searching tool\n- Supports both literal strings and regular expressions\n- Provides context lines around matches to help understand code logic\n- Includes built-in safety limits for total matches and matches per file\n- Use this tool when you need to find specific strings, patterns, or definitions across multiple files without reading them one by one",
	InputSchema: map[string]interface{}{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "The regex or literal pattern to search for.",
			},
			"dir_path": map[string]interface{}{
				"type":        "string",
				"description": "Optional: Subdirectory to search within. Defaults to the current directory.",
			},
			"include_pattern": map[string]interface{}{
				"type":        "string",
				"description": "Optional: Glob pattern to filter files (e.g., '*.go').",
			},
			"exclude_pattern": map[string]interface{}{
				"type":        "string",
				"description": "Optional: Glob pattern to exclude files or paths.",
			},
			"fixed_strings": map[string]interface{}{
				"type":        "boolean",
				"description": "Optional: If true, treat the pattern as a literal string. Defaults to false (regex).",
			},
			"case_sensitive": map[string]interface{}{
				"type":        "boolean",
				"description": "Optional: Whether the search should be case-sensitive. Defaults to false.",
			},
			"context": map[string]interface{}{
				"type":        "integer",
				"description": "Optional: Number of context lines to return around each match.",
			},
			"total_max_matches": map[string]interface{}{
				"type":        "integer",
				"description": "Optional: Maximum total matches to return across all files. Defaults to 100.",
			},
			"max_matches_per_file": map[string]interface{}{
				"type":        "integer",
				"description": "Optional: Maximum matches to return per individual file. Defaults to 10.",
			},
		},
		"required":             []string{"pattern"},
		"additionalProperties": false,
	},
}

// GrepSearchHandler implements code searching with safety limits
func GrepSearchHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	var p GrepSearchParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}

	totalMax := p.TotalMaxMatches
	if totalMax <= 0 {
		totalMax = 100 // Default as per spec
	}
	perFileMax := p.MaxMatchesPerFile
	if perFileMax <= 0 {
		perFileMax = 10 // Default as per spec
	}

	searchDir := p.DirPath
	if searchDir == "" {
		searchDir = "."
	}

	// Validate path against sandbox
	absSearchDir, vErr := api.ValidatePath(searchDir)
	if vErr != nil {
		return nil, vErr
	}
	if vErr = session.CheckRead(absSearchDir); vErr != nil {
		return nil, vErr
	}
	searchDir = absSearchDir

	var re *regexp.Regexp
	var err error
	if !p.FixedStrings {
		pattern := p.Pattern
		if !p.CaseSensitive {
			pattern = "(?i)" + pattern
		}
		re, err = regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %v", err)
		}
	}

	result := GrepSearchResult{Matches: []MatchInfo{}}
	count := 0

	err = filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			// Skip directories that match ignore lists
			if info != nil && (info.Name() == ".git" || info.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}

		// Basic file filtering
		if p.IncludePattern != "" {
			if matched, _ := filepath.Match(p.IncludePattern, info.Name()); !matched {
				return nil
			}
		}

		if count >= totalMax {
			result.Truncated = true
			return filepath.SkipAll
		}

		matches, fileErr := searchInFile(path, p, re, perFileMax)
		if fileErr == nil {
			for _, m := range matches {
				result.Matches = append(result.Matches, m)
				count++
				if count >= totalMax {
					result.Truncated = true
					return filepath.SkipAll
				}
			}
		}

		return nil
	})

	return result, err
}

func searchInFile(path string, p GrepSearchParams, re *regexp.Regexp, limit int) ([]MatchInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matches []MatchInfo
	scanner := bufio.NewScanner(f)
	lineNum := 0
	fileMatches := 0

	for scanner.Scan() {
		lineNum++
		content := scanner.Text()
		matched := false

		if p.FixedStrings {
			if p.CaseSensitive {
				matched = strings.Contains(content, p.Pattern)
			} else {
				matched = strings.Contains(strings.ToLower(content), strings.ToLower(p.Pattern))
			}
		} else {
			matched = re.MatchString(content)
		}

		if matched {
			matches = append(matches, MatchInfo{
				FilePath:   path,
				LineNumber: lineNum,
				Content:    content,
			})
			fileMatches++
			if fileMatches >= limit {
				break
			}
		}
	}

	return matches, scanner.Err()
}
