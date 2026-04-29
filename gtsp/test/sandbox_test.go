package handlers_test

import (
	"gTSP/src/api"
	"gTSP/src/tools"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandbox_PathValidation(t *testing.T) {
	// Create a real directory to ensure filepath.Abs works consistently
	tmpDir, _ := filepath.Abs("test_sandbox_root_dir")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	// Enable sandbox for testing
	api.SetSandboxEnabled(true)

	err := api.SetWorkdirRoot(tmpDir)
	if err != nil {
		t.Fatalf("SetWorkdirRoot failed: %v", err)
	}

	// 1. Valid path inside workspace
	validFile := "file.txt"
	expectedAbs := filepath.Join(tmpDir, validFile)
	
	abs, err := api.ValidatePath(validFile)
	if err != nil {
		t.Errorf("Expected valid path for %s, got error: %v", validFile, err)
	}
	if abs != expectedAbs {
		t.Errorf("Expected %s, got %s", expectedAbs, abs)
	}

	// 2. Invalid path outside workspace (absolute)
	_, err = api.ValidatePath("/etc/passwd")
	if err == nil {
		t.Error("Expected error for /etc/passwd, but got nil")
	}

	// 3. Path traversal attempt
	_, err = api.ValidatePath("../../../etc/passwd")
	if err == nil {
		t.Error("Expected error for path traversal, but got nil")
	}
}

func TestSandbox_ToolIntegration(t *testing.T) {
	tmpDir, _ := filepath.Abs("test_tool_sandbox")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	// Enable sandbox for testing
	api.SetSandboxEnabled(true)

	api.SetWorkdirRoot(tmpDir)
	session := api.NewSession()
	session.SetInitialized(true)
	// No rules set -> default deny

	// Try to read a file outside the sandbox using the tool
	params := json.RawMessage(`{"file_path": "/etc/passwd"}`)
	_, err := tools.ReadFileHandler(session, params)
	if err == nil {
		t.Error("Tool should have failed reading /etc/passwd due to sandbox")
	} else if !strings.Contains(err.Error(), "security error") {
		t.Errorf("Expected security error, got: %v", err)
	}

	// Try to write a file outside the sandbox
	params = json.RawMessage(`{"file_path": "../malicious.txt", "content": "hack"}`)
	_, err = tools.WriteFileHandler(session, params)
	if err == nil {
		t.Error("Tool should have failed writing outside sandbox")
	}
}

func TestSandbox_SessionRules(t *testing.T) {
	tmpDir, _ := filepath.Abs("test_session_sandbox")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	// Enable sandbox for testing
	api.SetSandboxEnabled(true)

	api.SetWorkdirRoot(tmpDir)
	
	session := api.NewSession()
	session.SetInitialized(true)
	
	// Set rule: allow only "allowed_dir"
	allowedPath := filepath.Join(tmpDir, "allowed_dir")
	os.MkdirAll(allowedPath, 0755)
	
	rule := api.PathRule{Action: "allow", Path: allowedPath}
	session.SetPathRules(
		[]api.PathRule{rule},
		[]api.PathRule{rule},
	)
	
	// 1. Access allowed path
	filePath := filepath.Join(allowedPath, "test.txt")
	os.WriteFile(filePath, []byte("hello"), 0644)
	
	params := json.RawMessage(`{"file_path": "allowed_dir/test.txt"}`)
	_, err := tools.ReadFileHandler(session, params)
	if err != nil {
		t.Errorf("Expected access allowed to %s, got error: %v", filePath, err)
	}
	
	// 2. Access denied path (inside workdirRoot but not in allow rules)
	deniedPath := filepath.Join(tmpDir, "denied.txt")
	os.WriteFile(deniedPath, []byte("secret"), 0644)
	
	params = json.RawMessage(`{"file_path": "denied.txt"}`)
	_, err = tools.ReadFileHandler(session, params)
	if err == nil {
		t.Error("Expected access denied for denied.txt, but got nil")
	} else if !strings.Contains(err.Error(), "denied") {
		t.Errorf("Expected denial error, got: %v", err)
	}
}
