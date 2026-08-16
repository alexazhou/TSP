package tools

import (
	"gTSP/src/api"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
)

// ReadImageParams defines input for read_image according to doc/tool_spec/read_image.md
type ReadImageParams struct {
	FilePath string `json:"file_path"`
}

// ReadImageResult defines output for read_image.
// FilePath is the absolute path resolved by api.ValidatePath (unlike read_file,
// which echoes the input path). Base64 is the raw file content encoded as
// base64, suitable for building a data URL (data:image/<mime>;base64,<data>).
type ReadImageResult struct {
	FilePath  string `json:"file_path"`
	MimeType  string `json:"mime_type"`
	Format    string `json:"format"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	SizeBytes int64  `json:"size_bytes"`
	Base64    string `json:"base64"`
}

var ReadImageSchema = api.ToolDefinition{
	Name: "read_image",
	Description: "- Reads and returns an image file as base64 plus metadata (mime_type/format/width/height/size_bytes)\n" +
		"- Detects format by magic bytes (png/jpeg/gif/webp/bmp), not by file extension\n" +
		"- Width/height are returned for png/jpeg/gif; webp/bmp return 0 (no external image lib in v1)\n" +
		"- Rejects images over the max size limit (default 5MB)\n" +
		"- Use this tool when the agent needs to view or analyze an image file (e.g. a screenshot)",
	InputSchema: map[string]interface{}{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]interface{}{
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "The path to the image file to read.",
			},
		},
		"required":             []string{"file_path"},
		"additionalProperties": false,
	},
}

// MaxImageSize is the maximum allowed image file size in raw bytes.
// It is configurable via the --max-image-size flag (default 5MB).
var MaxImageSize int64 = 5 * 1024 * 1024

// SetMaxImageSize updates the max image file size limit (in bytes).
func SetMaxImageSize(size int64) {
	if size > 0 {
		MaxImageSize = size
	}
}

// detectImageFormat identifies the image format from magic bytes.
// It never trusts the file extension. Returns (format, mime, ok).
func detectImageFormat(data []byte) (string, string, bool) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "png", "image/png", true
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "jpeg", "image/jpeg", true
	case bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")):
		return "gif", "image/gif", true
	case len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "webp", "image/webp", true
	case len(data) >= 2 && data[0] == 'B' && data[1] == 'M':
		return "bmp", "image/bmp", true
	}
	return "", "", false
}

// decodeImageDimensions reads width/height for formats supported by the Go
// standard library (png/jpeg/gif). For webp/bmp it returns 0,0 since v1 does
// not depend on golang.org/x/image.
func decodeImageDimensions(r io.Reader, format string) (int, int) {
	switch format {
	case "png", "jpeg", "gif":
		if cfg, _, err := image.DecodeConfig(r); err == nil {
			return cfg.Width, cfg.Height
		}
	}
	return 0, 0
}

// ReadImageHandler implements read_image: reads an image file and returns
// base64 content plus metadata for multimodal consumption on the host side.
func ReadImageHandler(session api.Session, params json.RawMessage) (interface{}, error) {
	var p ReadImageParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}
	if p.FilePath == "" {
		return nil, fmt.Errorf("missing required parameter: \"file_path\"")
	}

	// Validate path against sandbox; resolve to absolute path.
	absPath, err := api.ValidatePath(p.FilePath)
	if err != nil {
		return nil, err
	}
	if err := session.CheckRead(absPath); err != nil {
		return nil, err
	}

	// Stat first: reject oversized files before reading any bytes.
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", p.FilePath)
		}
		return nil, fmt.Errorf("error opening file: %v", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory: %s", p.FilePath)
	}
	if info.Size() > MaxImageSize {
		return nil, fmt.Errorf("image file is too large (%d bytes, max %d bytes). Please compress the image and try again", info.Size(), MaxImageSize)
	}

	f, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %v", err)
	}
	defer f.Close()

	// Read enough bytes to detect magic bytes (12 bytes covers all supported formats).
	header := make([]byte, 12)
	n, readErr := io.ReadFull(f, header)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("error reading file header: %v", readErr)
	}
	header = header[:n]

	format, mime, ok := detectImageFormat(header)
	if !ok {
		return nil, fmt.Errorf("unsupported image format: %s (supported: png, jpeg, gif, webp, bmp)", p.FilePath)
	}

	// Decode dimensions for supported formats.
	var width, height int
	if format == "png" || format == "jpeg" || format == "gif" {
		if _, err := f.Seek(0, io.SeekStart); err == nil {
			width, height = decodeImageDimensions(f, format)
		}
	}

	// Read the full file content and base64-encode it.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("error seeking file: %v", err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %v", err)
	}

	return ReadImageResult{
		FilePath:  absPath,
		MimeType:  mime,
		Format:    format,
		Width:     width,
		Height:    height,
		SizeBytes: info.Size(),
		Base64:    base64.StdEncoding.EncodeToString(data),
	}, nil
}
