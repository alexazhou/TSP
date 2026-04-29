package handlers_test

import (
	"gTSP/src/api"
	"gTSP/src/tools"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

func TestAllToolsHaveSchema(t *testing.T) {
	dispatcher := api.NewDispatcher()
	tools.RegisterAll(dispatcher)
	
	schemas := dispatcher.GetSchemas()
	expectedTools := []string{
		"list_dir", "read_file", "write_file",
		"execute_bash", "edit", "grep_search", "glob",
		"process_output", "process_stop",
	}

	for _, tool := range expectedTools {
		if _, ok := schemas[tool]; !ok {
			t.Errorf("tool %s is missing a schema", tool)
		}
	}
}

func TestSchemaCLI(t *testing.T) {
	// Test the new "schema" command
	cmd := exec.Command("go", "run", "../src/main.go", "schema")
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
	cmd := exec.Command("go", "run", "../src/main.go", "schema", "-o", tmpFile)
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
	cmd := exec.Command("go", "run", "../src/main.go", "--schema")
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
