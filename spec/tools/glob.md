# glob

Find files by name pattern using glob syntax.

## When to Use

Use `glob` when you know the naming convention of the files you're looking for (e.g., "all Go test files", "all TypeScript files under `src/`"). For searching by file *content*, use [`grep_search`](./grep_search.md).

## Request

```json
{
    "id": "1",
    "method": "tool",
    "tool": "glob",
    "input": {
        "pattern": "src/**/*.go",
        "path": "."
    }
}
```

### Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `pattern` | `string` | **Yes** | A glob pattern relative to `path`. Supports `*`, `**`, `?`, and `{a,b}` syntax. |
| `path` | `string` | No | Base directory to search within. Default: workspace root. Subject to workspace sandbox. |
| `case_sensitive` | `boolean` | No | Whether pattern matching is case-sensitive. Default: `false`. |

### Glob Pattern Syntax

| Pattern | Description |
|---|---|
| `*` | Matches any sequence of non-separator characters |
| `**` | Matches any sequence of characters including path separators (recursive) |
| `?` | Matches exactly one non-separator character |
| `[abc]` | Matches any character in the set |
| `{a,b}` | Matches either `a` or `b` |

**Examples:** `*.go`, `**/*.test.ts`, `src/**/*.{js,ts}`, `cmd/*/main.go`

## Response

```json
{
    "id": "1",
    "type": "result",
    "result": [
        "/workspace/src/main.go",
        "/workspace/src/api/dispatcher.go",
        "/workspace/src/tools/read_file.go"
    ]
}
```

The result is a JSON array of **absolute** file paths matching the pattern, or an empty array `[]` if no files match.

## Error Cases

| Condition | Error message |
|---|---|
| Invalid glob syntax | `invalid glob pattern: ...` |
| `path` outside workspace | `security error: path "..." is outside of workspace root "..."` |
| Invalid parameters | `invalid params: <details>` |

## Examples

### Find all Go source files

```json
{"id":"1","method":"tool","tool":"glob","input":{"pattern":"**/*.go"}}
```

### Find all test files under `test/`

```json
{"id":"2","method":"tool","tool":"glob","input":{"pattern":"*_test.go","path":"test"}}
```

### Find all config files at any depth

```json
{"id":"3","method":"tool","tool":"glob","input":{"pattern":"**/*.{json,yaml,toml}"}}
```
