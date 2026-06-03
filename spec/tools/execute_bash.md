# execute_bash

Execute a shell command and return its output.

## When to Use

Use `execute_bash` for operations that cannot be expressed through the other tools — running build scripts, executing tests, installing dependencies, querying system state, etc.

> **Security notice:** Shell commands run under the server process's OS user permissions and can access the full file system, not just the workspace. Only expose `execute_bash` to agents that are trusted to run arbitrary code on the host machine.

## Request

```json
{
    "id": "1",
    "method": "tool",
    "tool": "execute_bash",
    "input": {
        "command": "go test ./... -v",
        "task_timeout": 30,
        "description": "Run all unit tests"
    }
}
```

### Parameters

| Field | Type | Default | Description |
|---|---|---|---|
| `command` | `string` | — | **Required.** The bash command to execute. Runs via `bash -c <command>`. |
| `task_timeout` | `integer` | `60` | Timeout in seconds. `0` means use the default (60s). Ignored when `run_in_background: true`. |
| `timeout_action` | `string` | `"background"` | Action when timeout expires: `"background"` promotes to background; `"kill"` terminates the process group and returns partial output. |
| `run_in_background` | `boolean` | `false` | Start immediately in background. Equivalent to `task_timeout: 0, timeout_action: "background"`. |
| `description` | `string` | — | Optional human-readable description of the command's purpose (for logging). |

#### Parameter combinations

| Scenario | Parameters | Behavior |
|---|---|---|
| Default (no params) | _(none)_ | Waits up to 60s. Returns result if done; otherwise promotes to background and returns `process_id`. |
| Custom timeout, background on expiry | `task_timeout: 120` | Waits up to 120s. Returns result if done; otherwise promotes to background. |
| Timeout with kill | `task_timeout: 60, timeout_action: "kill"` | Waits up to 60s. Returns result if done; otherwise kills the process group and returns partial output. |
| Immediate background | `run_in_background: true` | Registers process, returns `process_id` immediately. |

Use [`process_output`](./process_output.md) to retrieve output and check whether a background process has finished.

## Response

### Short-lived command (exits within `task_timeout`)

```json
{
    "id": "1",
    "type": "result",
    "result": {
        "stdout": "ok  \tgTSP/src/...\n",
        "stderr": "",
        "exit_code": 0,
        "truncated": false
    }
}
```

| Field | Type | Description |
|---|---|---|
| `stdout` | `string` | Standard output of the command. May be truncated (see limits below). |
| `stderr` | `string` | Standard error of the command. May be truncated. |
| `exit_code` | `integer` | The command's exit code. `0` indicates success. Non-zero indicates failure. |
| `truncated` | `boolean` | `true` if either `stdout` or `stderr` was truncated due to size limits. |

A non-zero `exit_code` does **not** produce an Error Response — the result is still returned with `type: "result"`. This allows the caller to inspect partial output and stderr.

### Long-running command (still running after `task_timeout`)

```json
{
    "id": "1",
    "type": "result",
    "result": {
        "process_id": "proc-abc123",
        "status": "running",
        "stdout": "Starting server...\n",
        "stderr": ""
    }
}
```

| Field | Type | Description |
|---|---|---|
| `process_id` | `string` | Opaque handle. Pass to [`process_output`](./process_output.md) or [`process_stop`](./process_stop.md). |
| `status` | `"running"` | Indicates the process is still running in the background. |
| `stdout` | `string` | Output collected during the `task_timeout` window. |
| `stderr` | `string` | Stderr collected during the `task_timeout` window. |

At the same time, the server emits a `process/pending` event:

```json
{
    "type": "event",
    "event": "process/pending",
    "data": {
        "process_id": "proc-abc123",
        "elapsed": 10,
        "stdout": "Starting server...\n",
        "stderr": ""
    }
}
```

Use [`process_output`](./process_output.md) to retrieve output and check whether the process has finished. Use [`process_stop`](./process_stop.md) to terminate it, and [`process_list`](./process_list.md) to see all currently running background processes.

## Output Limits

| Limit | Value | Behavior on Exceed |
|---|---|---|
| Max lines (stdout or stderr) | 1000 lines | Truncated; `truncated: true` in result |
| Max bytes (stdout or stderr) | 50 KB | Truncated; `truncated: true` in result |

When truncated, a message is appended: `... (further output truncated due to line/byte limit)`.

## Error Cases

An Error Response is returned (instead of a result) only for hard failures:

| Condition | Error message |
|---|---|
| `command` is empty string (`""`) | `invalid params: command cannot be empty` |
| Command cannot be launched (e.g., `bash` not found) | `command execution failed: ...` |
| Invalid parameters | `invalid params: <details>` |

## Examples

### Run tests

```json
{"id":"1","method":"tool","tool":"execute_bash","input":{"command":"go test ./...","task_timeout":60}}
```

### Build the project

```json
{"id":"2","method":"tool","tool":"execute_bash","input":{"command":"./script/build.sh","description":"Build release binary"}}
```

### Query git log

```json
{"id":"3","method":"tool","tool":"execute_bash","input":{"command":"git log --oneline -10"}}
```

### Start a server immediately in the background

```json
{"id":"4","method":"tool","tool":"execute_bash","input":{"command":"./gtsp --mode websocket --port 9001","run_in_background":true,"description":"Start test server"}}
```

Returns `process_id` immediately without waiting. Use `process_output` to confirm it started successfully.

### Run a command with kill-on-timeout

```json
{"id":"5","method":"tool","tool":"execute_bash","input":{"command":"make build","task_timeout":120,"timeout_action":"kill","description":"Build project, kill if stuck"}}
```

Waits up to 120 seconds. If the build is still running, terminates the entire process group and returns partial output.
