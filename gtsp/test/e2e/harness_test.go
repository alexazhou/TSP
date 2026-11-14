package e2e_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const defaultCallTimeout = 10 * time.Second

var (
	gtspBinPath       string
	gtspBuildTmpDir   string
	defaultBusyboxEnv string
)

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func TestMain(m *testing.M) {
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve module root: %v\n", err)
		os.Exit(1)
	}

	if runtime.GOOS == "windows" && os.Getenv("GTSP_BUSYBOX") == "" {
		candidate := filepath.Join(moduleRoot, "third_party", "busybox", "busybox64u.exe")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			defaultBusyboxEnv = candidate
		}
	}

	if bin := os.Getenv("GTSP_BIN"); bin != "" {
		if _, err := os.Stat(bin); err != nil {
			fmt.Fprintf(os.Stderr, "GTSP_BIN=%q not found: %v\n", bin, err)
			os.Exit(1)
		}
		gtspBinPath = bin
	} else {
		tmpDir, err := os.MkdirTemp("", "gtsp-e2e-bin-*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "mkdtemp: %v\n", err)
			os.Exit(1)
		}
		gtspBuildTmpDir = tmpDir
		binPath := filepath.Join(tmpDir, "gtsp"+exeSuffix())
		cmd := exec.Command("go", "build", "-o", binPath, "./src")
		cmd.Dir = moduleRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "go build failed: %v\n%s\n", err, out)
			_ = os.RemoveAll(tmpDir)
			os.Exit(1)
		}
		gtspBinPath = binPath
	}

	code := m.Run()
	if gtspBuildTmpDir != "" {
		_ = os.RemoveAll(gtspBuildTmpDir)
	}
	os.Exit(code)
}

type sessionConfig struct {
	workdir    string
	logDir     string
	extraArgs  []string
	include    []string
	exclude    []string
	noAutoInit bool
}

type sessionOption func(*sessionConfig)

func withWorkdir(dir string) sessionOption {
	return func(c *sessionConfig) { c.workdir = dir }
}

func withExtraArgs(args ...string) sessionOption {
	return func(c *sessionConfig) { c.extraArgs = append(c.extraArgs, args...) }
}

func withInclude(tools ...string) sessionOption {
	return func(c *sessionConfig) { c.include = append(c.include, tools...) }
}

func withExclude(tools ...string) sessionOption {
	return func(c *sessionConfig) { c.exclude = append(c.exclude, tools...) }
}

func withNoAutoInit() sessionOption {
	return func(c *sessionConfig) { c.noAutoInit = true }
}

func buildProcessEnv() []string {
	env := os.Environ()
	if defaultBusyboxEnv != "" {
		env = append(env, "GTSP_BUSYBOX="+defaultBusyboxEnv)
	}
	return env
}

// TSPSession manages a real gtsp subprocess over stdio line-delimited JSON.
type TSPSession struct {
	t       *testing.T
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	lines   chan lineReadResult
	waitCh  chan error
	workdir string
	logDir  string

	mu          sync.Mutex
	stdinClosed bool
	exited      bool
	exitErr     error
	nextReqID   int
	initResp    map[string]any
}

type lineReadResult struct {
	line string
	err  error
}

func startGTSP(t *testing.T, opts ...sessionOption) *TSPSession {
	t.Helper()
	resetPIDIdentity()
	cfg := &sessionConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.workdir == "" {
		cfg.workdir = t.TempDir()
	}
	if cfg.logDir == "" {
		cfg.logDir = t.TempDir()
	}

	args := append([]string{"--log-path", cfg.logDir}, cfg.extraArgs...)
	cmd := exec.Command(gtspBinPath, args...)
	cmd.Dir = cfg.workdir
	cmd.Env = buildProcessEnv()
	setHarnessProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start gtsp: %v", err)
	}

	s := &TSPSession{
		t:       t,
		cmd:     cmd,
		stdin:   stdin,
		lines:   make(chan lineReadResult, 32),
		waitCh:  make(chan error, 1),
		workdir: cfg.workdir,
		logDir:  cfg.logDir,
	}

	go func() {
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadString('\n')
			s.lines <- lineReadResult{line: line, err: err}
			if err != nil {
				close(s.lines)
				return
			}
		}
	}()

	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		s.exited = true
		s.exitErr = err
		s.mu.Unlock()
		s.waitCh <- err
		close(s.waitCh)
	}()

	t.Cleanup(func() {
		s.closeStdin()
		if err := s.waitExit(5 * time.Second); err != nil {
			killHarnessProcessTree(s.cmd)
			_ = s.waitExit(3 * time.Second)
		}
	})

	if !cfg.noAutoInit {
		initInput := map[string]any{
			"protocolVersion": "0.3",
		}
		if len(cfg.include) > 0 || len(cfg.exclude) > 0 {
			toolsCap := map[string]any{}
			if len(cfg.include) > 0 {
				toolsCap["include"] = cfg.include
			}
			if len(cfg.exclude) > 0 {
				toolsCap["exclude"] = cfg.exclude
			}
			initInput["capabilities"] = map[string]any{"tools": toolsCap}
		}
		resp, err := s.callMethod("initialize", initInput)
		if err != nil {
			t.Fatalf("initialize failed: %v", err)
		}
		if typ, _ := resp["type"].(string); typ == "error" {
			t.Fatalf("initialize returned error: %v", resp)
		}
		s.initResp = resp
	}

	return s
}

func (s *TSPSession) nextID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextReqID++
	return strconv.Itoa(s.nextReqID)
}

func (s *TSPSession) callMethod(method string, input any, timeout ...time.Duration) (map[string]any, error) {
	req := map[string]any{
		"id":     s.nextID(),
		"method": method,
		"input":  input,
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return s.callRawLine(raw, timeout...)
}

func (s *TSPSession) callToolRaw(name string, params any, timeout ...time.Duration) (map[string]any, error) {
	if params == nil {
		params = map[string]any{}
	}
	req := map[string]any{
		"id":     s.nextID(),
		"method": "tool",
		"tool":   name,
		"input":  params,
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return s.callRawLine(raw, timeout...)
}

func (s *TSPSession) callTool(name string, params any, timeout ...time.Duration) map[string]any {
	s.t.Helper()
	resp, err := s.callToolRaw(name, params, timeout...)
	if err != nil {
		s.t.Fatalf("callTool(%s) transport error: %v", name, err)
	}
	if typ, _ := resp["type"].(string); typ == "error" {
		body, _ := json.Marshal(resp)
		s.t.Fatalf("callTool(%s) returned error: %s", name, body)
	}
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		s.t.Fatalf("callTool(%s) missing result field: %v", name, resp)
	}
	return result
}

func (s *TSPSession) callRawLine(raw []byte, timeout ...time.Duration) (map[string]any, error) {
	to := defaultCallTimeout
	if len(timeout) > 0 && timeout[0] > 0 {
		to = timeout[0]
	}

	line := append(append([]byte(nil), raw...), '\n')
	if _, err := s.stdin.Write(line); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	timer := time.NewTimer(to)
	defer timer.Stop()

	select {
	case res, ok := <-s.lines:
		if !ok {
			return nil, fmt.Errorf("stdout closed before response")
		}
		if res.err != nil && res.line == "" {
			return nil, fmt.Errorf("read response: %w", res.err)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(res.line), &m); err != nil {
			return nil, fmt.Errorf("unmarshal response %q: %w", res.line, err)
		}
		return m, nil
	case <-timer.C:
		return nil, fmt.Errorf("timeout after %v waiting for response", to)
	}
}

func (s *TSPSession) closeStdin() {
	s.mu.Lock()
	already := s.stdinClosed
	s.stdinClosed = true
	s.mu.Unlock()
	if !already && s.stdin != nil {
		_ = s.stdin.Close()
	}
}

func (s *TSPSession) waitExit(timeout time.Duration) error {
	s.mu.Lock()
	if s.exited {
		err := s.exitErr
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-s.waitCh:
		return err
	case <-timer.C:
		return fmt.Errorf("gtsp did not exit within %v", timeout)
	}
}

func (s *TSPSession) killSignal() {
	s.t.Helper()
	if err := sendTerminateSignal(s.cmd); err != nil {
		s.t.Fatalf("sendTerminateSignal: %v", err)
	}
}

// WSServerProcess runs gtsp in --mode websocket for multi-session E2E tests.
type WSServerProcess struct {
	t      *testing.T
	cmd    *exec.Cmd
	wsURL  string
	logDir string
	waitCh chan error
}

func startGTSPWS(t *testing.T, opts ...sessionOption) *WSServerProcess {
	t.Helper()
	cfg := &sessionConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.workdir == "" {
		cfg.workdir = t.TempDir()
	}
	if cfg.logDir == "" {
		cfg.logDir = t.TempDir()
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	args := append([]string{"--mode", "websocket", "--port", strconv.Itoa(port), "--log-path", cfg.logDir}, cfg.extraArgs...)
	cmd := exec.Command(gtspBinPath, args...)
	cmd.Dir = cfg.workdir
	cmd.Env = buildProcessEnv()
	setHarnessProcessGroup(cmd)
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start gtsp ws: %v", err)
	}

	srv := &WSServerProcess{
		t:      t,
		cmd:    cmd,
		wsURL:  fmt.Sprintf("ws://127.0.0.1:%d/tsp", port),
		logDir: cfg.logDir,
		waitCh: make(chan error, 1),
	}
	go func() {
		srv.waitCh <- cmd.Wait()
		close(srv.waitCh)
	}()

	t.Cleanup(func() {
		killHarnessProcessTree(srv.cmd)
		select {
		case <-srv.waitCh:
		case <-time.After(3 * time.Second):
		}
	})

	// Wait until TCP port accepts connections
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return srv
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("gtsp websocket server did not listen on %s within 5s", addr)
	return nil
}

type WSSession struct {
	t         *testing.T
	conn      *websocket.Conn
	mu        sync.Mutex
	nextReqID int
}

func wsConnect(t *testing.T, wsURL string, include ...string) *WSSession {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial %s: %v", wsURL, err)
	}
	ws := &WSSession{t: t, conn: conn}
	t.Cleanup(func() { _ = conn.Close() })

	initInput := map[string]any{"protocolVersion": "0.3"}
	if len(include) > 0 {
		initInput["capabilities"] = map[string]any{
			"tools": map[string]any{"include": include},
		}
	}
	resp, err := ws.callMethod("initialize", initInput)
	if err != nil {
		t.Fatalf("ws initialize: %v", err)
	}
	if typ, _ := resp["type"].(string); typ == "error" {
		t.Fatalf("ws initialize error: %v", resp)
	}
	return ws
}

func (w *WSSession) nextID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nextReqID++
	return strconv.Itoa(w.nextReqID)
}

func (w *WSSession) callMethod(method string, input any, timeout ...time.Duration) (map[string]any, error) {
	to := defaultCallTimeout
	if len(timeout) > 0 && timeout[0] > 0 {
		to = timeout[0]
	}
	req := map[string]any{
		"id":     w.nextID(),
		"method": method,
		"input":  input,
	}
	_ = w.conn.SetWriteDeadline(time.Now().Add(to))
	if err := w.conn.WriteJSON(req); err != nil {
		return nil, err
	}
	_ = w.conn.SetReadDeadline(time.Now().Add(to))
	var resp map[string]any
	if err := w.conn.ReadJSON(&resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (w *WSSession) callTool(name string, params any, timeout ...time.Duration) map[string]any {
	w.t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	to := defaultCallTimeout
	if len(timeout) > 0 && timeout[0] > 0 {
		to = timeout[0]
	}
	req := map[string]any{
		"id":     w.nextID(),
		"method": "tool",
		"tool":   name,
		"input":  params,
	}
	_ = w.conn.SetWriteDeadline(time.Now().Add(to))
	if err := w.conn.WriteJSON(req); err != nil {
		w.t.Fatalf("ws write %s: %v", name, err)
	}
	_ = w.conn.SetReadDeadline(time.Now().Add(to))
	var resp map[string]any
	if err := w.conn.ReadJSON(&resp); err != nil {
		w.t.Fatalf("ws read %s: %v", name, err)
	}
	if typ, _ := resp["type"].(string); typ == "error" {
		body, _ := json.Marshal(resp)
		w.t.Fatalf("ws callTool(%s) error: %s", name, body)
	}
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		w.t.Fatalf("ws callTool(%s) missing result: %v", name, resp)
	}
	return result
}

func (w *WSSession) Close() {
	_ = w.conn.Close()
}

// pidIdentity records the first-seen identity of each probed pid so later
// probes can tell "the original descendant" apart from "an unrelated process
// that reused the pid number" — pid alone is not a unique process identity.
// startGTSP resets it per test so records never leak across sessions.
var (
	pidIdentityMu sync.Mutex
	pidIdentity   = map[int]string{}
)

func resetPIDIdentity() {
	pidIdentityMu.Lock()
	defer pidIdentityMu.Unlock()
	pidIdentity = map[int]string{}
}

// checkPIDInstance verifies that pid still refers to the same process instance
// as on first sight, using the platform-provided identity token. Platforms
// without a token pass supported=false and the plain liveness probe is
// trusted as-is.
func checkPIDInstance(pid int, token string, supported bool) bool {
	if !supported {
		return true
	}
	pidIdentityMu.Lock()
	defer pidIdentityMu.Unlock()
	prev, ok := pidIdentity[pid]
	if !ok {
		pidIdentity[pid] = token
		return true
	}
	return prev == token
}

// osSurvivors returns the subset of pids that are still alive in the OS.
// waitForNoSurvivors is preferred at assertion sites: a freshly SIGKILLed process
// still answers kill(pid,0) until the kernel finishes reaping it, so a single
// immediate check races the reaper on slow CI runners.
func osSurvivors(t *testing.T, pids []int) []int {
	t.Helper()
	var alive []int
	for _, pid := range pids {
		if isPIDAlive(pid) {
			alive = append(alive, pid)
		}
	}
	return alive
}

// waitForNoSurvivors polls osSurvivors until empty or the deadline expires,
// absorbing the dying/zombie window between SIGKILL delivery and reap.
// If a process is genuinely alive (a cleanup bug), the deadline still fails.
func waitForNoSurvivors(t *testing.T, pids []int, timeout time.Duration) []int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		surv := osSurvivors(t, pids)
		if len(surv) == 0 {
			return nil
		}
		if !time.Now().Before(deadline) {
			return surv
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// readPIDFile parses whitespace/newline-separated PIDs written by a test script.
func readPIDFile(t *testing.T, path string) []int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pid file %s: %v", path, err)
	}
	var pids []int
	for _, f := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			t.Fatalf("parse pid %q in %s: %v", f, path, err)
		}
		pids = append(pids, pid)
	}
	return pids
}

// extractLeftoverPIDs collects all alive descendant PIDs from a leftover_processes result map.
func extractLeftoverPIDs(res map[string]any) []int {
	lp, _ := res["leftover_processes"].(map[string]any)
	if lp == nil {
		return nil
	}
	tree, _ := lp["tree"].(map[string]any)
	if tree == nil {
		return nil
	}
	var pids []int
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		if alive, _ := node["alive"].(bool); alive {
			if pidFloat, ok := node["pid"].(float64); ok {
				pids = append(pids, int(pidFloat))
			}
		}
		children, _ := node["children"].([]any)
		for _, c := range children {
			if cm, ok := c.(map[string]any); ok {
				walk(cm)
			}
		}
	}
	walk(tree)
	return pids
}

func toolNamesFromInit(t *testing.T, resp map[string]any) []string {
	t.Helper()
	result, _ := resp["result"].(map[string]any)
	caps, _ := result["capabilities"].(map[string]any)
	toolsRaw, _ := caps["tools"].([]any)
	var names []string
	for _, tr := range toolsRaw {
		if m, ok := tr.(map[string]any); ok {
			if n, ok := m["name"].(string); ok {
				names = append(names, n)
			}
		}
	}
	return names
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
