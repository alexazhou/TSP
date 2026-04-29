package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var sandboxEnabled bool
var workdirRoot string
var currentWorkdir string

// SetSandboxEnabled sets whether sandbox restrictions are active.
func SetSandboxEnabled(enabled bool) {
	sandboxEnabled = enabled
}

// IsSandboxEnabled returns whether sandbox is currently enabled.
func IsSandboxEnabled() bool {
	return sandboxEnabled
}

// SetWorkdirRoot sets the absolute root of the server-level sandbox.
// All file operations are checked against this root before any rule evaluation.
func SetWorkdirRoot(rootPath string) error {
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for workdir root: %v", err)
	}
	workdirRoot = absRoot
	// Also set the current working directory to this root initially.
	currentWorkdir = absRoot
	return nil
}

// GetWorkdirRoot returns the current workdir root.
func GetWorkdirRoot() string {
	return workdirRoot
}

// GetWorkdir returns the current working directory.
func GetWorkdir() string {
	return currentWorkdir
}

// ValidatePath checks if the given path is within the workdir root.
// Returns the absolute path if valid, or an error if invalid/out of bounds.
// Note: This only checks against the top-level workdir. Use CheckRead/CheckWrite
// for per-session PathRule validation.
func ValidatePath(path string) (string, error) {
	// If sandbox is disabled, allow any path
	if !sandboxEnabled {
		return filepath.Abs(path)
	}

	if workdirRoot == "" {
		return filepath.Abs(path)
	}

	// 1. If path is relative, make it relative to currentWorkdir
	var absInput string
	if filepath.IsAbs(path) {
		absInput = filepath.Clean(path)
	} else {
		absInput = filepath.Join(currentWorkdir, path)
	}

	// 2. Ensure root has a trailing separator for prefix matching
	root := workdirRoot
	if !strings.HasSuffix(root, string(os.PathSeparator)) {
		root += string(os.PathSeparator)
	}

	// 3. Exact match or starts with prefix
	if absInput == workdirRoot || strings.HasPrefix(absInput, root) {
		return absInput, nil
	}

	return "", &TSPError{
		Code:    ErrSandboxDenied,
		Message: fmt.Sprintf("security error: path %q is outside of workdir root %q", path, workdirRoot),
	}
}
