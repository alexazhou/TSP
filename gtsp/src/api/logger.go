package api

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

var (
	globalLogPath string
	logPathMu     sync.RWMutex
)

// InitLogger initializes the global log directory path and sets up a global logger
func InitLogger(logPath string) error {
	// 1. Create directory if not exists
	if err := os.MkdirAll(logPath, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	// Store the log path for session-specific log files
	logPathMu.Lock()
	globalLogPath = logPath
	logPathMu.Unlock()

	// Create a global log file for system-level messages
	logFile := filepath.Join(logPath, "gt-agent.log")

	// 2. Open log file (append mode, create if not exists)
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}

	// 3. Set output to file
	log.SetOutput(f)

	// Add some standard flags
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Printf("Logger initialized. Log file: %s", logFile)
	return nil
}

// GetLogPath returns the current global log directory path
func GetLogPath() string {
	logPathMu.RLock()
	defer logPathMu.RUnlock()
	return globalLogPath
}

// SessionLogger provides per-session logging with its own log file
type SessionLogger struct {
	sessionID string
	logFile   *os.File
	logger    *log.Logger
	mu        sync.Mutex
	closed    bool
}

// NewSessionLogger creates a new logger for a specific session with its own log file
func NewSessionLogger(sessionID string) (*SessionLogger, error) {
	logPath := GetLogPath()
	if logPath == "" {
		// Fallback: use current directory
		logPath = "./logs"
		if err := os.MkdirAll(logPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %v", err)
		}
	}

	// Create session-specific log file with timestamp
	logFileName := fmt.Sprintf("gt-agent-%s.log", sessionID)
	logFilePath := filepath.Join(logPath, logFileName)

	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create session log file: %v", err)
	}

	logger := log.New(f, "", log.Ldate|log.Ltime|log.Lshortfile)
	logger.Printf("Session logger initialized. Session ID: %s", sessionID)

	return &SessionLogger{
		sessionID: sessionID,
		logFile:   f,
		logger:    logger,
		closed:    false,
	}, nil
}

// Printf logs a formatted message to the session log file
func (sl *SessionLogger) Printf(format string, v ...interface{}) {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if !sl.closed {
		sl.logger.Printf(format, v...)
	}
}

// Close closes the session log file
func (sl *SessionLogger) Close() error {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.closed {
		return nil
	}
	sl.closed = true
	sl.logger.Printf("Session logger closed. Session ID: %s", sl.sessionID)
	return sl.logFile.Close()
}

// GetSessionID returns the session ID
func (sl *SessionLogger) GetSessionID() string {
	return sl.sessionID
}

// GenerateSessionID generates a unique session ID based on timestamp
func GenerateSessionID() string {
	return time.Now().Format("20060102-150405.999")
}

// Global log writer that can be used elsewhere if needed
var LogWriter io.Writer

// recoverPanic logs a panic with stack trace. Call via defer in goroutine entry points.
func recoverPanic(context string) {
	if r := recover(); r != nil {
		log.Printf("PANIC [%s]: %v\n%s", context, r, debug.Stack())
	}
}
