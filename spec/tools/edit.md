# edit

Perform a precise string replacement within a file.

## When to Use

Use `edit` to make surgical changes to a specific part of a file without rewriting the entire content. This is preferable to [`write_file`](./write_file.md) when modifying a small section of a large file.

The replacement is **exact** — `old_string` must match the file content character-for-character, including whitespace and line endings.

## Request

```json
{
    "id": "1",
    "method": "tool",
    "tool": "edit",
    "input": {
        "file_path": "src/main.go",
        "old_string": "const Version = \"v0.1.0\"",
        "new_string": "const Version = \"v0.2.0\""
    }
}
```

### Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `file_path` | `string` | **Yes** | Path to the file to modify. Subject to workspace sandbox. |
| `old_string` | `string` | **Yes** | The exact literal text to find and replace. Whitespace is significant. |
| `new_string` | `string` | **Yes** | The replacement text. May be empty string to delete `old_string`. |
| `allow_multiple` | `boolean` | No | If `true`, replace all occurrences. Default: `false` — fails if more than one occurrence is found. |
| `instruction` | `string` | No | Optional human-readable description of the change (for logging). |

## Response

```json
{
    "id": "1",
    "type": "result",
    "result": {
        "file_path": "src/main.go",
        "status": "success",
        "message": "Successfully replaced 1 occurrence(s)"
    }
}
```

### Result Fields

| Field | Type | Description |
|---|---|---|
| `file_path` | `string` | The input path as provided. |
| `status` | `string` | Always `"success"` on a successful edit. |
| `message` | `string` | Human-readable summary, e.g. `"Successfully replaced 2 occurrence(s)"`. |

## Implementation Details

- **Atomic write:** The edited content is written to a `.tmp` file and then renamed, preventing corruption on failure.
- **Uniqueness check:** By default, the tool counts occurrences of `old_string` before replacing. If multiple are found and `allow_multiple` is `false`, the request is rejected — this prevents accidental mass changes.

## Error Cases

| Condition | Error message |
|---|---|
| `old_string` not found | `could not find old_string in file. Ensure exact match including whitespace` |
| `old_string` found multiple times (and `allow_multiple` is false) | `found <N> occurrences of old_string. Please provide more context or set 'allow_multiple' to true` |
| `old_string` equals `new_string` | `no changes to apply: old_string and new_string are identical` |
| File not found | `failed to read file <path>: ...` |
| Path outside workspace | `security error: path "..." is outside of workspace root "..."` |

## Examples

### Rename a function

```json
{
    "id": "1",
    "method": "tool",
    "tool": "edit",
    "input": {
        "file_path": "src/api/dispatcher.go",
        "old_string": "func (d *Dispatcher) HandleRequest(",
        "new_string": "func (d *Dispatcher) Dispatch("
    }
}
```

### Replace all occurrences of a string (rename variable)

```json
{
    "id": "2",
    "method": "tool",
    "tool": "edit",
    "input": {
        "file_path": "src/main.go",
        "old_string": "oldVarName",
        "new_string": "newVarName",
        "allow_multiple": true
    }
}
```

### Delete a line by replacing with empty string

```json
{
    "id": "3",
    "method": "tool",
    "tool": "edit",
    "input": {
        "file_path": "config.json",
        "old_string": "    \"debug\": true,\n",
        "new_string": ""
    }
}
```
