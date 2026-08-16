package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

// These tests spawn the real gtsp binary (via `go run ../src/main.go`), so
// they live in the e2e test folder alongside the process-level scripts.

func TestSchemaCLI(t *testing.T) {
	// Test the new "schema" command
	cmd := exec.Command("go", "run", "../../src/main.go", "schema")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run schema command: %v\nOutput: %s", err, string(output))
	}

	var schemas []interface{}
	if err := json.Unmarshal(output, &schemas); err != nil {
		t.Fatalf("failed to parse schema output as JSON: %v\nOutput: %s", err, string(output))
	}

	if len(schemas) == 0 {
		t.Error("schema output is empty")
	}
}

func TestSchemaOutputFileCLI(t *testing.T) {
	tmpFile := "test_schema.json"
	defer os.Remove(tmpFile)

	// Test "schema -o file"
	cmd := exec.Command("go", "run", "../../src/main.go", "schema", "-o", tmpFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run schema -o: %v\nOutput: %s", err, string(output))
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	var schemas []interface{}
	if err := json.Unmarshal(data, &schemas); err != nil {
		t.Fatalf("failed to parse schema file as JSON: %v", err)
	}

	if len(schemas) == 0 {
		t.Error("schema file is empty")
	}
}

func TestLegacySchemaFlag(t *testing.T) {
	// Test the legacy "--schema" flag still works
	cmd := exec.Command("go", "run", "../../src/main.go", "--schema")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run --schema flag: %v\nOutput: %s", err, string(output))
	}

	var schemas []interface{}
	if err := json.Unmarshal(output, &schemas); err != nil {
		t.Fatalf("failed to parse schema output as JSON: %v\nOutput: %s", err, string(output))
	}

	if len(schemas) == 0 {
		t.Error("schema output is empty")
	}
}
