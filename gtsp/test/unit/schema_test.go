package unit_test

import (
	"gTSP/src/api"
	"gTSP/src/tools"
	"testing"
)

func TestAllToolsHaveSchema(t *testing.T) {
	dispatcher := api.NewDispatcher()
	tools.RegisterAll(dispatcher)

	schemas := dispatcher.GetSchemas()
	expectedTools := []string{
		"list_dir", "read_file", "read_image", "write_file",
		"execute_bash", "edit", "grep_search", "glob",
		"process_output", "process_stop", "process_list",
	}

	for _, tool := range expectedTools {
		if _, ok := schemas[tool]; !ok {
			t.Errorf("tool %s is missing a schema", tool)
		}
	}
}

func TestAllSchemasHaveRequiredField(t *testing.T) {
	dispatcher := api.NewDispatcher()
	tools.RegisterAll(dispatcher)

	schemas := dispatcher.GetSchemas()

	for toolName, schema := range schemas {
		inputSchema, ok := schema.InputSchema.(map[string]interface{})
		if !ok {
			t.Errorf("tool %s: InputSchema is not a map", toolName)
			continue
		}

		if _, hasRequired := inputSchema["required"]; !hasRequired {
			t.Errorf("tool %s: InputSchema missing 'required' field", toolName)
		}
	}
}
