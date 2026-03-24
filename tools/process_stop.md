# process_stop

Terminate a background process.

## When to Use

Use `process_stop` to kill a background process started by a long-running `execute_bash` call — for example, to stop a server, cancel a build, or clean up after an error.

## Request

```json
{
    "id": "5",
    "method": "tool",
    "tool": "process_stop",
    "input": {
        "process_id": "proc-abc123"
    }
}
```

### Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `process_id` | `string` | **Yes** | The process handle returned by `execute_bash`. |

## Response

```json
{
    "id": "5",
    "type": "result",
    "result": {}
}
```

The process has been terminated by the time the response is returned.

## Error Cases

| Condition | Error code |
|---|---|
| Unknown `process_id` | `resource/not_found` |
| Process has already exited | Returns `{}` (no-op) |
| Invalid parameters | `protocol/invalid_params` |

## Examples

### Stop a background server

```json
{"id":"5","method":"tool","tool":"process_stop","input":{"process_id":"proc-abc123"}}
```
