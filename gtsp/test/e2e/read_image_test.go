package e2e_test

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// buildGTSP returns the path to a gtsp binary, building it from source into a
// temp dir unless the GTSP_BIN environment variable points at one already.
func buildGTSP(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("GTSP_BIN"); bin != "" {
		if _, err := os.Stat(bin); err != nil {
			t.Fatalf("GTSP_BIN not found: %v", err)
		}
		return bin
	}
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	tmpBin := filepath.Join(t.TempDir(), "gtsp"+exeSuffix())
	cmd := exec.Command("go", "build", "-o", tmpBin, "./src")
	cmd.Dir = moduleRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return tmpBin
}

func readJSONLine(t *testing.T, r *bufio.Reader) map[string]interface{} {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read response line: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("parse response %q: %v", line, err)
	}
	return m
}

// toolNamesFromInit returns the list of tool names advertised by initialize.
func toolNamesFromInit(t *testing.T, resp map[string]interface{}) []string {
	t.Helper()
	result, _ := resp["result"].(map[string]interface{})
	caps, _ := result["capabilities"].(map[string]interface{})
	toolsRaw, _ := caps["tools"].([]interface{})
	var names []string
	for _, tr := range toolsRaw {
		if m, ok := tr.(map[string]interface{}); ok {
			if n, ok := m["name"].(string); ok {
				names = append(names, n)
			}
		}
	}
	return names
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestReadImageEndToEnd(t *testing.T) {
	bin := buildGTSP(t)
	workdir := t.TempDir()

	const width, height = 16, 12
	pngBytes := makePNG(t, width, height)
	if err := os.WriteFile(filepath.Join(workdir, "test_image.png"), pngBytes, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cmd := exec.Command(bin)
	cmd.Dir = workdir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gtsp: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	reader := bufio.NewReader(stdout)
	enc := json.NewEncoder(stdin)

	// 1. initialize
	initMsg := map[string]interface{}{
		"id":     "1",
		"method": "initialize",
		"input": map[string]interface{}{
			"protocolVersion": "0.3",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{"include": []string{"read_image"}},
			},
		},
	}
	if err := enc.Encode(initMsg); err != nil {
		t.Fatalf("write initialize: %v", err)
	}
	initResp := readJSONLine(t, reader)
	names := toolNamesFromInit(t, initResp)
	if !contains(names, "read_image") {
		t.Fatalf("read_image not advertised in initialize tools: %v", names)
	}

	// 2. read_image
	callMsg := map[string]interface{}{
		"id":     "2",
		"method": "tool",
		"tool":   "read_image",
		"input":  map[string]interface{}{"file_path": "test_image.png"},
	}
	if err := enc.Encode(callMsg); err != nil {
		t.Fatalf("write read_image call: %v", err)
	}
	resp := readJSONLine(t, reader)
	if typ, _ := resp["type"].(string); typ == "error" {
		body, _ := json.Marshal(resp)
		t.Fatalf("read_image returned error: %s", body)
	}

	result, _ := resp["result"].(map[string]interface{})
	if result == nil {
		t.Fatal("read_image response missing result")
	}
	str := func(k string) string { s, _ := result[k].(string); return s }
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
	allOK := true
	for _, c := range checks {
		if !c.ok {
			allOK = false
			t.Errorf("FAIL %s (mime=%q format=%q %dx%d size=%d base64=%d chars)", c.name, mime, format, w, h, size, len(b64))
		}
	}
	if allOK {
		t.Log("read_image e2e: all checks passed")
	}
}
