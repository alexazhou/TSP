# read_file

Read the content of a text file, with optional line-range selection.

## When to Use

Use `read_file` to inspect the content of a known file. For large files, use `start_line` / `end_line` to read specific sections. To search across many files without reading each one, use [`grep_search`](./grep_search.md) instead.

The returned `content` is line-numbered: each line is prefixed with its 1-based file line number (e.g. `"   12│code"`), so the model can reference specific lines when reasoning or when preparing an [`edit`](./edit.md) call.

## Request

```json
{
    "id": "1",
    "method": "tool",
    "tool": "read_file",
    "input": {
        "file_path": "src/main.go",
        "start_line": 1,
        "end_line": 50
    }
}
```

### Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `file_path` | `string` | **Yes** | Path to the file to read. Subject to workspace sandbox. |
| `start_line` | `integer` | No | 1-based line number to start reading from. Default: `1`. |
| `end_line` | `integer` | No | 1-based line number to stop reading at (inclusive). Default: end of file, capped at `start_line + 499`. |
| `encoding` | `string` | No | File encoding. Default: `utf-8`. |

## Response

```json
{
    "id": "1",
    "type": "result",
    "result": {
        "file_path": "src/main.go",
        "content": "   1│package main\n   2│\n   3│import (\n   4│    \"fmt\"\n",
        "total_lines": 171,
        "start_line": 1,
        "end_line": 50
    }
}
```

### Result Fields

| Field | Type | Description |
|---|---|---|
| `file_path` | `string` | The input path as provided. |
| `content` | `string` | The file content for the requested line range. Each line is prefixed with its 1-based file line number in `%4d│` format (e.g. `"   12│code"`), so the model can reference specific lines. |
| `total_lines` | `integer` | Total number of lines in the file. |
| `start_line` | `integer` | Actual start line returned. |
| `end_line` | `integer` | Actual last line returned. |
| `truncated` | `boolean` | `true` when the file exceeded the full-read size limit and `content` was truncated to the first ~1 KB. Omitted when `false`. |

> **Note for `edit`**: the line-number prefix is for reference only. When using the [`edit`](./edit.md) tool, provide the exact text **without** the prefix.

> **Truncation**: a full read (no `start_line`/`end_line`) of a file larger than 25 KB returns only the first ~1 KB of `content`, sets `truncated: true`, and appends a notice with the file size. Use `start_line`/`end_line` or [`grep_search`](./grep_search.md) to page through large files.

## Safety Limits

| Limit | Value | Behavior on Exceed |
|---|---|---|
| Full-read file size | 25 KB | Truncated to the first ~1 KB, sets `truncated: true` |
| Max lines per call | 500 lines | Silently capped (use `start_line`/`end_line` to page) |
| Binary files | — | Rejected: `file appears to be binary and cannot be read as text` |

Binary detection checks for null bytes and excessive invalid UTF-8 sequences in the first 512 bytes of the file.

## Error Cases

| Condition | Error message |
|---|---|
| File not found | `file not found: <path>` |
| Path is a directory | `path is a directory: <path>` |
| File is binary | `file appears to be binary and cannot be read as text` |
| `start_line` beyond EOF | `start_line (<N>) is beyond total lines (<M>)` |
| Path outside workspace | `security error: path "..." is outside of workspace root "..."` |

## Examples

### Read an entire (small) file

```json
{"id":"1","method":"tool","tool":"read_file","input":{"file_path":"go.mod"}}
```

### Read lines 100–150 of a large file

```json
{"id":"2","method":"tool","tool":"read_file","input":{"file_path":"src/main.go","start_line":100,"end_line":150}}
```
