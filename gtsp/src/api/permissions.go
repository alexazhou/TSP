package api

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PathRule represents a single access control rule: allow or deny a path.
type PathRule struct {
	Action string `json:"action"` // "allow" | "deny"
	Path   string `json:"path"`   // absolute path
}

func (r PathRule) pathClean() string {
	return filepath.Clean(r.Path)
}

// TSPSession implements the Session interface.
type TSPSession struct {
	mu             sync.RWMutex
	initialized    bool
	shuttingDown   bool
	readRules      []PathRule
	writeRules     []PathRule
	networkAllowed bool
	allowedTools   map[string]bool
	sessionID      string
	logger         *SessionLogger
}

func NewSession() Session {
	sessionID := GenerateSessionID()
	session := &TSPSession{
		sessionID:      sessionID,
		networkAllowed: true, // Default to true as per existing code
	}

	// Create session-specific logger
	logger, err := NewSessionLogger(sessionID)
	if err != nil {
		// Fallback to global logger if session logger creation fails
		log.Printf("Warning: failed to create session logger for %s: %v", sessionID, err)
	} else {
		session.logger = logger
	}

	return session
}

// GetSessionID returns the unique session ID
func (s *TSPSession) GetSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID
}

// GetLogger returns the session-specific logger
func (s *TSPSession) GetLogger() *SessionLogger {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.logger
}

// CloseLogger closes the session logger
func (s *TSPSession) CloseLogger() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logger != nil {
		return s.logger.Close()
	}
	return nil
}

func (s *TSPSession) IsInitialized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.initialized
}

func (s *TSPSession) SetInitialized(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initialized = v
}

func (s *TSPSession) IsShuttingDown() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shuttingDown
}

func (s *TSPSession) SetShuttingDown(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shuttingDown = v
}

func (s *TSPSession) GetPathRules() (read []PathRule, write []PathRule) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readRules, s.writeRules
}

func (s *TSPSession) SetPathRules(read []PathRule, write []PathRule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readRules = read
	s.writeRules = write
}

func (s *TSPSession) GetNetworkAllowed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.networkAllowed
}

func (s *TSPSession) SetNetworkAllowed(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.networkAllowed = v
}

func (s *TSPSession) GetAllowedTools() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.allowedTools
}

func (s *TSPSession) SetAllowedTools(v map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowedTools = v
}

func (s *TSPSession) CheckRead(absPath string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return checkPathRules(absPath, s.readRules, "read")
}

func (s *TSPSession) CheckWrite(absPath string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return checkPathRules(absPath, s.writeRules, "write")
}

func (s *TSPSession) CheckNetwork() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.networkAllowed {
		return &TSPError{
			Code:    ErrSandboxDenied,
			Message: "security error: outbound network access is blocked by sandbox",
		}
	}
	return nil
}

func checkPathRules(absTarget string, rules []PathRule, actionType string) error {
	// If sandbox is disabled, allow all access
	if !sandboxEnabled {
		return nil
	}

	target, err := filepath.Abs(absTarget)
	if err != nil {
		target = filepath.Clean(absTarget)
	}
	// On systems like macOS, /var is a symlink to /private/var.
	// We try to evaluate symlinks to get the canonical path for robust matching.
	if evaled, err := filepath.EvalSymlinks(target); err == nil {
		target = evaled
	}

	for _, rule := range rules {
		rulePath, err := filepath.Abs(rule.pathClean())
		if err != nil {
			rulePath = rule.pathClean()
		}
		if evaled, err := filepath.EvalSymlinks(rulePath); err == nil {
			rulePath = evaled
		}

		// Match: target is the rule path, or a child of it.
		matched := false
		if target == rulePath {
			matched = true
		} else {
			prefix := rulePath
			if !strings.HasSuffix(prefix, string(os.PathSeparator)) {
				prefix += string(os.PathSeparator)
			}
			if strings.HasPrefix(target, prefix) {
				matched = true
			}
		}

		if matched {
			if rule.Action == "allow" {
				return nil
			}
			return &TSPError{
				Code:    ErrSandboxDenied,
				Message: fmt.Sprintf("security error: %s access to %q is explicitly denied by sandbox rule", actionType, absTarget),
			}
		}
	}

	// Default deny
	return &TSPError{
		Code:    ErrSandboxDenied,
		Message: fmt.Sprintf("security error: %s access to %q is denied (no matching allow rule)", actionType, absTarget),
	}
}
