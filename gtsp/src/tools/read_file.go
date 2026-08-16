package tools

import (
	"gTSP/src/api"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

// ReadFileParams defines input for read_file according to doc/tool_spec/read_file.md
type ReadFileParams struct {
	Path      string `json:"file_path"`
	StartLine int    `json:"start_line,omitempty"` // 1-based index
	EndLine   int    `json:"end_line,omitempty"`   // 1-based index, inclusive
	Encoding  string `json:"encoding,omitempty"`   // default utf-8
}

// ReadFileResult defines output for read_file
type ReadFileResult struct {
	FilePath   string `json:"file_path"`
	Content    string `json:"content"`
	TotalLines int    `json:"total_lines"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Truncated  bool   `json:"truncated,omitempty"`
}

var ReadFileSchema = api.ToolDefinition{
	Name:        "read_file",
	Description: "- Reads and returns the content of a specified file\n- Supports line-based slicing for reading specific portions of large files\n- Full reads of files larger than 25KB are truncated to the first ~1KB of content and marked with 'truncated: true' (plus a notice); use 'start_line'/'end_line' or 'grep_search' to page through large files\n- Detects and rejects binary files for safety\n- Each content line is prefixed with its 1-based line number (e.g. \"   12│code\") for reference\n- When using 'edit', provide the exact text WITHOUT the line-number prefix\n- Use this tool when you need to read the contents of a file to understand its logic or data",
	InputSchema: map[string]interface{}{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]interface{}{
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "The path to the file to read.",
			},
			"start_line": map[string]interface{}{
				"type":        "integer",
				"description": "Optional: The 1-based line number to start reading from. Defaults to 1.",
			},
			"end_line": map[string]interface{}{
				"type":        "integer",
				"description": "Optional: The 1-based line number to end reading at (inclusive). Defaults to the end of the file or 500 lines.",
			},
			"encoding": map[string]interface{}{
				"type":        "string",
				"description": "Optional: The file encoding to use. Defaults to utf-8.",
			},
		},
		"required":             []string{"file_path"},
		"additionalProperties": false,
	},
}

const (
	// truncateThresholdBytes: a full read (no start_line/end_line) of a file
	// larger than this is truncated to the first truncatedReturnBytes.
	truncateThresholdBytes = 25 * 1024 // 25KB
	truncatedReturnBytes   = 1024      // 1KB
	maxLinesToReturn       = 500       // Safety limit for a single call
)

// appendNumberedLine writes "N│<line>\n" to buf. When capBytes > 0 the line is
// cut so buf never exceeds capBytes; ok reports whether more lines should be
// collected (false when the cap was reached).
func appendNumberedLine(buf *bytes.Buffer, lineNum int, line []byte, capBytes int) (ok bool) {
	prefix := fmt.Sprintf("%4d│", lineNum)
	if capBytes <= 0 {
		buf.WriteString(prefix)
		buf.Write(line)
		buf.WriteByte('\n')
		return true
	}
	room := capBytes - buf.Len() - 1 // reserve the trailing '\n'
	if room <= 0 {
		return false
	}
	avail := room - len(prefix)
	if avail <= 0 {
		return false
	}
	if avail < len(line) {
		line = line[:avail]
	}
	buf.WriteString(prefix)
	buf.Write(line)
	buf.WriteByte('\n')
	return buf.Len() < capBytes
}

// ReadFileHandler implements read_file with line-based slicing and safety protections
func ReadFileHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	var p ReadFileParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}

	// Validate path against sandbox
	absPath, err := api.ValidatePath(p.Path)
	if err != nil {
		return nil, err
	}
	if err := session.CheckRead(absPath); err != nil {
		return nil, err
	}

	f, err := os.Open(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", p.Path)
		}
		return nil, fmt.Errorf("error opening file: %v", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("could not stat file: %v", err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory: %s", p.Path)
	}

	// 1. Binary protection: check first 512 bytes for null bytes or invalid UTF-8
	header := make([]byte, 512)
	n, _ := f.Read(header)
	if isBinary(header[:n]) {
		return nil, fmt.Errorf("file appears to be binary and cannot be read as text")
	}
	// Reset file pointer after binary check
	_, _ = f.Seek(0, io.SeekStart)

	// 2. Full-read truncation: a full read (no explicit range) of a file larger
	// than truncateThresholdBytes returns only the first truncatedReturnBytes.
	// Explicit line ranges are the intended way to page through large files and
	// are not truncated.
	truncated := false
	if p.StartLine <= 0 && p.EndLine <= 0 && info.Size() > truncateThresholdBytes {
		truncated = true
	}

	// 3. Line-based slicing
	var content bytes.Buffer
	scanner := bufio.NewScanner(f)
	currentLine := 0
	totalLines := 0
	contentEndLine := 0 // last line actually written to content
	collecting := true
	byteCap := 0
	if truncated {
		byteCap = truncatedReturnBytes
	}

	startLine := p.StartLine
	if startLine <= 0 {
		startLine = 1
	}
	endLine := p.EndLine

	// Optimization: if we have a huge endLine, cap it for safety
	if endLine > 0 && endLine-startLine > maxLinesToReturn {
		endLine = startLine + maxLinesToReturn - 1
	}

	for scanner.Scan() {
		currentLine++
		totalLines++

		// Collect lines within range
		if currentLine >= startLine && (endLine <= 0 || currentLine <= endLine) {
			// If we hit a hard line count limit without an explicit endLine
			if endLine <= 0 && (currentLine-startLine) >= maxLinesToReturn {
				continue // Keep counting total lines but stop collecting
			}
			if !collecting {
				continue // already filled the truncation cap; keep counting
			}
			// Prefix each line with its 1-based file line number so the model
			// can reference specific lines (e.g. when editing). The prefix uses
			// the actual file line number, not the slice-relative index.
			contentEndLine = currentLine
			collecting = appendNumberedLine(&content, currentLine, scanner.Bytes(), byteCap)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning file: %v", err)
	}

	actualEnd := currentLine
	if endLine > 0 && endLine < actualEnd {
		actualEnd = endLine
	}
	if truncated {
		actualEnd = contentEndLine
	}
	if startLine > totalLines && totalLines > 0 {
		return nil, fmt.Errorf("start_line (%d) is beyond total lines (%d)", startLine, totalLines)
	}

	if truncated {
		fmt.Fprintf(&content, "... (file truncated: first %d of %d bytes shown; use 'grep_search' or specify 'start_line'/'end_line')\n",
			truncatedReturnBytes, info.Size())
	}

	return ReadFileResult{
		FilePath:   p.Path,
		Content:    content.String(),
		TotalLines: totalLines,
		StartLine:  startLine,
		EndLine:    actualEnd,
		Truncated:  truncated,
	}, nil
}

// isBinary checks if a byte slice contains null bytes or many non-UTF8 sequences
func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	// Check for null bytes
	if bytes.IndexByte(data, 0) != -1 {
		return true
	}
	// Check UTF-8 validity
	if !utf8.Valid(data) {
		// Some invalid UTF-8 is fine for text files (e.g. mixed encodings), 
		// but if a large portion is invalid, it's likely binary.
		invalidCount := 0
		for len(data) > 0 {
			r, size := utf8.DecodeRune(data)
			if r == utf8.RuneError && size == 1 {
				invalidCount++
			}
			data = data[size:]
		}
		if invalidCount > 10 { // Heuristic
			return true
		}
	}
	return false
}
