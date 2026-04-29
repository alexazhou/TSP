# grep_search

Search file contents using a regular expression or literal string pattern.

## When to Use

Use `grep_search` to find where a symbol, string, or pattern appears across many files without reading each one individually. It is the primary tool for code exploration and cross-file navigation.

For finding files by name rather than content, use [`glob`](./glob.md).

## Request

```json
{
    "id": "1",
    "method": "tool",
    "tool": "grep_search",
    "input": {
        "pattern": "func\\s+New\\w+",
        "dir_path": "src",
        "include_pattern": "*.go",
        "context": 2
    }
}
```

### Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `pattern` | `string` | **Yes** | The search pattern. Interpreted as a Go regular expression unless `fixed_strings` is `true`. |
| `dir_path` | `string` | No | Subdirectory to search within. Default: workspace root (`.`). Subject to workspace sandbox. |
| `include_pattern` | `string` | No | Glob pattern to restrict which files are searched (e.g., `"*.go"`, `"*.{ts,tsx}"`). |
| `exclude_pattern` | `string` | No | Glob pattern to exclude files (e.g., `"*_test.go"`). *(Currently applied at filename level.)* |
| `fixed_strings` | `boolean` | No | Treat `pattern` as a literal string (no regex). Default: `false`. |
| `case_sensitive` | `boolean` | No | Whether the match is case-sensitive. Default: `false` (case-insensitive). |
| `context` | `integer` | No | Number of lines to include before and after each match. Default: `0`. |
| `total_max_matches` | `integer` | No | Maximum total matches to return across all files. Default: `100`. |
| `max_matches_per_file` | `integer` | No | Maximum matches to return per file. Default: `10`. |

**Always skipped:** `.git/`, `node_modules/`

## Response

```json
{
    "id": "1",
    "type": "result",
    "result": {
        "matches": [
            {
                "file_path": "/workspace/src/api/dispatcher.go",
                "line_number": 22,
                "content": "func NewDispatcher() *Dispatcher {",
                "context": [
                    "// NewDispatcher creates a new RPC dispatcher",
                    "func NewDispatcher() *Dispatcher {"
                ]
            }
        ],
        "truncated": false
    }
}
```

### Result Fields

| Field | Type | Description |
|---|---|---|
| `matches` | `MatchInfo[]` | Array of matching lines. |
| `truncated` | `boolean` | `true` if results were cut short by `total_max_matches`. Narrow your search if this is `true`. |

### MatchInfo Object

| Field | Type | Description |
|---|---|---|
| `file_path` | `string` | Absolute path to the file containing the match. |
| `line_number` | `integer` | 1-based line number of the match. |
| `content` | `string` | The matching line's text (trimmed). |
| `context` | `string[]` | Surrounding lines (only present when `context > 0`). |

## Safety Limits

| Limit | Default | Override |
|---|---|---|
| Max total matches | 100 | `total_max_matches` parameter |
| Max matches per file | 10 | `max_matches_per_file` parameter |

When `truncated` is `true`, increase these limits or narrow the search with `dir_path` or `include_pattern`.

## Error Cases

| Condition | Error message |
|---|---|
| Invalid regex | `invalid regex: <details>` |
| `dir_path` outside workspace | `security error: path "..." is outside of workspace root "..."` |
| Invalid parameters | `invalid params: <details>` |

## Examples

### Find all usages of a function

```json
{"id":"1","method":"tool","tool":"grep_search","input":{"pattern":"ValidatePath","include_pattern":"*.go"}}
```

### Case-sensitive literal string search

```json
{"id":"2","method":"tool","tool":"grep_search","input":{"pattern":"TODO","fixed_strings":true,"case_sensitive":true}}
```

### Find struct definitions with surrounding context

```json
{"id":"3","method":"tool","tool":"grep_search","input":{"pattern":"^type \\w+ struct","include_pattern":"*.go","context":3}}
```
