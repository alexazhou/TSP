package e2e_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// makePNG generates a small WxH RGB png used as the fixture for the e2e flow.
func makePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	img.Set(width-1, height-1, color.RGBA{0, 0, 255, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func TestReadImageEndToEnd(t *testing.T) {
	s := startGTSP(t, withInclude("read_image"))
	names := toolNamesFromInit(t, s.initResp)
	if !containsStr(names, "read_image") {
		t.Fatalf("read_image not advertised in initialize tools: %v", names)
	}

	const width, height = 16, 12
	pngBytes := makePNG(t, width, height)
	if err := os.WriteFile(filepath.Join(s.workdir, "test_image.png"), pngBytes, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	result := s.callTool("read_image", map[string]any{"file_path": "test_image.png"})
	str := func(k string) string { v, _ := result[k].(string); return v }
	num := func(k string) int { f, _ := result[k].(float64); return int(f) }

	mime, format, b64 := str("mime_type"), str("format"), str("base64")
	fp := str("file_path")
	size, w, h := num("size_bytes"), num("width"), num("height")

	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}

	checks := []struct {
		name string
		ok   bool
	}{
		{"mime_type == image/png", mime == "image/png"},
		{"format == png", format == "png"},
		{"width == 16", w == width},
		{"height == 12", h == height},
		{"size_bytes == fixture", size == len(pngBytes)},
		{"base64 roundtrip", bytes.Equal(decoded, pngBytes)},
		{"file_path absolute", filepath.IsAbs(fp)},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("FAIL %s (mime=%q format=%q %dx%d size=%d base64=%d chars)", c.name, mime, format, w, h, size, len(b64))
		}
	}
}
