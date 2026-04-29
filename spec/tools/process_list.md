# `process_list`

Lists all currently running background processes started by `execute_bash`.

## Request

- **Method**: `tool`
- **Tool**: `process_list`
- **Input**:
  - None (empty object `{}`)

## Response

- **Type**: `result`
- **Result**:
  - `processes`: An array of process objects, each containing:
    - `process_id`: The unique identifier for the process.
    - `command`: The command string being executed.
    - `running_time`: The human-readable time the process has been running (e.g., "1h 21m 39s").
    - `started_at`: (Optional) ISO 8601 timestamp of when the process started.
    - `status`: Always `"running"` for entries in this list.

### Example Request

```json
{
  "id": "list-1",
  "method": "tool",
  "tool": "process_list",
  "input": {}
}
```

### Example Response

```json
{
  "id": "list-1",
  "type": "result",
  "result": {
    "processes": [
      {
        "process_id": "proc-xyz123",
        "command": "npm run start",
        "running_time": "2m 0s",
        "started_at": "2024-03-28T10:00:00Z",
        "status": "running"
      },
      {
        "process_id": "proc-abc456",
        "command": "python worker.py",
        "running_time": "45s",
        "started_at": "2024-03-28T10:05:30Z",
        "status": "running"
      }
    ]
  }
}
```
