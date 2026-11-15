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
| `command` | `string` | — | **Required.** The shell command to execute. The interpreter is platform-dependent: `bash -c` on Unix, busybox `ash` (POSIX sh, **not** bash) on Windows. |
| `task_timeout` | `integer` | `180` | Timeout in seconds. `0` or omitted means use the default (180 s) — it does **not** mean "run in background immediately". Ignored when `run_in_background: true`. |
| `timeout_action` | `string` | `"background"` | Action when timeout expires: `"background"` promotes to background; `"kill"` terminates the process group and returns partial output. |
| `run_in_background` | `boolean` | `false` | Start immediately in background and return a `process_id` without waiting. This is **not** equivalent to `task_timeout: 0` — that falls back to the 180 s default and only promotes on timeout. |
| `description` | `string` | — | Optional human-readable description of the command's purpose (for logging). |

#### Parameter combinations

| Scenario | Parameters | Behavior |
|---|---|---|
| Default (no params) | _(none)_ | Waits up to 180 s. Returns result if done; otherwise promotes to background and returns `process_id`. |
| Custom timeout, background on expiry | `task_timeout: 120` | Waits up to 120 s. Returns result if done; otherwise promotes to background. |
| Timeout with kill | `task_timeout: 60, timeout_action: "kill"` | Waits up to 60 s. Returns result if done; otherwise kills the process group and returns partial output. |
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
| `success` | `boolean` | `true` when `exit_code == 0`. |
| `stdout` | `string` | Standard output of the command, truncated Head+Tail (see limits below). |
| `stderr` | `string` | Standard error of the command, truncated Head+Tail. |
| `exit_code` | `integer` | The command's exit code. `0` indicates success. Non-zero indicates failure. |
| `truncated` | `boolean` | `true` if `stdout` or `stderr` was clipped, **or** if either stream's buffer overflowed (`stdout_overflow` / `stderr_overflow`). |
| `stdout_overflow` / `stderr_overflow` | `boolean` | Present (omitempty) when that stream's 512 KB ring buffer had to evict its oldest bytes for good. |
| `stdout_offset` / `stderr_offset` | `integer` | Present (omitempty) on **handover** results only — the cursors to pass to `process_output` for reading what comes after this snapshot. |

A non-zero `exit_code` does **not** produce an Error Response — the result is still returned with `type: "result"`. This allows the caller to inspect partial output and stderr.

### Long-running command (still running after `task_timeout`)

```json
{
    "id": "1",
    "type": "result",
    "result": {
        "process_id": "proc_1",
        "status": "running",
        "stdout": "... (61440 bytes omitted) ...\nstep 412/500\n",
        "stderr": "",
        "truncated": true,
        "stdout_offset": 262144,
        "stderr_offset": 0,
        "stdout_overflow": true
    }
}
```

| Field | Type | Description |
|---|---|---|
| `process_id` | `string` | Opaque handle. Pass to [`process_output`](./process_output.md) or [`process_stop`](./process_stop.md). |
| `status` | `"running"` | Indicates the process is still running in the background. |
| `stdout` | `string` | Output collected during the `task_timeout` window, **truncated Head+Tail** — never the raw buffer (that could be hundreds of KB). |
| `stderr` | `string` | Same for stderr. |
| `truncated` | `boolean` | `true` if either stream was clipped or overflowed. |
| `stdout_offset` / `stderr_offset` | `integer` | Continuation cursors for `process_output` (snapshot `totalBytes`). Pass them back to read only newer output; pass `0` to re-read what the buffer still retains. |
| `stdout_overflow` / `stderr_overflow` | `boolean` | Present (omitempty) when that stream's ring buffer evicted its oldest bytes. |

Per `spec/Protocol.md`, the server emits a `process/pending` event at the same time:

> **Not implemented in the current server.** `grep -rn 'process/pending' src/` returns nothing — no Go code ever emitted this event (verified on `dev` @ `efe6b3a` and on the P1 branch). Do **not** block on it; poll with [`process_output`](./process_output.md) instead. The event shape below is kept only because `spec/Protocol.md:366/379/639` still documents it.

```json
{
    "type": "event",
    "event": "process/pending",
    "data": {
        "process_id": "proc_1",
        "elapsed": 10,
        "stdout": "Starting server...\n",
        "stderr": ""
    }
}
```

### Command exited but left descendants alive (handover)

When the shell exits but descendant processes (`&`, daemons) are still running, they are **not** killed. The result contains `process_id`, `leftover_processes` and a `message`, and the offsets let you keep reading their output:

```json
{
    "id": "5",
    "type": "result",
    "result": {
        "success": true,
        "stdout": "child=80332\n",
        "stderr": "",
        "exit_code": 0,
        "truncated": false,
        "process_id": "proc_7",
        "stdout_offset": 12,
        "stderr_offset": 0,
        "leftover_processes": {
            "alive": 1,
            "tree": {
                "pid": 80331,
                "ppid": 80072,
                "command": "bash -c 'sleep 57 & echo child=$!'",
                "alive": false,
                "children": [
                    {"pid": 80332, "ppid": 1, "command": "sleep 57", "alive": true, "children": []}
                ]
            }
        },
        "message": "command exited but descendant process(es) are still running; use process_stop to terminate, or process_output to poll; unclaimed processes persist until server shutdown"
    }
}
```

| Field | Type | Description |
|---|---|---|
| `process_id` | `string` | Handle for `process_output` / `process_stop`. |
| `leftover_processes` | `object` | `{alive, tree: {pid, ppid, command, alive, children}}`. Descendants that already re-parented to PID 1 are still listed while they remain in the command's process group. |
| `stdout_offset` / `stderr_offset` | `integer` | Cursors for continuing to read this process's output via `process_output`. |

> **Known limitation:** leftover detection follows the command's **process group**. A descendant that creates its own process group or session (e.g. `timeout` without `--foreground`, `setsid`, most daemons) leaves the group and will not be reported. See `gtsp/doc/process-output-optimization.md` and the probe report for details.

Use [`process_output`](./process_output.md) to retrieve output and check whether the process has finished. Use [`process_stop`](./process_stop.md) to terminate it, and [`process_list`](./process_list.md) to see all currently running background processes.

## Output Limits

Per stream: at most **1000 lines and 50 KB** per call, with a 256-byte marker budget reserved so the total never exceeds the byte limit. The buffer itself is a 512 KB ring per stream (`DefaultProcRingBytes`), so older bytes may be evicted before you read them.

### Head+Tail truncation (synchronous results and timeout promotion)

Truncation keeps **both ends**: the head keeps context (up to the first 200 lines / 20 % of the budget) and the tail keeps the failure/result (up to the last 800 lines / 80 %). The dropped middle is replaced by a single marker.

**All marker forms `execute_bash` can emit** — match on the fragments, not on the whole string:

| Form | When |
|---|---|
| `... (M bytes omitted) ...` | Head+Tail dropped `M` bytes from the middle; buffer did **not** overflow |
| `... (N bytes earlier output evicted by buffer) ...` | Short stream that fits the byte/line budget, **but** the ring buffer already overwrote older bytes — fast path only (notice prepended to the whole retained range). `N` = total evicted before the retained range (ring overwrite + up to 3 B UTF-8 alignment), a **real byte count** |
| `... (N bytes earlier output evicted by buffer; M bytes omitted) ...` | **Both at once** — the ring overflowed **and** Head+Tail dropped a middle chunk. Common for long-running chatty commands. `N` = total evicted (real count), `M` = omitted middle |

```
... (53248 bytes omitted) ...
... (65536 bytes earlier output evicted by buffer) ...
... (35712 bytes earlier output evicted by buffer; 516288 bytes omitted) ...
```

> **`N` is a real byte count, not an alignment artefact.** It is the total bytes permanently unavailable before the retained range: the ring overwrite **plus** up to 3 bytes skipped for UTF-8 alignment (`api/process.go:303`, `EvictedBytes = alignCeil(start)`). A long-running command therefore shows a large `N` (e.g. `560000 − 512 KB = 35712`), not `0`. Since the alignment skip can only happen at a real page boundary (`offset > 0`), `N > 0` now happens **exactly when** the ring overflowed — `*_overflow` and `N` can no longer disagree.

### `stdout` / `stderr` are text, not a binary channel

Both fields are JSON strings, so they can only carry text: when the result is serialized, **every invalid UTF-8 byte becomes U+FFFD** (`EF BF BD`) — one bad byte turns into three. Two consequences:

- **Byte-exact round-tripping only holds for valid UTF-8 output.** For binary/GBK/partial-rune output the corruption already happens in the JSON layer, before any of the buffer logic.
- **Never use `stdout` to carry binary data.** Write it to a file and read it back with `read_file`, or `base64`-encode it.

What the buffer no longer does is drop bytes *before* that serialization: the **stream head is never treated as mid-character**, so even when the first byte is a UTF-8 continuation byte (`0x80`–`0xBF`) all leading bytes are returned (see `TestInvalidUTF8StreamHeadIsNotDropped`). Pagination skips for alignment still apply at genuine mid-stream page boundaries, where the skipped bytes are re-readable on the next page only if the writer's stream was valid UTF-8 there.

`truncated: true` therefore means "text was clipped **or** the buffer evicted older bytes" — it never means the tail was dropped: the tail is always preserved.

| Limit | Value | Behavior on exceed |
|---|---|---|
| Max lines (stdout or stderr) | 1000 lines | Head+Tail clipped; `truncated: true` |
| Max bytes (stdout or stderr) | 50 KB | Head+Tail clipped; `truncated: true` |
| Buffer per stream | 512 KB ring | Oldest bytes evicted; `stdout_overflow` / `stderr_overflow: true` |

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

Returns `process_id` immediately without waiting (`stdout` / `stderr` are empty, `stdout_offset` / `stderr_offset` are `0`). Use `process_output` to confirm it started successfully.

### Run a command with kill-on-timeout

```json
{"id":"5","method":"tool","tool":"execute_bash","input":{"command":"make build","task_timeout":120,"timeout_action":"kill","description":"Build project, kill if stuck"}}
```

Waits up to 120 seconds. If the build is still running, terminates the entire process group and returns partial output.

### Avoid blowing the context window on a huge build log

```json
{"id":"6","method":"tool","tool":"execute_bash","input":{"command":"cargo build 2>&1","task_timeout":30}}
```

If the command is promoted to background, the returned `stdout` is already Head+Tail truncated and `stdout_offset` points at the snapshot end. Then poll incrementally:

```json
{"id":"7","method":"tool","tool":"process_output","input":{"process_id":"proc_1","block":false,"stdout_offset":262144}}
```
