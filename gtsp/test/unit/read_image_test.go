package unit_test

import (
	"gTSP/src/api"
	"gTSP/src/tools"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ───────────────────── fixtures ─────────────────────

// makePNG creates a 2x3 RGBA png fixture.
func makePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	img.Set(1, 2, color.RGBA{0, 255, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode png: %v", err)
	}
	return buf.Bytes()
}

// makeJPEG creates a 4x2 RGB jpeg fixture.
func makeJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for x := 0; x < 4; x++ {
		for y := 0; y < 2; y++ {
			img.Set(x, y, color.RGBA{uint8(x * 60), uint8(y * 100), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("failed to encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// makeGIF creates a 3x3 palette gif fixture.
func makeGIF(t *testing.T) []byte {
	t.Helper()
	palette := color.Palette{color.Black, color.White, color.RGBA{255, 0, 0, 255}}
	img := image.NewPaletted(image.Rect(0, 0, 3, 3), palette)
	for i := range img.Pix {
		img.Pix[i] = uint8(i % 3)
	}
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatalf("failed to encode gif: %v", err)
	}
	return buf.Bytes()
}

// webpFixture is a minimal valid 1x1 WebP (lossless, base64).
const webpFixtureBase64 = "UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEALmk0mk0iIiIiIgBoSygABc6zbAAA/v56QAAAAA=="

func makeWebP() []byte {
	data, err := base64.StdEncoding.DecodeString(webpFixtureBase64)
	if err != nil {
		panic(err)
	}
	return data
}

// makeBMP creates a minimal 1x1 24-bit BMP.
func makeBMP() []byte {
	buf := make([]byte, 58)
	// BITMAPFILEHEADER (14 bytes)
	copy(buf[0:2], "BM")
	putUint32LE(buf[2:6], 58)   // file size
	putUint32LE(buf[10:14], 54) // data offset
	// BITMAPINFOHEADER (40 bytes)
	putUint32LE(buf[14:18], 40) // header size
	putUint32LE(buf[18:22], 1)  // width
	putUint32LE(buf[22:26], 1)  // height
	putUint16LE(buf[26:28], 1)  // planes
	putUint16LE(buf[28:30], 24) // bit count
	putUint32LE(buf[30:34], 0)  // compression
	putUint32LE(buf[34:38], 4)  // image size
	// pixel data: B G R + padding (row aligned to 4 bytes)
	buf[54] = 0xFF // B
	buf[55] = 0x00 // G
	buf[56] = 0x00 // R
	buf[57] = 0x00 // padding
	return buf
}

func putUint32LE(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func putUint16LE(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func writeFixture(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to write fixture %s: %v", name, err)
	}
	return path
}

func callReadImage(t *testing.T, session api.Session, path string) (tools.ReadImageResult, error) {
	t.Helper()
	params, _ := json.Marshal(tools.ReadImageParams{FilePath: path})
	res, err := tools.ReadImageHandler(session, params)
	if err != nil {
		return tools.ReadImageResult{}, err
	}
	result, ok := res.(tools.ReadImageResult)
	if !ok {
		t.Fatalf("expected ReadImageResult, got %T", res)
	}
	return result, nil
}

// ───────────────────── handler success cases ─────────────────────

func TestReadImageHandler_PNG(t *testing.T) {
	tmp := t.TempDir()
	path := writeFixture(t, tmp, "test.png", makePNG(t))
	session := setupTestSession(tmp)

	result, err := callReadImage(t, session, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Format != "png" || result.MimeType != "image/png" {
		t.Errorf("format/mime: got %q/%q", result.Format, result.MimeType)
	}
	if result.Width != 2 || result.Height != 3 {
		t.Errorf("dimensions: got %dx%d, want 2x3", result.Width, result.Height)
	}
	decoded, err := base64.StdEncoding.DecodeString(result.Base64)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}
	original := makePNG(t)
	if !bytes.Equal(decoded, original) {
		t.Errorf("base64 content mismatch (len %d vs %d)", len(decoded), len(original))
	}
	if result.SizeBytes != int64(len(original)) {
		t.Errorf("size_bytes: got %d, want %d", result.SizeBytes, len(original))
	}
}

func TestReadImageHandler_JPEG(t *testing.T) {
	tmp := t.TempDir()
	path := writeFixture(t, tmp, "photo.jpg", makeJPEG(t))
	session := setupTestSession(tmp)

	result, err := callReadImage(t, session, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Format != "jpeg" || result.MimeType != "image/jpeg" {
		t.Errorf("format/mime: got %q/%q, want jpeg/image/jpeg", result.Format, result.MimeType)
	}
	// format must be jpeg even when the extension is .jpg
	if result.Width != 4 || result.Height != 2 {
		t.Errorf("dimensions: got %dx%d, want 4x2", result.Width, result.Height)
	}
}

func TestReadImageHandler_GIF(t *testing.T) {
	tmp := t.TempDir()
	path := writeFixture(t, tmp, "anim.gif", makeGIF(t))
	session := setupTestSession(tmp)

	result, err := callReadImage(t, session, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Format != "gif" || result.MimeType != "image/gif" {
		t.Errorf("format/mime: got %q/%q, want gif/image/gif", result.Format, result.MimeType)
	}
	if result.Width != 3 || result.Height != 3 {
		t.Errorf("dimensions: got %dx%d, want 3x3", result.Width, result.Height)
	}
}

func TestReadImageHandler_WebP(t *testing.T) {
	tmp := t.TempDir()
	path := writeFixture(t, tmp, "pic.webp", makeWebP())
	session := setupTestSession(tmp)

	result, err := callReadImage(t, session, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Format != "webp" || result.MimeType != "image/webp" {
		t.Errorf("format/mime: got %q/%q, want webp/image/webp", result.Format, result.MimeType)
	}
	// v1: webp width/height are 0 (no golang.org/x/image)
	if result.Width != 0 || result.Height != 0 {
		t.Errorf("webp dimensions: got %dx%d, want 0x0 in v1", result.Width, result.Height)
	}
	if result.Base64 == "" {
		t.Errorf("webp base64 should not be empty")
	}
}

func TestReadImageHandler_BMP(t *testing.T) {
	tmp := t.TempDir()
	path := writeFixture(t, tmp, "bitmap.bmp", makeBMP())
	session := setupTestSession(tmp)

	result, err := callReadImage(t, session, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Format != "bmp" || result.MimeType != "image/bmp" {
		t.Errorf("format/mime: got %q/%q, want bmp/image/bmp", result.Format, result.MimeType)
	}
	// v1: bmp width/height are 0 (no golang.org/x/image)
	if result.Width != 0 || result.Height != 0 {
		t.Errorf("bmp dimensions: got %dx%d, want 0x0 in v1", result.Width, result.Height)
	}
}

func TestReadImageHandler_MismatchedExtension(t *testing.T) {
	// PNG content with a .txt extension must still be detected as png.
	tmp := t.TempDir()
	path := writeFixture(t, tmp, "hidden.txt", makePNG(t))
	session := setupTestSession(tmp)

	result, err := callReadImage(t, session, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Format != "png" || result.MimeType != "image/png" {
		t.Errorf("format/mime: got %q/%q, want png/image/png (magic bytes detection)", result.Format, result.MimeType)
	}
}

func TestReadImageHandler_ReturnsAbsolutePath(t *testing.T) {
	tmp := t.TempDir()
	path := writeFixture(t, tmp, "a.png", makePNG(t))
	session := setupTestSession(tmp)

	result, err := callReadImage(t, session, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(result.FilePath) {
		t.Errorf("file_path should be absolute, got %q", result.FilePath)
	}
}

// ───────────────────── handler error cases ─────────────────────

func TestReadImageHandler_FileNotFound(t *testing.T) {
	tmp := t.TempDir()
	session := setupTestSession(tmp)
	_, err := callReadImage(t, session, filepath.Join(tmp, "missing.png"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if got := err.Error(); !contains(got, "file not found") {
		t.Errorf("expected 'file not found', got %q", got)
	}
}

func TestReadImageHandler_Directory(t *testing.T) {
	tmp := t.TempDir()
	session := setupTestSession(tmp)
	_, err := callReadImage(t, session, tmp)
	if err == nil {
		t.Fatal("expected error for directory")
	}
	if got := err.Error(); !contains(got, "is a directory") {
		t.Errorf("expected 'is a directory', got %q", got)
	}
}

func TestReadImageHandler_UnsupportedFormat(t *testing.T) {
	tmp := t.TempDir()
	path := writeFixture(t, tmp, "notes.txt", []byte("plain text"))
	session := setupTestSession(tmp)

	_, err := callReadImage(t, session, path)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if got := err.Error(); !contains(got, "unsupported image format") {
		t.Errorf("expected 'unsupported image format', got %q", got)
	}
}

func TestReadImageHandler_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := writeFixture(t, tmp, "empty.png", []byte{})
	session := setupTestSession(tmp)

	_, err := callReadImage(t, session, path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if got := err.Error(); !contains(got, "unsupported image format") {
		t.Errorf("expected 'unsupported image format', got %q", got)
	}
}

func TestReadImageHandler_TooLarge(t *testing.T) {
	tmp := t.TempDir()
	path := writeFixture(t, tmp, "big.png", makePNG(t))
	session := setupTestSession(tmp)

	// Temporarily lower the limit below the fixture size.
	original := tools.MaxImageSize
	defer func() { tools.MaxImageSize = original }()
	tools.MaxImageSize = 10

	_, err := callReadImage(t, session, path)
	if err == nil {
		t.Fatal("expected error for oversized image")
	}
	if got := err.Error(); !contains(got, "too large") {
		t.Errorf("expected 'too large', got %q", got)
	}
}

func TestReadImageHandler_UnlimitedSize(t *testing.T) {
	tmp := t.TempDir()
	path := writeFixture(t, tmp, "big.png", makePNG(t))
	session := setupTestSession(tmp)

	// MaxImageSize = 0 means no limit: even a fixture larger than any
	// artificial cap is accepted.
	original := tools.MaxImageSize
	defer func() { tools.MaxImageSize = original }()
	tools.SetMaxImageSize(0)

	result, err := callReadImage(t, session, path)
	if err != nil {
		t.Fatalf("unexpected error with unlimited size: %v", err)
	}
	if result.Format != "png" {
		t.Errorf("expected png, got %q", result.Format)
	}
}

func TestReadImageHandler_MissingFilePath(t *testing.T) {
	session := setupTestSession(t.TempDir())
	params, _ := json.Marshal(tools.ReadImageParams{})
	_, err := tools.ReadImageHandler(session, params)
	if err == nil {
		t.Fatal("expected error for missing file_path")
	}
}

func TestReadImageHandler_InvalidParams(t *testing.T) {
	session := setupTestSession(t.TempDir())
	_, err := tools.ReadImageHandler(session, json.RawMessage(`{"file_path": 123}`))
	if err == nil {
		t.Fatal("expected error for invalid params")
	}
}

// ───────────────────── SetMaxImageSize ─────────────────────

func TestSetMaxImageSize(t *testing.T) {
	original := tools.MaxImageSize
	defer func() { tools.MaxImageSize = original }()

	tools.SetMaxImageSize(123)
	if tools.MaxImageSize != 123 {
		t.Errorf("SetMaxImageSize(123): got %d", tools.MaxImageSize)
	}
	// negative input is ignored
	tools.SetMaxImageSize(-1)
	if tools.MaxImageSize != 123 {
		t.Errorf("negative input should be ignored, got %d", tools.MaxImageSize)
	}
	// 0 disables the limit
	tools.SetMaxImageSize(0)
	if tools.MaxImageSize != 0 {
		t.Errorf("SetMaxImageSize(0): got %d, want 0", tools.MaxImageSize)
	}
}

// ───────────────────── RedactForLog ─────────────────────

func TestReadImageResult_RedactForLog(t *testing.T) {
	full := strings.Repeat("A", 100)
	r := tools.ReadImageResult{
		FilePath:  "/abs/a.png",
		MimeType:  "image/png",
		Format:    "png",
		Width:     2,
		Height:    3,
		SizeBytes: 100,
		Base64:    full,
	}

	red, ok := r.RedactForLog().(tools.ReadImageResult)
	if !ok {
		t.Fatalf("RedactForLog returned %T, want ReadImageResult", r.RedactForLog())
	}
	if red.Base64 == full {
		t.Error("base64 should be masked")
	}
	// The masked value must keep a recognizable head of the original payload.
	if !strings.HasPrefix(full, red.Base64[:20]) {
		t.Errorf("base64 should keep a recognizable prefix, got %q", red.Base64)
	}
	if !strings.Contains(red.Base64, "100 chars") {
		t.Errorf("masked base64 should describe the true length, got %q", red.Base64)
	}
	// Metadata must be untouched.
	if red.FilePath != r.FilePath || red.MimeType != r.MimeType || red.Format != r.Format ||
		red.Width != r.Width || red.Height != r.Height || red.SizeBytes != r.SizeBytes {
		t.Errorf("metadata changed by RedactForLog: %+v", red)
	}
	// The original must not be mutated.
	if r.Base64 != full {
		t.Error("original result must not be mutated")
	}
}

func TestReadImageResult_RedactForLog_ShortKept(t *testing.T) {
	r := tools.ReadImageResult{Base64: "aGVsbG8="}
	red := r.RedactForLog().(tools.ReadImageResult)
	if red.Base64 != "aGVsbG8=" {
		t.Errorf("short base64 should be kept as-is, got %q", red.Base64)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		bytes.Index([]byte(s), []byte(substr)) >= 0)
}
