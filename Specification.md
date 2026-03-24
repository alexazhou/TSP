# TSP Specification

**Version:** 0.3  |  **Status:** Draft

---

## Table of Contents

1. [Overview](#1-overview)
2. [Base Protocol](#2-base-protocol)
   - 2.1 [Transport Layers](#21-transport-layers)
   - 2.2 [Authentication (WebSocket)](#22-authentication-websocket)
   - 2.3 [Message Framing](#23-message-framing)
   - 2.4 [Request](#24-request)
   - 2.5 [Response](#25-response)
   - 2.6 [Error Response](#26-error-response)
   - 2.7 [Event](#27-event)
3. [Lifecycle](#3-lifecycle)
   - 3.1 [Initialize](#31-initialize)
   - 3.2 [Sandbox Config](#32-sandbox-config) *(optional)*
   - 3.3 [Tool](#33-tool)
   - 3.4 [Shutdown](#34-shutdown)
4. [Tool API](#4-tool-api)
   - 4.1 [Tool Definition](#41-tool-definition)
   - 4.2 [Tool Invocation](#42-tool-invocation)
   - 4.3 [Concurrency](#43-concurrency)
   - 4.4 [Long-running Processes](#44-long-running-processes)
5. [Security Model](#5-security-model)
   - 5.1 [Sandbox Features](#51-sandbox-features)
   - 5.2 [Workdir & Path Validation](#52-workdir--path-validation)
6. [Error Codes](#6-error-codes)
7. [Appendix: Type Definitions](#7-appendix-type-definitions)

---

## 1. Overview

TSP is a **request/response** protocol for executing system tools. A **client** (typically an AI agent or its host process) sends JSON requests to a **server**, which executes the requested tool and returns a JSON result.

The design goals are:

- **Simplicity** — A single JSON object per message, minimal handshake.
- **Transport agnosticism** — Same message format works over stdio and WebSocket.
- **Concurrency** — Multiple requests can be in-flight simultaneously; responses are correlated by `id`.
- **LLM-native** — Tool schemas are expressed in the format expected by major LLM APIs (Anthropic, OpenAI), and delivered at connection time via `initialize`.
- **Safety** — A workdir sandbox prevents path traversal; clients can further restrict the tool set via capability filtering in `initialize`.

---

## 2. Base Protocol

### 2.1 Transport Layers

TSP supports two transports. The message format is identical on both.

#### stdio (default)

The client launches the server as a child process and communicates via its standard streams:

| Stream | Direction | Content |
|---|---|---|
| `stdin` | Client → Server | Newline-delimited JSON requests |
| `stdout` | Server → Client | Newline-delimited JSON responses and events |
| `stderr` | Server → (log file) | Human-readable debug logs (not part of the protocol) |

Each message is a single line of JSON terminated by `\n`. Clients MUST NOT send multi-line JSON. Servers MUST write each response as a single line.

#### WebSocket

The server starts an HTTP server and upgrades connections to WebSocket.

- **Endpoint:** `ws://<host>:<port>/tsp`
- **Message type:** One JSON object per text frame.
- Multiple clients may connect concurrently; each connection is independent.

### 2.2 Authentication (WebSocket)

Authentication applies to the WebSocket transport only. stdio connections run as a child process under the parent's OS user and require no additional authentication.

Two methods are supported:

#### Method 1 — HTTP Authorization Header

Pass a Bearer token in the `Authorization` header during the WebSocket upgrade handshake. This is the preferred method for server-to-server or native client scenarios.

```
GET /tsp HTTP/1.1
Upgrade: websocket
Authorization: Bearer <token>
```

The server SHOULD reject the upgrade with HTTP 401 if the token is missing or invalid.

#### Method 2 — Token in `initialize` Input

For environments that cannot set custom HTTP headers (e.g. browser `WebSocket` API), pass the token in the `initialize` request's `input.auth.token` field.

```json
{
    "id": "init-1",
    "method": "initialize",
    "input": {
        "protocolVersion": "0.3",
        "auth": {"token": "<token>"},
        "clientInfo": {"name": "browser-agent"}
    }
}
```

The server MUST validate the token before processing the `initialize` response. If the token is invalid or missing, the server MUST return an error and close the connection.

> When both an `Authorization` header and `auth.token` are provided, the header takes precedence.

#### Authentication Failure

If authentication fails at the HTTP upgrade stage (Method 1), the server MUST reject with HTTP 401 and not establish the WebSocket connection.

If authentication fails at the `initialize` stage (Method 2), the server MUST:
1. Return an error response with `code: "security/unauthorized"`.
2. Close the WebSocket connection immediately after sending the response.

Any tool request that arrives before a successful `initialize` is also rejected with `security/unauthorized` and the connection is closed.

If a WebSocket connection is established without a valid `Authorization` header (i.e. Method 1 was not used) and `initialize` is not completed within the authentication timeout (default: 10 seconds), the server MUST close the connection without sending an error response. Implementations MAY make this timeout configurable.

### 2.3 Message Framing

| Transport | Framing |
|---|---|
| stdio | One JSON object per line (`\n`-terminated) |
| WebSocket | One JSON object per text frame |

### 2.4 Request

A Request is sent from client to server. The `method` field discriminates the message type.

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Unique identifier. Echoed back in the response for correlation. |
| `method` | string | Yes | `"initialize"`, `"sandbox"` *(optional)*, `"tool"`, or `"shutdown"`. |
| `tool` | string | When `method="tool"` | Name of the tool to invoke (e.g. `"read_file"`). |
| `input` | object | Yes | Request-specific parameters. Structure depends on `method` and `tool`. |

```json
{"id":"req-1","method":"tool","tool":"read_file","input":{"file_path":"src/main.go"}}
```

### 2.5 Response

Sent by the server after successfully executing a request.

| Field | Type | Description |
|---|---|---|
| `id` | string | Echoes the `id` from the originating Request. |
| `type` | `"result"` | Always `"result"` for a successful response. |
| `result` | object | The tool's output. Structure depends on the tool. |

```json
{"id":"req-1","type":"result","result":{"content":"package main\n...","total_lines":171}}
```

### 2.6 Error Response

Sent when the server cannot execute the request.

| Field | Type | Description |
|---|---|---|
| `id` | string \| null | Echoes the request `id`. `null` if the request could not be parsed. |
| `type` | `"error"` | Always `"error"`. |
| `code` | ErrorCode | Machine-readable error code. See [§6 Error Codes](#6-error-codes). |
| `error` | string \| object | Human-readable description, or structured detail for complex errors. |

```json
{"id":"req-1","type":"error","code":"resource/not_found","error":"file not found: src/missing.go"}
```

When JSON parsing fails (`protocol/parse_error`), the `id` cannot be extracted and is set to `null`. Clients MUST identify these responses by `code` rather than `id`.

### 2.7 Event

An unsolicited message from server to client. Events have no `id` and require no reply.

| Field | Type | Description |
|---|---|---|
| `type` | `"event"` | Always `"event"`. |
| `event` | string | Event name. Built-in events use `"process/"` prefix; extension events use `"x/"` prefix. |
| `data` | object | Event-specific payload. |

---

## 3. Lifecycle

### 3.1 Initialize

The client MUST send an `initialize` request before invoking any tools.

The `initialize` request serves three purposes:
1. **Protocol version negotiation** — client and server agree on a protocol version.
2. **Client identification** — client declares its identity for logging and auditing.
3. **Capability negotiation** — client requests a filtered subset of tools; server returns their schemas and declares optional capabilities (e.g. sandbox) ready for use.

**Request `input` fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `protocolVersion` | string | Yes | Protocol version the client supports, e.g. `"0.3"`. |
| `clientInfo.name` | string | No | Client name for logging, e.g. `"my-agent"`. |
| `clientInfo.version` | string | No | Client version string. |
| `auth.token` | string | No | Bearer token for WebSocket authentication when HTTP headers cannot be set. See §2.2. |
| `capabilities.tools.include` | string[] | No | Whitelist: only return these tools. Omit for all tools. |
| `capabilities.tools.exclude` | string[] | No | Blacklist: exclude these tools. Applied after `include`. |

**Response `result` fields:**

| Field | Type | Description |
|---|---|---|
| `protocolVersion` | string | Protocol version the server will use for this session. |
| `serverInfo.name` | string | Server implementation name, e.g. `"gTSP"`. |
| `serverInfo.version` | string | Server version, e.g. `"v0.3.0"`. |
| `capabilities.tools` | ToolDefinition[] | Tool schemas after applying the capability filter. Ready to pass directly to an LLM's tool registration API. |
| `capabilities.sandbox` | string[] \| absent | List of sandbox features supported by this server, e.g. `["read","write"]`. Absent if the server does not support the `sandbox` method. See §3.2. |
| `workdir` | string | Absolute path of the current working directory. |

```
Client                                        Server
  │────────────── initialize ──────────────────►│
  │◄── result {protocolVersion, tools, ...} ────│
```

**Version negotiation:**

| Situation | Server behavior |
|---|---|
| Versions match | Normal response |
| Client minor version lower | Normal response; server uses higher version |
| Major version incompatible | Error response (`protocol/parse_error`) |

**Backward compatibility:** Servers SHOULD process tool requests that arrive before `initialize` as if a default `initialize` had been performed (all tools, no filter).

### 3.2 Sandbox Config *(Optional)*

The `sandbox` method configures per-session access restrictions for file I/O and network. It may be sent at any point after `initialize` and before `shutdown`. Sending multiple `sandbox` requests replaces the previous configuration.

Clients MUST check `capabilities.sandbox` in the `initialize` response before sending a `sandbox` request. If absent, the server does not support this method and any `sandbox` request will return `protocol/tool_not_found`. For supported features and their semantics, see [§5.1 Sandbox Features](#51-sandbox-features).

**Request `input`:**

A flat object where each key is a feature name from `capabilities.sandbox` and the value is feature-specific configuration. Unknown or unsupported features are rejected with `protocol/invalid_params`.

```json
{
    "read": [
        {"action": "deny",  "path": "/project/secrets"},
        {"action": "allow", "path": "/project"}
    ],
    "write": [
        {"action": "allow", "path": "/project/out"}
    ],
    "network": false
}
```

**Response `result`:** An empty object `{}` on success.

```
Client                                                         Server
  │────────────────────────── initialize ───────────────────────►│
  │◄────── result {capabilities.sandbox: ["read","write"]} ──────│
  │                                                              │
  │──────────── sandbox {"read":[{action,path},...]} ───────────►│
  │◄────────────────────── result {} ────────────────────────────│
  │                                                              │
  │───────────────────── tool request ─── ──────────────────────►│  (sandbox in effect)
```

### 3.3 Tool

After `initialize` (and optionally `sandbox`), the client may invoke tools at any time until `shutdown`. Tool requests are independent and may be sent concurrently; responses may arrive in a different order and MUST be correlated by `id`.

To invoke a tool, send `method: "tool"` with the tool name and input:

```
Client                                          Server
  │── {"method":"tool","tool":"read_file",...} ──►│
  │◄── {"type":"result","result":{...}} ──────────│
```

The server MUST only execute tools included in the `initialize` capability negotiation. Requests for excluded or unknown tools return `protocol/tool_not_found`.

For the full tool schema reference, concurrency model, and long-running process handling, see [§4 Tool API](#4-tool-api).

### 3.4 Shutdown

TSP uses a two-phase shutdown to allow the server to clean up resources (background processes, temporary files, log buffers).

**Phase 1 — shutdown request:** The client sends `{"id":"...","method":"shutdown","input":{}}`. The server MUST:
1. Stop accepting new requests (return `protocol/shutting_down` for any that arrive).
2. Wait for all in-flight requests to complete.
3. Terminate all background processes created by long-running `execute_bash` calls.
4. Clean up resources.
5. Return an empty result `{}`.

**Phase 2 — transport close:** The client closes the transport (EOF on stdio, or closes the WebSocket). This signals the server to exit.

```
Client                     Server
  │────────── shutdown ───────►│
  │                            │  ① stop accepting new requests
  │                            │  ② await in-flight completions
  │                            │  ③ cleanup resources
  │◄──────── result {} ────────│
  │───── EOF / disconnect ────►│
  │                            │── exit
```

If the transport closes without a prior `shutdown` (e.g. client crash), the server SHOULD perform best-effort cleanup. In-flight requests may be interrupted.

---

## 4. Tool API

### 4.1 Tool Definition

A Tool Definition describes a single tool. It is delivered to the client in the `initialize` response and can be passed directly to an LLM's tool registration API without transformation.

| Field | Type | Description |
|---|---|---|
| `name` | string | Tool name, used as the `tool` field in a ToolRequest. |
| `description` | string | LLM-readable description of what the tool does and when to use it. |
| `input_schema` | JSONSchema | JSON Schema (draft 2020-12) for the tool's `input`. Compatible with Anthropic Tool Use API and OpenAI function calling. |

### 4.2 Tool Invocation

To invoke a tool, the client sends a Request with `method: "tool"`. The server MUST only execute tools included in the `initialize` capability negotiation; requests for excluded or unknown tools return `protocol/tool_not_found`.

```
Client                                          Server
  │── {"method":"tool","tool":"read_file",...} ──►│
  │◄── {"type":"result","result":{...}} ──────────│
```

The full input and result schema for each built-in tool is in the [Built-in Tools Reference](./tools/README.md).

### 4.3 Concurrency

Servers MUST support concurrent request execution:

- Requests are processed as they arrive; responses may arrive in a **different order**.
- Clients MUST use the `id` field to correlate responses with requests.

```
Client                Server
  │── req id="1" ──────►│
  │── req id="2" ──────►│  (both executing concurrently)
  │◄── resp id="2" ─────│  (id="2" finishes first)
  │◄── resp id="1" ─────│
```

Clients that cannot handle out-of-order responses SHOULD use a single in-flight request at a time.

### 4.4 Long-running Processes

By default, `execute_bash` blocks until the command exits. A process becomes a **background process** in two ways:

- **Explicit:** Set `run_in_background: true` to return `process_id` immediately without waiting.
- **Automatic:** When a command does not exit within `task_timeout` seconds (default: 10), the server promotes it automatically, returns a result with `process_id`, and emits a `process/pending` event.

The caller uses `process_output` to retrieve output and check completion status, and `process_stop` to terminate the process.

**Background process result fields:**

| Field | Type | Description |
|---|---|---|
| `process_id` | string | Opaque handle. Pass to `process_output` or `process_stop`. |
| `status` | `"running"` | Indicates the process is still running. |
| `stdout` | string | Output collected so far (empty when `run_in_background: true`). |
| `stderr` | string | Stderr collected so far (empty when `run_in_background: true`). |

**`process/pending` event `data` fields:**

| Field | Type | Description |
|---|---|---|
| `process_id` | string | Matches the `process_id` in the result. |
| `elapsed` | integer | Seconds elapsed since the command started. |
| `stdout` | string | Output collected so far. |
| `stderr` | string | Stderr collected so far. |

```
Client                                          Server
  │── execute_bash (long command) ─────────-──────►│
  │                          (task_timeout passes) │
  │◄── result {process_id, status:"running", ...} ─│
  │◄── event  {process/pending, data:{process_id}} ┤
  │                                                │
  │── process_output {process_id, block:true} ────►│
  │◄── result {stdout, running:false, exit_code} ──│
```

Background processes are terminated when a `shutdown` request is received.

---

## 5. Security Model

### 5.1 Sandbox Features

The `capabilities.sandbox` field in the `initialize` response lists every sandbox feature the server supports. Clients send a `sandbox` request (see §3.2) to activate restrictions; only features in this list may be configured. The per-session sandbox can only be **equal to or narrower** than the server-level workdir.

| Feature | Config value type | Description |
|---|---|---|
| `"read"` | `PathRule[]` | Access control rules for read tools (`read_file`, `list_dir`, `glob`, `grep_search`). |
| `"write"` | `PathRule[]` | Access control rules for write tools (`write_file`, `edit`). |
| `"network"` | `boolean` | Whether outbound network access is permitted. `true` allows network calls; `false` blocks all outbound connections. |

**PathRule — rule-list matching:**

`read` and `write` each take an ordered list of `PathRule` objects:

```typescript
interface PathRule {
    action: "allow" | "deny";
    path:   string;            // absolute path
}
```

Rules are evaluated **top-down**; the first rule whose `path` equals or is an ancestor of the target path wins. If no rule matches, access is **denied** by default.

```
Rules:
  {"action": "deny",  "path": "/project/secrets"}
  {"action": "allow", "path": "/project"}

  /project/main.go       → rule 2 matches → allow  ✓
  /project/secrets/key   → rule 1 matches → deny   ✗
  /other/file            → no match       → deny   ✗
```

Extension features MUST use an `"x/"` prefix (e.g. `"x/gpu"`). Unknown or unsupported features are rejected with `protocol/invalid_params`.

### 5.2 Workdir & Path Validation

Every server instance is bound to a **workdir**. All file-system tools validate path arguments against the workdir before any I/O is performed. Requests that attempt to access paths outside the workdir are rejected with `security/sandbox_denied` before any system call is made. If the workdir is not configured, the server's current working directory at startup is used.

The server resolves each input path to an absolute path and checks:

1. **Relative paths** are resolved relative to the workdir (not the process CWD).
2. **Absolute paths** are accepted only if they fall within the workdir.
3. **Path traversal** sequences are rejected after `filepath.Clean` resolves them.
4. The resolved path must be **equal to** or **a descendant of** the workdir.

```
Workdir:  /home/user/project

  "src/main.go"               → /home/user/project/src/main.go  ✓
  "/home/user/project/go.mod" → /home/user/project/go.mod       ✓
  "../secret"                 → /home/user/secret                ✗
  "/etc/passwd"               → /etc/passwd                      ✗
```

> **Note on `execute_bash`:** Shell commands run under the server's OS user permissions and can access the full file system. The workdir restriction applies only to the `working_dir` parameter. Clients SHOULD be aware of this when deciding whether to expose `execute_bash` to an LLM.

---

## 6. Error Codes

Error codes use a `"category/specific"` string format. The `code` field appears in every Error Response.

```typescript
type ErrorCode =
    // Protocol errors
    | "protocol/parse_error"        // Request body is not valid JSON
    | "protocol/tool_not_found"     // No tool registered with that name
    | "protocol/invalid_params"     // Tool-specific parameter validation failed
    | "protocol/not_initialized"    // Tool request arrived before initialize
    | "protocol/shutting_down"      // Request arrived after shutdown

    // Security errors
    | "security/unauthorized"       // Missing or invalid authentication credentials
    | "security/sandbox_denied"      // Server-level access denial (e.g. path outside workdir)
    | "security/os_denied"          // OS-level permission denied (EPERM / EACCES)

    // Resource errors
    | "resource/not_found"          // File, directory, or executable does not exist
    | "resource/is_directory"       // Expected a file but got a directory
    | "resource/unsupported_format" // File format not supported by this tool
    | "resource/too_large"          // File exceeds the size limit

    // Execution errors
    | "exec/timeout"                // Command exceeded the timeout

    // Server errors
    | "server/internal_error"       // Unexpected server-side failure

    // Custom / extension errors
    | `x/${string}`;                // Extension-defined codes; MUST be prefixed with "x/"
```

---

## 7. Appendix: Type Definitions

Formal TypeScript definitions and concrete examples for all protocol messages. The normative description is in §2–§5.

### Request

```typescript
type Request = LifecycleRequest | ToolRequest;

interface BaseRequest {
    id: string;
    method: "initialize" | "sandbox" | "tool" | "shutdown";
    input: object;
}

interface LifecycleRequest extends BaseRequest {
    method: "initialize" | "sandbox" | "shutdown";
}

interface ToolRequest extends BaseRequest {
    method: "tool";
    tool: string;
}
```

**Example — lifecycle request:**

```json
{"id":"init-1","method":"initialize","input":{"protocolVersion":"0.3","clientInfo":{"name":"my-agent"}}}
```

**Example — tool invocation:**

```json
{
    "id": "req-42",
    "method": "tool",
    "tool": "read_file",
    "input": {
        "file_path": "src/main.go",
        "start_line": 1,
        "end_line": 50
    }
}
```

### Response

```typescript
interface Response {
    id: string;
    type: "result";
    result: object;
}
```

**Example:**

```json
{
    "id": "req-42",
    "type": "result",
    "result": {
        "file_path": "src/main.go",
        "content": "package main\n...",
        "total_lines": 171,
        "start_line": 1,
        "end_line": 50
    }
}
```

### Error Response

```typescript
interface ErrorResponse {
    id: string | null;
    type: "error";
    code: ErrorCode;
    error: string | object;
}
```

**Example — security error:**

```json
{
    "id": "req-42",
    "type": "error",
    "code": "security/sandbox_denied",
    "error": "path \"../etc/passwd\" is outside of workdir \"/home/user/project\""
}
```

**Example — execution timeout with structured error:**

```json
{
    "id": "req-10",
    "type": "error",
    "code": "exec/timeout",
    "error": {
        "message": "command timed out after 60 seconds",
        "timeout": 60,
        "stdout": "Running tests...\n...",
        "stderr": ""
    }
}
```

**Example — parse error (`id` is null):**

```json
{
    "id": null,
    "type": "error",
    "code": "protocol/parse_error",
    "error": "invalid JSON: unexpected end of JSON input"
}
```

### Event

```typescript
interface Event {
    type: "event";
    event: string;
    data: object;
}
```

**Example — built-in process event:**

```json
{
    "type": "event",
    "event": "process/pending",
    "data": {
        "process_id": "proc-abc123",
        "elapsed": 10,
        "stdout": "Running migrations...\n",
        "stderr": ""
    }
}
```

**Example — extension event:**

```json
{"type":"event","event":"x/build_complete","data":{"status":"success","duration_ms":3420}}
```

### Initialize

```typescript
interface InitializeParams {
    protocolVersion: string;
    clientInfo?: {
        name: string;
        version?: string;
    };
    auth?: {
        token: string;  // Used when HTTP headers cannot be set (e.g. browser WebSocket)
    };
    capabilities?: {
        tools?: {
            include?: string[];
            exclude?: string[];
        };
    };
}

interface InitializeResult {
    protocolVersion: string;
    serverInfo: {
        name: string;
        version: string;
    };
    capabilities: {
        tools: ToolDefinition[];
        /**
         * Present if the server supports the sandbox method (§3.2).
         * Lists the sandbox features the server supports.
         * Clients MUST only configure features that appear in this list.
         */
        sandbox?: SandboxFeature[];
    };
    /** Absolute path of the current working directory. */
    workdir: string;
}

/** Built-in sandbox feature names. Extension features use the "x/" prefix. */
type SandboxFeature = "read" | "write" | "network" | `x/${string}`;

/** A single access-control rule for path-based features. */
interface PathRule {
    action: "allow" | "deny";
    path:   string;   // absolute path
}

/**
 * Sandbox request input: a flat object keyed by feature name.
 * Only features advertised in capabilities.sandbox may be configured.
 */
interface SandboxParams {
    read?:    PathRule[];  // Ordered rule list; first match wins; default deny
    write?:   PathRule[];  // Ordered rule list; first match wins; default deny
    network?: boolean;     // true = allow outbound network; false = block all
    [feature: string]: unknown;
}

interface SandboxResult {
    // Empty object on success
}
```

**Example — full tool set:**

```json
{
    "id": "init-1",
    "method": "initialize",
    "input": {
        "protocolVersion": "0.3",
        "clientInfo": {"name": "my-agent", "version": "1.0"}
    }
}
```

**Example — read-only agent (exclude write and execute tools):**

```json
{
    "id": "init-1",
    "method": "initialize",
    "input": {
        "protocolVersion": "0.3",
        "clientInfo": {"name": "code-reviewer"},
        "capabilities": {
            "tools": {"exclude": ["write_file", "edit", "execute_bash"]}
        }
    }
}
```

**Example — minimal agent with explicit whitelist:**

```json
{
    "id": "init-1",
    "method": "initialize",
    "input": {
        "protocolVersion": "0.3",
        "capabilities": {
            "tools": {"include": ["read_file", "list_dir", "grep_search"]}
        }
    }
}
```

**Example — response (server supports sandbox):**

```json
{
    "id": "init-1",
    "type": "result",
    "result": {
        "protocolVersion": "0.3",
        "serverInfo": {"name": "gTSP", "version": "v0.3.0"},
        "capabilities": {
            "tools": [
                {"name": "read_file", "description": "...", "input_schema": {}},
                {"name": "list_dir",  "description": "...", "input_schema": {}}
            ],
            "sandbox": ["read", "write"]
        },
        "workdir": "/home/user/myproject"
    }
}
```

**Example — sandbox request (restrict reads to `/project/src`, writes to `/project/out`):**

```json
{"id": "sb-1", "method": "sandbox", "input": {
    "read":  ["/project/src", "/project/docs"],
    "write": ["/project/out"]
}}
```

**Example — sandbox response:**

```json
{"id": "sb-1", "type": "result", "result": {
    "workdir": "/project",
    "read":  ["/project/src", "/project/docs"],
    "write": ["/project/out"]
}}
```

**Example — version incompatibility error:**

```json
{
    "id": "init-1",
    "type": "error",
    "code": "protocol/parse_error",
    "error": "unsupported protocol version \"1.0\"; server supports \"0.3\""
}
```

### Shutdown

**Request:**

```json
{"id":"shutdown-1","method":"shutdown","input":{}}
```

**Response:**

```json
{"id":"shutdown-1","type":"result","result":{}}
```

### Tool Definition

```typescript
interface ToolDefinition {
    name: string;
    description: string;
    input_schema: JSONSchema;
}
```

**Example:**

```json
{
    "name": "read_file",
    "description": "Reads and returns the content of a file. Use for inspecting source code, configs, or any text file within the workdir.",
    "input_schema": {
        "type": "object",
        "properties": {
            "file_path": {"type": "string", "description": "Path to the file to read."},
            "start_line": {"type": "integer", "description": "1-based line to start from."},
            "end_line":   {"type": "integer", "description": "1-based line to stop at (inclusive)."}
        },
        "required": ["file_path"]
    }
}
```
