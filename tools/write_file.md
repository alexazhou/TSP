# write_file

Create a new file or completely overwrite an existing file with provided content.

## When to Use

Use `write_file` to create a new file or replace a file's content in its entirety. To make targeted changes to part of a file without rewriting it, use [`edit`](./edit.md) instead.

## Request

```json
{
    "id": "1",
    "method": "tool",
    "tool": "write_file",
    "input": {
        "file_path": "src/config.go",
        "content": "package main\n\nconst DefaultPort = 8080\n"
    }
}
```

### Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `file_path` | `string` | **Yes** | Destination path. Subject to workspace sandbox. |
| `content` | `string` | **Yes** | Complete file content to write. Must be the full content — do not use placeholders like `// ... existing code ...`. |
| `encoding` | `string` | No | File encoding. Default: `utf-8`. |

## Response

```json
{
    "id": "1",
    "type": "result",
    "result": {
        "file_path": "src/config.go",
        "written": 42
    }
}
```

### Result Fields

| Field | Type | Description |
|---|---|---|
| `file_path` | `string` | The input path as provided. |
| `written` | `integer` | Number of bytes written. |

## Implementation Details

- **Atomic write:** The server writes to a `.tmp` file alongside the target and then renames it. This prevents partial writes from corrupting the destination file.
- **Parent directory creation:** Missing parent directories are created automatically (equivalent to `mkdir -p`).
- **File permissions:** Created files have mode `0644`.

## Safety Limits

| Limit | Value | Behavior on Exceed |
|---|---|---|
| Maximum content size | 100 KB | Error: split the content or use multiple writes |

## Error Cases

| Condition | Error message |
|---|---|
| Content exceeds 100 KB | `content is too large (<N> bytes). Maximum allowed is 102400 bytes...` |
| Path outside workspace | `security error: path "..." is outside of workspace root "..."` |
| Cannot create parent directory | `failed to create directory <dir>: ...` |
| Write failure | `failed to write temporary file: ...` |

## Examples

### Create a new file

```json
{
    "id": "1",
    "method": "tool",
    "tool": "write_file",
    "input": {
        "file_path": "scripts/deploy.sh",
        "content": "#!/bin/bash\nset -e\necho 'Deploying...'\n"
    }
}
```

### Overwrite an existing file (creates parent dirs if missing)

```json
{
    "id": "2",
    "method": "tool",
    "tool": "write_file",
    "input": {
        "file_path": "config/app/production.json",
        "content": "{\"env\":\"production\",\"port\":443}\n"
    }
}
```
