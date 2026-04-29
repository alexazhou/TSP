# list_dir

List the contents of a directory, with optional recursive traversal.

## When to Use

Use `list_dir` when you need to understand the structure of a directory or locate files within a known path. For pattern-based file searching across the entire workspace, prefer [`glob`](./glob.md).

## Request

```json
{
    "id": "1",
    "method": "tool",
    "tool": "list_dir",
    "input": {
        "dir_path": "src",
        "recursive": true,
        "depth": 2
    }
}
```

### Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `dir_path` | `string` | **Yes** | Path to the directory to list. Subject to workspace sandbox. |
| `recursive` | `boolean` | No | Whether to list subdirectories recursively. Default: `false`. |
| `depth` | `integer` | No | Maximum recursion depth. `0` means current directory only. Default: `0`. When `recursive` is `true` and `depth` is unset or `0`, defaults to `1`. |
| `ignore_patterns` | `string[]` | No | Additional glob patterns to skip (e.g., `["*.tmp", "node_modules"]`). |

**Always ignored:** `.git`, `.DS_Store`

## Response

```json
{
    "id": "1",
    "type": "result",
    "result": {
        "dir_path": "/workspace/src",
        "items": [
            {
                "name": "main.go",
                "path": "main.go",
                "is_dir": false,
                "size": 4096,
                "mod_time": "2024-01-15T10:30:00Z",
                "type": "file"
            },
            {
                "name": "tools",
                "path": "tools",
                "is_dir": true,
                "size": 128,
                "mod_time": "2024-01-15T09:00:00Z",
                "type": "dir"
            }
        ]
    }
}
```

### Result Fields

| Field | Type | Description |
|---|---|---|
| `dir_path` | `string` | The resolved absolute path of the listed directory. |
| `items` | `FileInfo[]` | Array of entries. |

### FileInfo Object

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Entry name (filename or directory name). |
| `path` | `string` | Path relative to the listed `dir_path`. |
| `is_dir` | `boolean` | `true` if this entry is a directory. |
| `size` | `integer` | File size in bytes. For directories, this is the directory entry size. |
| `mod_time` | `string` | Last modification time in RFC 3339 format (e.g., `"2024-01-15T10:30:00Z"`). |
| `type` | `string` | One of `"file"`, `"dir"`, or `"symlink"`. |

## Error Cases

| Condition | Error message |
|---|---|
| Directory does not exist | `directory not found: <path>` |
| Path outside workspace | `security error: path "..." is outside of workspace root "..."` |
| Permission denied | `error accessing directory: ...` |

## Examples

### List current directory (flat)

```json
{"id":"1","method":"tool","tool":"list_dir","input":{"dir_path":"."}}
```

### Recursive listing up to 3 levels deep

```json
{"id":"2","method":"tool","tool":"list_dir","input":{"dir_path":".","recursive":true,"depth":3}}
```

### List and ignore build artifacts

```json
{"id":"3","method":"tool","tool":"list_dir","input":{"dir_path":".","ignore_patterns":["*.o","dist","build"]}}
```
