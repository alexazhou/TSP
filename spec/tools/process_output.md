# process_output

Retrieve output from a background process, incrementally or from the tail, and check whether it has finished.

## When to Use

Use `process_output` after receiving a `process_id` from `execute_bash`. Three ways to use it:

- **Poll for completion / new output**: `block: false` + the `stdout_offset` / `stderr_offset` you got last time.
- **Wait for the process (and its surviving descendants) to finish**: `block: true` with a `timeout`.
- **Just look at the latest result**: no output parameters — the default mode returns the **tail** of each stream.

**Where `process_id` comes from** (three sources, all from `execute_bash`):

1. `run_in_background: true` — started in the background immediately;
2. `task_timeout` expiry with `timeout_action: "background"` (the default) — promoted to background;
3. **handover**: a synchronous command whose main shell exited but whose descendant processes are still alive. The result then carries `process_id`, `leftover_processes` and a `message`.

A command that completes synchronously **without** surviving descendants produces no `process_id`.

**Lifecycle and scope:**

- Process IDs are opaque strings (shape `proc_1`; never parse them) and are only valid while the TSP server session is active. They are lost on server restart.
- `GlobalProcessRegistry.Get` does **not** validate the session, so any client that knows an ID can read it.
- A reaper runs every 5 minutes and removes entries whose **main shell exited more than 30 minutes ago** *and* that currently have **no surviving descendants** (`gtsp/src/api/process.go:674-704`). The 30-minute clock starts when the **main shell** exits (`doneAt`, set at `cmd.Wait()` return, `:597`) — **not** when the last descendant dies. Consequence: an entry whose descendants outlive the main shell by 40 minutes is removed on the **very next tick** after they exit, with no further wait. Entries with live descendants are never auto-killed or auto-removed.
- When a WebSocket session closes, `KillSession` kills and removes that session's processes.

## Request

```json
{
    "id": "2",
    "method": "tool",
    "tool": "process_output",
    "input": {
        "process_id": "proc_1",
        "block": true,
        "timeout": 30000
    }
}
```

### Parameters

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `process_id` | `string` | **Yes** | — | Opaque handle returned by `execute_bash` (background start, timeout promotion, or sync handover). |
| `block` | `boolean` | No | `true` | If `true`, wait until the process exits **and its surviving descendants exit** (polled at 100 ms intervals) or the deadline expires. If `false`, return the current snapshot immediately; `timeout` is ignored. |
| `timeout` | `integer` | No | `30000` | Maximum milliseconds to wait when `block: true`. Values `> 60000` are silently clamped to `60000`; values `<= 0` (including `0`) return immediately. This is an **absolute** deadline covering both the main process and the descendant wait — it is not restarted between stages. **Note:** the actual effective wait time is also limited by the client's transport request timeout (typically 30–65 s); if you need to wait longer, use `block: false` and poll repeatedly. |
| `stdout_offset` | `integer` | No | `null` (tail mode) | Explicit incremental read start for stdout. `minimum: 0`. Omit it to get the default tail mode. |
| `stderr_offset` | `integer` | No | `null` (tail mode) | Explicit incremental read start for stderr. `minimum: 0`. |
| `tail_lines` | `integer` | No | `null` | Return at most the last N lines of **both** streams. `minimum: 1`, `maximum: 1000`; values `> 1000` are silently clamped to `1000`. **Mutually exclusive** with `stdout_offset` / `stderr_offset`. |
| `include_output` | `boolean` | No | `true` | When `false`, no output is returned at all (zero-copy status read) and the cursors advance to the latest position. Use `tail_lines`, or pass an earlier `*_offset`, afterwards to read the results. |

## Response

```json
{
    "id": "2",
    "type": "result",
    "result": {
        "process_id": "proc_1",
        "stdout": "... (53248 bytes earlier output omitted/evicted) ...\nBUILD SUCCESS\n",
        "stderr": "",
        "running": false,
        "exit_code": 0,
        "truncated": true,
        "stdout_offset": 1048576,
        "stderr_offset": 0
    }
}
```

Handover / leftover example (main shell exited, a descendant is still alive):

```json
{
    "id": "3",
    "type": "result",
    "result": {
        "process_id": "proc_7",
        "stdout": "handed_over\n",
        "stderr": "",
        "running": true,
        "exit_code": null,
        "truncated": false,
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
        }
    }
}
```

### Result Fields

| Field | Type | Description |
|---|---|---|
| `process_id` | `string` | Echoes the input `process_id`. |
| `stdout` / `stderr` | `string` | The truncated text of that stream (see [Output Limits](#output-limits)). May contain markers, so **`len(stdout)` is not the number of bytes it represents** — never derive a cursor from it. |
| `running` | `boolean` | `true` while the main process is alive **or** any surviving descendant is alive. |
| `exit_code` | `integer` \| `null` | Exit code, only when `running == false`. **`null`** while the main process is still running **and also** when the main process exited but descendants are still alive. |
| `truncated` | `boolean` | Stream-level completeness flag: `clipped \|\| stdout_overflow \|\| stderr_overflow`. |
| `stdout_offset` / `stderr_offset` | `integer` | Continuation cursors: bytes-absolute positions to pass back as `stdout_offset` / `stderr_offset` next call. Always present. |
| `stdout_overflow` / `stderr_overflow` | `boolean` | Present (omitempty) when that stream's ring buffer had to evict its oldest bytes; those bytes are gone for good. In a future Phase 2 file-backed buffer (`false` always) nothing is evicted. |
| `stdout_offset_expired` / `stderr_offset_expired` | `boolean` | Present (omitempty) when the `offset` you passed was already overwritten. **Even then, the returned `*_offset` is a valid new cursor — continue from it, do not reset to 0.** |
| `leftover_processes` | `object` | Present (omitempty) when the main process exited but descendants are alive: `{alive, tree: {pid, ppid, command, alive, children}}`. |

## Output Limits

The buffer per stream is a 512 KB ring (`DefaultProcRingBytes`); the truncation limit per call is **1000 lines / 50 KB per stream**, with a 256-byte marker budget reserved so the total never exceeds the byte limit.

Three modes:

| Mode | Trigger | What you get |
|---|---|---|
| **Tail** (default) | no `stdout_offset` / `stderr_offset` / `tail_lines` | The last ≤ 1000 lines and ≤ 50 KB of each stream — i.e. the latest result, not a repeated head. To inspect the beginning, pass `stdout_offset: 0`. |
| **Tail with N lines** | `tail_lines: N` | The last ≤ N lines (N clamped to 1000) and ≤ 50 KB of each stream. |
| **Prefix** (sequential pagination) | explicit `stdout_offset` / `stderr_offset` | Up to ≤ 1000 lines / ≤ 50 KB starting at that byte offset, plus a trailing marker when more bytes remain. |

**Prefix mode contracts** (read these before writing pagination code):

1. **Page boundaries may fall inside a line.** Do not re-align to lines yourself; just pass the returned offset back.
2. **Page size is not exact — the deviation has no fixed sign.** The read interface only guarantees that the **start** is rune-aligned. To avoid cutting a multi-byte character at the end, the page boundary is extended with `utf8Ceil` (up to +3 bytes) and then pulled back by `trimPartialRuneSuffix` (a `floor`); the resulting length therefore differs from the window by **at most 3 bytes in absolute value, in either direction** (a pure floor is not viable: with a 1-byte window and a 3-byte rune it would floor to an empty page and never advance). Any check like `len(page) == window` is wrong. Implementation anchor: `gtsp/src/api/process.go:272-290` (overshoot then re-read ≤3 bytes) and `:208` (`trimPartialRuneSuffix`).
   - **Two layers, two contracts — do not mix them.** *Buffer layer* (this section): `|len(page) - window| <= 3`, sign not guaranteed. *Tool layer*: only the end-to-end guarantee that the final `len(stdout) <= 50 KB` per stream (asserted by `gtsp/test/unit/process_output_protocol_test.go:334`). Do **not** turn the buffer-level ±3 window arithmetic into a tool-level contract (`window + 3` style assertions are wrong).
3. **Concatenation is byte-exact.** Feeding each returned offset back and concatenating the page bodies reproduces the retained stream byte-for-byte (before the buffer overflow point); after overflow, the concatenation equals the tail of the stream.

Markers you may see inside `stdout` / `stderr`:

| Marker | Meaning |
|---|---|
| `... (N bytes omitted) ...` | Head+Tail truncation dropped N bytes in the middle (only produced by `execute_bash`). |
| `... (N bytes earlier output evicted by buffer) ...` | Fast path: the whole retained range fits the budget, but the ring buffer had already overwritten older bytes. **N is the byte count skipped at the start of the retained range for UTF-8 alignment** (`api/process.go:303`), so it is **usually `0`** (only **1–3** when the retained range happens to start mid-character) — treat `*_overflow: true` as the authoritative "nothing older is retained" signal, not N. |
| `... (N bytes earlier output evicted by buffer; M bytes omitted) ...` | **Both at once**: buffer overflowed **and** Head+Tail dropped a middle chunk. Same caveat on `N` (usually `0`); `M` and `*_overflow` carry the real information. |
| `... (N bytes earlier output omitted/evicted) ...` | Tail mode: N earlier bytes are not included (evicted and/or beyond the line/byte window). |
| `... (N bytes evicted by buffer before this point) ...` | Your `stdout_offset` was already overwritten (`*_offset_expired: true`). |
| `... (N bytes skipped for UTF-8 alignment) ...` | The requested offset was mid-character; it was advanced to the next character boundary. |
| `... (N bytes remaining in buffer) ...` | Prefix mode: N bytes remain after this page; read again with the returned offset. |

## Parameter Combination Matrix

| Combination | `stdout` / `stderr` behavior | `*_offset` returned | `truncated` | `*_offset_expired` | Validation |
|---|---|---|---|---|---|
| none (default tail mode) | tail mode for each stream (≤ 1000 lines, ≤ 50 KB) | `totalBytes` of each stream | `clipped \|\| *_overflow` | omitted (`false`) | `process_id == ""` → `protocol/invalid_params` |
| only `stdout_offset` | stdout = prefix pagination; stderr = default tail mode | `stdout_offset = min(nextBaseOffset + deliveredBytes, totalBytes)`; echoed unchanged when `offset >= totalBytes`; `stderr_offset = stderrTotal` | `clipped \|\| *_overflow` | stdout: `offset < startOffset`; stderr: omitted | `offset < 0` → `protocol/invalid_params` |
| `tail_lines` | last N lines of each stream (split on `\n` only) | `totalBytes` of each stream | `clipped \|\| *_overflow` | omitted (`false`) | `tail_lines <= 0` → `protocol/invalid_params`; `> 1000` clamped |
| `tail_lines` + any `*_offset` | — | — | — | — | `protocol/invalid_params` (mutually exclusive) |
| `include_output: false` (may be combined with a legal `offset` or `tail_lines`) | `""` / `""` — truncation and expiry are skipped entirely | `max(passedOffset, totalBytes)` per stream | `*_overflow` only | always omitted (`false`) | parameter range and exclusivity checks still apply |

## Error Cases

| Condition | Error code |
|---|---|
| Unknown `process_id` | `resource/not_found` |
| `process_id` missing or empty | `protocol/invalid_params` |
| `stdout_offset` / `stderr_offset` negative | `protocol/invalid_params` |
| `tail_lines <= 0` | `protocol/invalid_params` |
| `tail_lines` combined with `stdout_offset` / `stderr_offset` | `protocol/invalid_params` |
| Wrong parameter type (e.g. `stdout_offset: "abc"`) | `protocol/invalid_params` |

## Examples

### Wait for process to finish (up to 60 seconds)

```json
{"id":"2","method":"tool","tool":"process_output","input":{"process_id":"proc_1","block":true,"timeout":60000}}
```

### Non-blocking poll — check current output without waiting

```json
{"id":"3","method":"tool","tool":"process_output","input":{"process_id":"proc_1","block":false}}
```

### Incremental polling — read only what is new

```json
{"id":"4","method":"tool","tool":"process_output","input":{"process_id":"proc_1","block":false,"stdout_offset":40960,"stderr_offset":0}}
```

Then pass back the returned `stdout_offset` / `stderr_offset`. This is the recommended pattern for long builds: each call returns only the new bytes.

### Wait-loop with no output (zero-copy)

```json
{"id":"5","method":"tool","tool":"process_output","input":{"process_id":"proc_1","block":true,"timeout":30000,"include_output":false}}
```

Only the status and cursors are returned; nothing is copied out of the buffer.

### See the last 50 lines after the process finished

```json
{"id":"6","method":"tool","tool":"process_output","input":{"process_id":"proc_1","block":false,"tail_lines":50}}
```

### Re-read what the buffer still retains from the beginning

```json
{"id":"7","method":"tool","tool":"process_output","input":{"process_id":"proc_1","block":false,"stdout_offset":0}}
```

If the beginning was already evicted you get `stdout_offset_expired: true` and a `... (N bytes evicted by buffer before this point) ...` marker; continue from the returned offset.
