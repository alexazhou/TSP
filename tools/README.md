# Built-in Tools Reference

gTSP ships with the following built-in tools. Each tool is registered as a TSP method and exposed in the schema output.

| Tool | Method | Category | Description |
|---|---|---|---|
| [list_dir](./list_dir.md) | `list_dir` | File System | List directory entries |
| [read_file](./read_file.md) | `read_file` | File System | Read file content with optional line range |
| [write_file](./write_file.md) | `write_file` | File System | Create or overwrite a file |
| [edit](./edit.md) | `edit` | File System | Exact string replacement within a file |
| [grep_search](./grep_search.md) | `grep_search` | Search | Search file contents by regex |
| [glob](./glob.md) | `glob` | Search | Find files by glob pattern |
| [execute_bash](./execute_bash.md) | `execute_bash` | Shell | Execute a shell command |
| [process_output](./process_output.md) | `process_output` | Process | Retrieve output from a background process |
| [process_stop](./process_stop.md) | `process_stop` | Process | Terminate a background process |

## Common Conventions

### Path Parameters

All tools that accept file paths enforce the [workspace sandbox](../Specification.md#5-security-model). Paths may be:

- **Relative** — resolved relative to the workspace root.
- **Absolute** — must fall within the workspace root.

### Error Handling

All tools return an [Error Response](../Specification.md#26-error-response) on failure. Common errors:

| Condition | Error message |
|---|---|
| Path outside workspace | `security error: path "..." is outside of workspace root "..."` |
| File not found | `file not found: <path>` |
| Path is a directory | `path is a directory: <path>` |
| File is binary | `file appears to be binary and cannot be read as text` |
| Invalid parameters | `invalid params: <details>` |

### Schema Format

The `input_schema` of each tool follows [JSON Schema draft 2020-12](https://json-schema.org/specification.html). The schema can be retrieved via:

```sh
./gtsp schema
```
