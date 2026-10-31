package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// HandlerFunc defines the signature for request handlers
type HandlerFunc func(session Session, params json.RawMessage) (interface{}, error)

// Dispatcher routes requests to handlers and supports multiple clients (stdio, ws)
type Dispatcher struct {
	handlers     map[string]HandlerFunc
	schemas      map[string]ToolDefinition
	toolOrder    []string // 保存注册顺序
}

// NewDispatcher creates a new RPC dispatcher
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		handlers:  make(map[string]HandlerFunc),
		schemas:   make(map[string]ToolDefinition),
		toolOrder: []string{},
	}
}

// Register adds a new handler for a specific tool name
func (d *Dispatcher) Register(toolName string, handler HandlerFunc) {
	d.handlers[toolName] = handler
}

// RegisterWithSchema adds a new handler and its JSON schema for a specific tool
func (d *Dispatcher) RegisterWithSchema(toolName string, handler HandlerFunc, schema ToolDefinition) {
	d.handlers[toolName] = handler
	d.schemas[toolName] = schema
	d.toolOrder = append(d.toolOrder, toolName)
}

// GetSchemas returns all registered tool schemas
func (d *Dispatcher) GetSchemas() map[string]ToolDefinition {
	return d.schemas
}

// maxLogLineLength caps how much of a single request/response line is written
// to logs. Large tool results (e.g. read_image base64) are kept out of logs by
// the result type's RedactForLog; this cap is a backstop for any other
// oversized line.
const maxLogLineLength = 4096

// truncateForLog shortens an in/out log line for the log file. It only affects
// the log output, never the payload sent to the client. The cut lands on a
// UTF-8 rune boundary so multi-byte content is never split mid-rune.
func truncateForLog(s string) string {
	if len(s) <= maxLogLineLength {
		return s
	}
	cut := maxLogLineLength
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("...[truncated %d bytes]", len(s)-cut)
}

// HandleRequest processes a raw JSON request from a specific client
func (d *Dispatcher) HandleRequest(session Session, client Client, data []byte) {
	// Use session-specific logger if available
	sessionLogger := session.GetLogger()
	if sessionLogger != nil {
		sessionLogger.Printf("→ %s", truncateForLog(string(data)))
	} else {
		log.Printf("→ %s", truncateForLog(string(data)))
	}

	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		d.SendError(session, client, nil, ErrParseError, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if session.IsShuttingDown() {
		id := req.ID
		d.SendError(session, client, &id, ErrShuttingDown, "server is shutting down")
		return
	}

	switch req.Method {
	case "initialize":
		d.handleInitialize(session, client, req)
	case "shutdown":
		d.handleShutdown(session, client, req)
	case "sandbox":
		if !session.IsInitialized() {
			id := req.ID
			d.SendError(session, client, &id, ErrNotInitialized, "server not initialized; send initialize first")
			return
		}
		d.handleSandbox(session, client, req)
	case "tool":
		if !session.IsInitialized() {
			id := req.ID
			d.SendError(session, client, &id, ErrNotInitialized, "server not initialized; send initialize first")
			return
		}
		handler, ok := d.handlers[req.Tool]
		if !ok {
			id := req.ID
			d.SendError(session, client, &id, ErrToolNotFound, fmt.Sprintf("tool not found: %s", req.Tool))
			return
		}

		// Check if tool is allowed in this session
		allowedTools := session.GetAllowedTools()
		if len(allowedTools) > 0 && !allowedTools[req.Tool] {
			id := req.ID
			d.SendError(session, client, &id, ErrUnauthorized, fmt.Sprintf("tool not authorized for this session: %s", req.Tool))
			return
		}

		// Validate params against schema
		if schema, ok := d.schemas[req.Tool]; ok {
			if err := d.validateParams(schema, req.Input); err != nil {
				id := req.ID
				d.SendError(session, client, &id, ErrInvalidParams, err.Error())
				return
			}
		}

		result, err := handler(session, req.Input)
		if err != nil {
			id := req.ID
			var tspErr *TSPError
			if errors.As(err, &tspErr) {
				d.SendError(session, client, &id, tspErr.Code, tspErr.Message)
			} else {
				d.SendError(session, client, &id, ErrInternalError, err.Error())
			}
			return
		}
		d.SendResponse(session, client, req.ID, result)
	default:
		id := req.ID
		d.SendError(session, client, &id, ErrParseError, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

// validateParams validates raw JSON params against the tool's input schema.
// It checks that all required fields are present and that no unknown fields
// are included (since all schemas use additionalProperties: false).
func (d *Dispatcher) validateParams(schema ToolDefinition, raw json.RawMessage) error {
	var params map[string]interface{}
	if err := json.Unmarshal(raw, &params); err != nil {
		return fmt.Errorf("invalid params: %v", err)
	}

	inputSchema, ok := schema.InputSchema.(map[string]interface{})
	if !ok {
		return nil // no schema to validate against
	}

	// Check additionalProperties
	propsRaw, hasProps := inputSchema["properties"]
	additionalProps, _ := inputSchema["additionalProperties"].(bool)

	var knownProps map[string]bool
	if hasProps {
		propsMap, ok := propsRaw.(map[string]interface{})
		if ok {
			knownProps = make(map[string]bool, len(propsMap))
			for k := range propsMap {
				knownProps[k] = true
			}
		}
	}

	// If additionalProperties is false, reject unknown fields
	if !additionalProps && knownProps != nil {
		for k := range params {
			if !knownProps[k] {
				return fmt.Errorf("unknown parameter: %q", k)
			}
		}
	}

	// Check required fields
	if requiredRaw, ok := inputSchema["required"]; ok {
		// required can be []string or []interface{} depending on how the schema was constructed
		var requiredNames []string
		switch v := requiredRaw.(type) {
		case []interface{}:
			for _, r := range v {
				if name, ok := r.(string); ok {
					requiredNames = append(requiredNames, name)
				}
			}
		case []string:
			requiredNames = v
		}
		for _, name := range requiredNames {
			if _, found := params[name]; !found {
				return fmt.Errorf("missing required parameter: %q", name)
			}
		}
	}

	return nil
}

func (d *Dispatcher) handleInitialize(session Session, client Client, req Request) {
	var input InitializeParams
	if err := json.Unmarshal(req.Input, &input); err != nil {
		d.SendError(session, client, &req.ID, ErrParseError, fmt.Sprintf("invalid initialize params: %v", err))
		return
	}

	// 1. Version check (basic major version check)
	if !strings.HasPrefix(input.ProtocolVersion, "0.") {
		d.SendError(session, client, &req.ID, ErrParseError, fmt.Sprintf("unsupported protocol version %q; server supports v0.3", input.ProtocolVersion))
		return
	}

	session.SetInitialized(true)

	// 2. Filter tools
	include := make(map[string]bool)
	for _, name := range input.Capabilities.Tools.Include {
		include[name] = true
	}
	exclude := make(map[string]bool)
	for _, name := range input.Capabilities.Tools.Exclude {
		exclude[name] = true
	}

	allowedTools := make(map[string]bool)
	toolList := make([]ToolDefinition, 0, len(d.schemas))
	for _, name := range d.toolOrder {
		if len(include) > 0 && !include[name] {
			continue
		}
		if exclude[name] {
			continue
		}
		schema, ok := d.schemas[name]
		if !ok {
			continue
		}
		toolList = append(toolList, schema)
		allowedTools[name] = true
	}
	session.SetAllowedTools(allowedTools)

	result := InitializeResult{
		ProtocolVersion: "0.3",
		Workdir:         GetWorkdir(),
	}
	result.ServerInfo.Name = "gTSP"
	result.ServerInfo.Version = Version
	result.Capabilities.Tools = toolList
	result.Capabilities.Sandbox = []string{"read", "write", "network"}

	d.SendResponse(session, client, req.ID, result)
}

func (d *Dispatcher) handleSandbox(session Session, client Client, req Request) {
	// Input: feature name → config
	var input struct {
		Read    []PathRule `json:"read"`
		Write   []PathRule `json:"write"`
		Network *bool      `json:"network"`
	}
	if err := json.Unmarshal(req.Input, &input); err != nil {
		d.SendError(session, client, &req.ID, ErrInvalidParams, fmt.Sprintf("invalid sandbox params: %v", err))
		return
	}

	network := true
	if input.Network != nil {
		network = *input.Network
	}

	// Validate paths
	for i, r := range input.Read {
		abs, err := filepath.Abs(r.Path)
		if err != nil {
			d.SendError(session, client, &req.ID, ErrInvalidParams, fmt.Sprintf("invalid read path %q: %v", r.Path, err))
			return
		}
		input.Read[i].Path = abs
	}
	for i, w := range input.Write {
		abs, err := filepath.Abs(w.Path)
		if err != nil {
			d.SendError(session, client, &req.ID, ErrInvalidParams, fmt.Sprintf("invalid write path %q: %v", w.Path, err))
			return
		}
		input.Write[i].Path = abs
	}

	session.SetPathRules(input.Read, input.Write)
	session.SetNetworkAllowed(network)

	d.SendResponse(session, client, req.ID, map[string]interface{}{})
}

func (d *Dispatcher) handleShutdown(session Session, client Client, req Request) {
	session.SetShuttingDown(true)

	GlobalProcessRegistry.KillAll()
	d.SendResponse(session, client, req.ID, map[string]interface{}{})
}

func (d *Dispatcher) SendResponse(session Session, client Client, id string, result interface{}) {
	resp := Response{
		ID:     id,
		Type:   "result",
		Result: result,
	}
	// Log a redacted copy if the result type opts in; the client always gets
	// the full, unmasked payload.
	logResp := resp
	if lr, ok := result.(LogRedactor); ok {
		logResp.Result = lr.RedactForLog()
	}
	if data, err := json.Marshal(logResp); err == nil {
		sessionLogger := session.GetLogger()
		if sessionLogger != nil {
			sessionLogger.Printf("← %s", truncateForLog(string(data)))
		} else {
			log.Printf("← %s", truncateForLog(string(data)))
		}
	}
	client.WriteJSON(resp)
}

func (d *Dispatcher) SendError(session Session, client Client, id *string, code ErrorCode, errDetail interface{}) {
	resp := ErrorResponse{
		ID:    id,
		Type:  "error",
		Code:  code,
		Error: errDetail,
	}
	if data, err := json.Marshal(resp); err == nil {
		sessionLogger := session.GetLogger()
		if sessionLogger != nil {
			sessionLogger.Printf("← %s", truncateForLog(string(data)))
		} else {
			log.Printf("← %s", truncateForLog(string(data)))
		}
	}
	client.WriteJSON(resp)
}

// StdioClient implements Client interface for standard I/O
type StdioClient struct {
	reader *bufio.Scanner
	writer io.Writer
	mu     sync.Mutex
}

// maxRequestLineBytes 是单行 JSON 请求的上限。
// bufio.Scanner 默认上限 64KB，超过时 Scan 返回 false 且 err = token too long，
// 表现为「请求无响应 + 进程直接退出」，调用方只看到 stdout EOF，原因完全不可见。
// write_file 允许 100KB content、edit 允许长文本，正常请求行会超过 64KB，
// 故放宽到 10MB（与 pytspclient 的 StreamReader limit 对齐）。
const maxRequestLineBytes = 10 * 1024 * 1024

// initialRequestBufBytes 是初始缓冲区大小，避免每行都重新分配 10MB。
const initialRequestBufBytes = 64 * 1024

func NewStdioClient(stdin io.Reader, stdout io.Writer) *StdioClient {
	reader := bufio.NewScanner(stdin)
	reader.Buffer(make([]byte, initialRequestBufBytes), maxRequestLineBytes)
	return &StdioClient{
		reader: reader,
		writer: stdout,
	}
}

func (c *StdioClient) WriteJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("error marshaling response: %v", err)
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.writer.Write(data)
	if err == nil {
		_, err = c.writer.Write([]byte("\n"))
	}
	if err != nil {
		log.Printf("error writing to stdout: %v", err)
	}
	return err
}

// ServeStdio starts the main loop listening for requests on stdin
func (d *Dispatcher) ServeStdio(session Session, client *StdioClient) {
	defer session.CloseLogger()

	var wg sync.WaitGroup
	for client.reader.Scan() {
		line := client.reader.Bytes()
		if len(line) == 0 {
			continue
		}

		buf := make([]byte, len(line))
		copy(buf, line)

		wg.Add(1)
		go func(data []byte) {
			defer wg.Done()
			defer recoverPanic("HandleRequest/stdio")
			d.HandleRequest(session, client, data)
		}(buf)
	}

	wg.Wait()
	if err := client.reader.Err(); err != nil {
		log.Printf("error reading stdin: %v", err)
		log.Printf("server exiting due to stdin error")
	} else {
		log.Printf("stdin closed (EOF), server exiting")
	}

	// stdin 消失后本进程无法再返回任何 tool 结果，残留的后台进程只会变成
	// 调用方看不见的孤儿（容器里则 reparent 到 PID 1）。此处与信号处理路径
	// 保持一致，统一清理。
	GlobalProcessRegistry.KillAll()
}
