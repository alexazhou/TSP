# process_output

Retrieve output from a background process and check whether it has finished.

## When to Use

Use `process_output` after receiving a `process_id` from a long-running `execute_bash` call. Call it repeatedly to poll for completion, or set `block: true` to wait until the process exits.

## Request

```json
{
    "id": "2",
    "method": "tool",
    "tool": "process_output",
    "input": {
        "process_id": "proc-abc123",
        "block": true,
        "timeout": 30000
    }
}
```

### Parameters

| Field | Type | Required | Description |
|---|---|---|---|
| `process_id` | `string` | **Yes** | The process handle returned by `execute_bash`. |
| `block` | `boolean` | No | If `true`, wait until the process exits before returning. If `false`, return the current output immediately. Default: `true`. |
| `timeout` | `integer` | No | Maximum milliseconds to wait when `block: true`. If the process has not exited by this deadline, returns with `running: true`. Default: `30000`. Maximum: `600000`. |

## Response

```json
{
    "id": "2",
    "type": "result",
    "result": {
        "process_id": "proc-abc123",
        "stdout": "Starting server...\nListening on :9001\n",
        "stderr": "",
        "running": false,
        "exit_code": 0,
        "truncated": false
    }
}
```

### Result Fields

| Field | Type | Description |
|---|---|---|
| `process_id` | `string` | Echoes the input `process_id`. |
| `stdout` | `string` | All stdout collected since the process started. May be truncated. |
| `stderr` | `string` | All stderr collected since the process started. May be truncated. |
| `running` | `boolean` | `true` if the process is still running. |
| `exit_code` | `integer` \| `null` | Exit code of the process. `null` if the process is still running. |
| `truncated` | `boolean` | `true` if stdout or stderr was truncated due to size limits. |

## Output Limits

| Limit | Value | Behavior on Exceed |
|---|---|---|
| Max lines (stdout or stderr) | 1000 lines | Truncated; `truncated: true` in result |
| Max bytes (stdout or stderr) | 50 KB | Truncated; `truncated: true` in result |

## Error Cases

| Condition | Error code |
|---|---|
| Unknown `process_id` | `resource/not_found` |
| Invalid parameters | `protocol/invalid_params` |

## Examples

### Wait for process to finish (up to 60 seconds)

```json
{"id":"2","method":"tool","tool":"process_output","input":{"process_id":"proc-abc123","block":true,"timeout":60000}}
```

### Non-blocking poll — check current output without waiting

```json
{"id":"3","method":"tool","tool":"process_output","input":{"process_id":"proc-abc123","block":false}}
```
