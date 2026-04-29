package api

import "encoding/json"

const Version = "v0.4.6"

// ErrorCode defines typed error codes per TSP v0.3 spec
type ErrorCode = string

const (
	// Protocol errors
	ErrParseError      ErrorCode = "protocol/parse_error"
	ErrToolNotFound    ErrorCode = "protocol/tool_not_found"
	ErrInvalidParams   ErrorCode = "protocol/invalid_params"
	ErrNotInitialized  ErrorCode = "protocol/not_initialized"
	ErrShuttingDown    ErrorCode = "protocol/shutting_down"

	// Security errors
	ErrUnauthorized    ErrorCode = "security/unauthorized"
	ErrSandboxDenied   ErrorCode = "security/sandbox_denied"
	ErrOSDenied        ErrorCode = "security/os_denied"

	// Execution errors
	ErrExecTimeout     ErrorCode = "exec/timeout"

	// Resource errors
	ErrNotFound        ErrorCode = "resource/not_found"

	// Server errors
	ErrInternalError   ErrorCode = "server/internal_error"
)

// TSPError is a structured error with a typed error code
type TSPError struct {
	Code    ErrorCode   `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *TSPError) Error() string {
	return e.Message
}

// Request represents a TSP v0.3 request
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Tool   string          `json:"tool,omitempty"`
	Input  json.RawMessage `json:"input"`
}

// Response represents a successful TSP result response
type Response struct {
	ID     string      `json:"id"`
	Type   string      `json:"type"`
	Result interface{} `json:"result,omitempty"`
}

// ErrorResponse represents a TSP error response
type ErrorResponse struct {
	ID    *string     `json:"id"`
	Type  string      `json:"type"`
	Code  ErrorCode   `json:"code"`
	Error interface{} `json:"error"`
}

// InitializeParams defines input for the initialize request
type InitializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version,omitempty"`
	} `json:"clientInfo,omitempty"`
	Auth struct {
		Token string `json:"token,omitempty"`
	} `json:"auth,omitempty"`
	Capabilities struct {
		Tools struct {
			Include []string `json:"include,omitempty"`
			Exclude []string `json:"exclude,omitempty"`
		} `json:"tools,omitempty"`
	} `json:"capabilities,omitempty"`
}

// InitializeResult defines output for the initialize response
type InitializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	ServerInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
	Capabilities struct {
		Tools   []ToolDefinition `json:"tools"`
		Sandbox []string         `json:"sandbox,omitempty"`
	} `json:"capabilities"`
	Workdir string `json:"workdir"`
}


// ToolDefinition represents a standardized tool schema for LLM registration
type ToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"input_schema"`
}

// Session represents a per-connection state for the TSP v0.3 protocol.
type Session interface {
	IsInitialized() bool
	SetInitialized(v bool)
	IsShuttingDown() bool
	SetShuttingDown(v bool)
	GetPathRules() (read []PathRule, write []PathRule)
	SetPathRules(read []PathRule, write []PathRule)
	GetNetworkAllowed() bool
	SetNetworkAllowed(v bool)
	GetAllowedTools() map[string]bool
	SetAllowedTools(v map[string]bool)

	// Permission checks
	CheckRead(absPath string) error
	CheckWrite(absPath string) error
	CheckNetwork() error

	// Session-specific logging
	GetSessionID() string
	GetLogger() *SessionLogger
	CloseLogger() error
}

// Client defines an interface for sending responses/events back to the caller
type Client interface {
	WriteJSON(v interface{}) error
}
