# TSP 协议

**版本:** 0.3  |  **状态:** Draft

---

## 目录

1. [概述](#1-概述)
2. [基础协议](#2-基础协议)
   - 2.1 [传输层](#21-传输层)
   - 2.2 [认证（WebSocket）](#22-认证websocket)
   - 2.3 [消息帧](#23-消息帧)
   - 2.4 [请求](#24-请求)
   - 2.5 [响应](#25-响应)
   - 2.6 [错误响应](#26-错误响应)
   - 2.7 [事件](#27-事件)
3. [生命周期](#3-生命周期)
   - 3.1 [初始化](#31-初始化)
   - 3.2 [沙箱配置](#32-沙箱配置) *(可选)*
   - 3.3 [工具调用](#33-工具调用)
   - 3.4 [关闭](#34-关闭)
4. [工具 API](#4-tool-api)
   - 4.1 [工具定义](#41-工具定义)
   - 4.2 [工具调用](#42-工具调用)
   - 4.3 [并发](#43-并发)
   - 4.4 [长时间运行的进程](#44-长时间运行的进程)
5. [安全模型](#5-安全模型)
   - 5.1 [沙箱特性](#51-沙箱特性)
   - 5.2 [工作目录与路径校验](#52-工作目录与路径校验)
6. [错误码](#6-错误码)
7. [附录：类型定义](#7-附录类型定义)

---

## 1. 概述

TSP 是一个用于执行系统工具的 **请求/响应** 协议。**客户端**（通常是 AI Agent 或其宿主进程）发送 JSON 请求到 **服务器**，服务器执行请求的工具并返回 JSON 结果。

设计目标：

- **简洁** — 每条消息一个 JSON 对象，最小化握手流程。
- **传输无关** — 相同的消息格式适用于 stdio 和 WebSocket。
- **并发** — 多个请求可以同时进行；响应通过 `id` 关联。
- **LLM 原生** — 工具 Schema 以主流 LLM API（Anthropic、OpenAI）期望的格式表达，并在连接时通过 `initialize` 传递。
- **安全** — 工作目录沙箱防止路径穿越；客户端可通过 `initialize` 中的能力过滤进一步限制工具集。

---

## 2. 基础协议

### 2.1 传输层

TSP 支持两种传输方式。消息格式在两者上完全相同。

#### stdio（默认）

客户端将服务器作为子进程启动，通过标准流通信：

| 流 | 方向 | 内容 |
|---|---|---|
| `stdin` | 客户端 → 服务器 | 换行分隔的 JSON 请求 |
| `stdout` | 服务器 → 客户端 | 换行分隔的 JSON 响应和事件 |
| `stderr` | 服务器 → (日志文件) | 人类可读的调试日志（不属于协议） |

每条消息是单行 JSON，以 `\n` 结束。客户端不得发送多行 JSON。服务器必须将每个响应写成单行。

#### WebSocket

服务器启动 HTTP 服务器并将连接升级为 WebSocket。

- **端点:** `ws://<host>:<port>/tsp`
- **消息类型:** 每个 text frame 一个 JSON 对象。
- 多个客户端可同时连接；每个连接独立。

### 2.2 认证（WebSocket）

认证仅适用于 WebSocket 传输。stdio 连接作为子进程在父进程的 OS 用户下运行，无需额外认证。

支持两种方式：

#### 方式 1 — HTTP Authorization Header

在 WebSocket 升级握手时传递 Bearer token。这是服务器间或原生客户端场景的推荐方式。

```
GET /tsp HTTP/1.1
Upgrade: websocket
Authorization: Bearer <token>
```

服务器应在 token 缺失或无效时以 HTTP 401 拒绝升级。

#### 方式 2 — 在 `initialize` Input 中传递 Token

对于无法设置自定义 HTTP 头的环境（如浏览器 `WebSocket` API），在 `initialize` 请求的 `input.auth.token` 字段传递 token。

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

服务器必须在处理 `initialize` 响应前验证 token。如果 token 无效或缺失，服务器必须返回错误并关闭连接。

> 当同时提供 `Authorization` 头和 `auth.token` 时，头部优先。

#### 认证失败

如果在 HTTP 升级阶段（方式 1）认证失败，服务器必须以 HTTP 401 拒绝，不建立 WebSocket 连接。

如果在 `initialize` 阶段（方式 2）认证失败，服务器必须：
1. 返回错误响应，`code: "security/unauthorized"`。
2. 发送响应后立即关闭 WebSocket 连接。

在成功 `initialize` 之前到达的任何工具请求也会以 `security/unauthorized` 拒绝，并关闭连接。

如果 WebSocket 连接已建立但没有有效的 `Authorization` 头（即未使用方式 1），且在认证超时（默认：10 秒）内未完成 `initialize`，服务器必须关闭连接而不发送错误响应。实现可配置此超时时间。

### 2.3 消息帧

| 传输 | 帧格式 |
|---|---|
| stdio | 每行一个 JSON 对象（`\n` 结束） |
| WebSocket | 每个 text frame 一个 JSON 对象 |

### 2.4 请求

请求从客户端发送到服务器。`method` 字段区分消息类型。

| 字段 | 类型 | 必须 | 描述 |
|---|---|---|---|
| `id` | string | 是 | 唯一标识符。在响应中回显以关联。 |
| `method` | string | 是 | `"initialize"`、`"sandbox"` *(可选)*、`"tool"` 或 `"shutdown"`。 |
| `tool` | string | 当 `method="tool"` | 要调用的工具名称（如 `"read_file"`）。 |
| `input` | object | 是 | 请求特定参数。结构取决于 `method` 和 `tool`。 |

```json
{"id":"req-1","method":"tool","tool":"read_file","input":{"file_path":"src/main.go"}}
```

### 2.5 响应

服务器成功执行请求后发送。

| 字段 | 类型 | 描述 |
|---|---|---|
| `id` | string | 回显原始请求的 `id`。 |
| `type` | `"result"` | 成功响应总是 `"result"`。 |
| `result` | object | 工具输出。结构取决于工具。 |

```json
{"id":"req-1","type":"result","result":{"content":"package main\n...","total_lines":171}}
```

### 2.6 错误响应

服务器无法执行请求时发送。

| 字段 | 类型 | 描述 |
|---|---|---|
| `id` | string \| null | 回显请求 `id`。如果请求无法解析则为 `null`。 |
| `type` | `"error"` | 总是 `"error"`。 |
| `code` | ErrorCode | 机器可读的错误码。见 [§6 错误码](#6-错误码)。 |
| `error` | string \| object | 人类可读的描述，或复杂错误的结构化详情。 |

```json
{"id":"req-1","type":"error","code":"resource/not_found","error":"file not found: src/missing.go"}
```

当 JSON 解析失败（`protocol/parse_error`）时，无法提取 `id`，设为 `null`。客户端必须通过 `code` 而非 `id` 识别这些响应。

### 2.7 事件

服务器主动发送给客户端的消息。事件没有 `id`，不需要回复。

| 字段 | 类型 | 描述 |
|---|---|---|
| `type` | `"event"` | 总是 `"event"`。 |
| `event` | string | 事件名称。内置事件使用 `"process/"` 前缀；扩展事件使用 `"x/"` 前缀。 |
| `data` | object | 事件特定数据。 |

---

## 3. 生命周期

### 3.1 初始化

客户端必须在调用任何工具前发送 `initialize` 请求。

`initialize` 请求有三个目的：
1. **协议版本协商** — 客户端和服务器就协议版本达成一致。
2. **客户端标识** — 客户端声明其身份用于日志和审计。
3. **能力协商** — 客户端请求过滤的工具子集；服务器返回它们的 Schema 并声明可选能力（如沙箱）已就绪。

**请求 `input` 字段：**

| 字段 | 类型 | 必须 | 描述 |
|---|---|---|---|
| `protocolVersion` | string | 是 | 客户端支持的协议版本，如 `"0.3"`。 |
| `clientInfo.name` | string | 否 | 客户端名称用于日志，如 `"my-agent"`。 |
| `clientInfo.version` | string | 否 | 客户端版本字符串。 |
| `auth.token` | string | 否 | 无法设置 HTTP 头时的 WebSocket 认证 token。见 §2.2。 |
| `capabilities.tools.include` | string[] | 否 | 白名单：仅返回这些工具。省略表示所有工具。 |
| `capabilities.tools.exclude` | string[] | 否 | 黑名单：排除这些工具。在 `include` 之后应用。 |

**响应 `result` 字段：**

| 字段 | 类型 | 描述 |
|---|---|---|
| `protocolVersion` | string | 此会话服务器将使用的协议版本。 |
| `serverInfo.name` | string | 服务器实现名称，如 `"gTSP"`。 |
| `serverInfo.version` | string | 服务器版本，如 `"v0.3.0"`。 |
| `capabilities.tools` | ToolDefinition[] | 应用能力过滤后的工具 Schema。可直接传递给 LLM 工具注册 API。 |
| `capabilities.sandbox` | string[] \| absent | 服务器支持的沙箱特性列表，如 `["read","write"]`。若服务器不支持 `sandbox` 方法则不存在。见 §3.2。 |
| `workdir` | string | 当前工作目录的绝对路径。 |

```
Client                                        Server
  │────────────── initialize ──────────────────►│
  │◄── result {protocolVersion, tools, ...} ────│
```

**版本协商：**

| 情况 | 服务器行为 |
|---|---|
| 版本匹配 | 正常响应 |
| 客户端次要版本较低 | 正常响应；服务器使用较高版本 |
| 主版本不兼容 | 错误响应（`protocol/parse_error`） |

**向后兼容：** 服务器应将 `initialize` 之前到达的工具请求视为已执行默认 `initialize`（所有工具，无过滤）。

### 3.2 沙箱配置 *(可选)*

`sandbox` 方法配置文件 I/O 和网络的会话级访问限制。可在 `initialize` 之后、`shutdown` 之前的任何时刻发送。发送多个 `sandbox` 请求会替换之前的配置。

客户端必须在发送 `sandbox` 请求前检查 `initialize` 响应中的 `capabilities.sandbox`。若不存在，服务器不支持此方法，任何 `sandbox` 请求将返回 `protocol/tool_not_found`。支持的特性及其语义见 [§5.1 沙箱特性](#51-沙箱特性)。

**请求 `input`：**

一个扁平对象，每个键是 `capabilities.sandbox` 中的特性名称，值是特性特定配置。未知或不支持的特性以 `protocol/invalid_params` 拒绝。

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

**响应 `result`：** 成功时为空对象 `{}`。

```
Client                                                         Server
  │────────────────────────── initialize ───────────────────────►│
  │◄────── result {capabilities.sandbox: ["read","write"]} ──────│
  │                                                              │
  │──────────── sandbox {"read":[{action,path},...]} ───────────►│
  │◄────────────────────── result {} ────────────────────────────│
  │                                                              │
  │───────────────────── tool request ─── ──────────────────────►│  (沙箱生效)
```

### 3.3 工具调用

`initialize` 之后（可选 `sandbox` 之后），客户端可在 `shutdown` 之前的任何时刻调用工具。工具请求独立，可并发发送；响应可能以不同顺序返回，必须通过 `id` 关联。

调用工具时，发送 `method: "tool"` 及工具名称和 input：

```
Client                                          Server
  │── {"method":"tool","tool":"read_file",...} ──►│
  │◄── {"type":"result","result":{...}} ──────────│
```

服务器必须只执行 `initialize` 能力协商中包含的工具。排除或未知的工具请求返回 `protocol/tool_not_found`。

完整工具 Schema 参考、并发模型和长进程处理见 [§4 工具 API](#4-tool-api)。

### 3.4 关闭

TSP 使用两阶段关闭，允许服务器清理资源（后台进程、临时文件、日志缓冲）。

**阶段 1 — shutdown 请求：** 客户端发送 `{"id":"...","method":"shutdown","input":{}}`。服务器必须：
1. 停止接受新请求（返回 `protocol/shutting_down`）。
2. 等待所有进行中的请求完成。
3. 终止所有长时间 `execute_bash` 调用创建的后台进程。
4. 清理资源。
5. 返回空结果 `{}`。

**阶段 2 — 传输关闭：** 客户端关闭传输（stdio 上 EOF，或关闭 WebSocket）。这通知服务器退出。

```
Client                     Server
  │────────── shutdown ───────►│
  │                            │  ① 停止接受新请求
  │                            │  ② 等待进行中请求完成
  │                            │  ③ 清理资源
  │◄──────── result {} ────────│
  │───── EOF / disconnect ────►│
  │                            │── exit
```

如果传输在没有事先 `shutdown` 的情况下关闭（如客户端崩溃），服务器应尽力清理。进行中的请求可能被中断。

---

## 4. 工具 API

### 4.1 工具定义

工具定义描述单个工具。它在 `initialize` 响应中传递给客户端，可直接传递给 LLM 工具注册 API 而无需转换。

| 字段 | 类型 | 描述 |
|---|---|---|
| `name` | string | 工具名称，用于 ToolRequest 的 `tool` 字段。 |
| `description` | string | LLM 可读的描述，说明工具功能及使用时机。 |
| `input_schema` | JSONSchema | 工具 `input` 的 JSON Schema（draft 2020-12）。兼容 Anthropic Tool Use API 和 OpenAI function calling。 |

### 4.2 工具调用

调用工具时，客户端发送 `method: "tool"` 的请求。服务器必须只执行 `initialize` 能力协商中包含的工具；排除或未知的工具请求返回 `protocol/tool_not_found`。

```
Client                                          Server
  │── {"method":"tool","tool":"read_file",...} ──►│
  │◄── {"type":"result","result":{...}} ──────────│
```

每个内置工具的完整 input 和 result Schema 见 [内置工具参考](./tools/README.md)。

### 4.3 并发

服务器必须支持并发请求执行：

- 请求按到达顺序处理；响应可能以**不同顺序**返回。
- 客户端必须使用 `id` 字段关联响应与请求。

```
Client                Server
  │── req id="1" ──────►│
  │── req id="2" ──────►│  (两者并发执行)
  │◄── resp id="2" ─────│  (id="2" 先完成)
  │◄── resp id="1" ─────│
```

无法处理乱序响应的客户端应一次只保持一个进行中的请求。

### 4.4 长时间运行的进程

默认情况下，`execute_bash` 会阻塞直到命令退出。进程通过两种方式成为**后台进程**：

- **显式：** 设置 `run_in_background: true`，立即返回 `process_id`，不等待。
- **自动：** 当命令在 `task_timeout` 秒（默认：10）内未退出时，服务器自动提升它，返回包含 `process_id` 的结果，并发出 `process/pending` 事件。

调用者使用 `process_output` 获取输出并检查完成状态，使用 `process_stop` 终止进程，使用 `process_list` 列出当前活跃的后台进程。

**后台进程结果字段：**

| 字段 | 类型 | 描述 |
|---|---|---|
| `process_id` | string | 不透明句柄。传递给 `process_output` 或 `process_stop`。 |
| `status` | `"running"` | 表示进程仍在运行。 |
| `stdout` | string | 目前收集的输出（`run_in_background: true` 时为空）。 |
| `stderr` | string | 目前收集的 stderr（`run_in_background: true` 时为空）。 |

**`process/pending` 事件 `data` 字段：**

| 字段 | 类型 | 描述 |
|---|---|---|
| `process_id` | string | 与结果中的 `process_id` 匹配。 |
| `running_time` | string | 命令启动以来的人类可读时间（如 "1h 21m 39s"）。 |
| `stdout` | string | 目前收集的输出。 |
| `stderr` | string | 目前收集的 stderr。 |

```
Client                                          Server
  │── execute_bash (long command) ─────────-──────►│
  │                          (task_timeout passes) │
  │◄── result {process_id, status:"running", ...} ─│
  │◄── event  {process/pending, data:{process_id}} ┤
  │                                                │
  │── process_list {} ────────────────────────────►│
  │◄── result {processes: [{process_id, ...}]} ────│
  │                                                │
  │── process_output {process_id, block:true} ────►│
  │◄── result {stdout, running:false, exit_code} ──│
```

收到 `shutdown` 请求时，后台进程会被终止。

---

## 5. 安全模型

### 5.1 沙箱特性

`initialize` 响应中的 `capabilities.sandbox` 字段列出服务器支持的所有沙箱特性。客户端发送 `sandbox` 请求（见 §3.2）激活限制；只有此列表中的特性可配置。会话级沙箱只能**等于或窄于**服务器级工作目录。

| 特性 | 配置值类型 | 描述 |
|---|---|---|
| `"read"` | `PathRule[]` | 读工具的访问控制规则（`read_file`、`list_dir`、`glob`、`grep_search`）。 |
| `"write"` | `PathRule[]` | 写工具的访问控制规则（`write_file`、`edit`）。 |
| `"network"` | `boolean` | 是否允许出站网络访问。`true` 允许；`false` 阻止所有出站连接。 |

**PathRule — 规则列表匹配：**

`read` 和 `write` 各接受有序的 `PathRule` 对象列表：

```typescript
interface PathRule {
    action: "allow" | "deny";
    path:   string;            // 绝对路径
}
```

规则**自上而下**评估；第一个 `path` 等于或为目标路径祖先的规则获胜。若无匹配规则，默认**拒绝**。

```
Rules:
  {"action": "deny",  "path": "/project/secrets"}
  {"action": "allow", "path": "/project"}

  /project/main.go       → 规则 2 匹配 → 允许 ✓
  /project/secrets/key   → 规则 1 匹配 → 拒绝 ✗
  /other/file            → 无匹配     → 拒绝 ✗
```

扩展特性必须使用 `"x/"` 前缀（如 `"x/gpu"`）。未知或不支持的特性以 `protocol/invalid_params` 拒绝。

### 5.2 工作目录与路径校验

每个服务器实例绑定到一个**工作目录**。所有文件系统工具在执行任何 I/O 前验证路径参数。尝试访问工作目录外路径的请求以 `security/sandbox_denied` 拒绝，不执行任何系统调用。若未配置工作目录，使用启动时的进程当前目录。

服务器将每个输入路径解析为绝对路径并检查：

1. **相对路径** 相对于工作目录解析（而非进程 CWD）。
2. **绝对路径** 仅在工作目录内时接受。
3. **路径穿越** 序列在 `filepath.Clean` 解析后拒绝。
4. 解析后路径必须**等于**或**为**工作目录的子路径。

```
Workdir:  /home/user/project

  "src/main.go"               → /home/user/project/src/main.go  ✓
  "/home/user/project/go.mod" → /home/user/project/go.mod       ✓
  "../secret"                 → /home/user/secret                ✗
  "/etc/passwd"               → /etc/passwd                      ✗
```

> **关于 `execute_bash` 的说明：** Shell 命令在服务器 OS 用户权限下运行，可访问完整文件系统。工作目录限制仅适用于 `working_dir` 参数。客户端在决定是否向 LLM 暴露 `execute_bash` 时应注意这一点。

---

## 6. 错误码

错误码使用 `"类别/具体"` 字符串格式。`code` 字段出现在每个错误响应中。

```typescript
type ErrorCode =
    // 协议错误
    | "protocol/parse_error"        // 请求体不是有效 JSON
    | "protocol/tool_not_found"     // 没有注册该名称的工具
    | "protocol/invalid_params"     // 工具特定参数验证失败
    | "protocol/not_initialized"    // 工具请求在 initialize 之前到达
    | "protocol/shutting_down"      // 请求在 shutdown 之后到达

    // 安全错误
    | "security/unauthorized"       // 缺失或无效的认证凭据
    | "security/sandbox_denied"      // 服务器级访问拒绝（如路径在工作目录外）
    | "security/os_denied"          // OS 级权限拒绝（EPERM / EACCES）

    // 资源错误
    | "resource/not_found"          // 文件、目录或可执行文件不存在
    | "resource/is_directory"       // 期望文件但得到目录
    | "resource/unsupported_format" // 文件格式不被此工具支持
    | "resource/too_large"          // 文件超过大小限制

    // 执行错误
    | "exec/timeout"                // 命令超过超时时间

    // 服务器错误
    | "server/internal_error"       // 意外的服务器端失败

    // 自定义 / 扩展错误
    | `x/${string}`;                // 扩展定义的错误码；必须以 "x/" 为前缀
```

---

## 7. 附录：类型定义

所有协议消息的正式 TypeScript 定义和具体示例。规范性描述在 §2–§5。

### 请求

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

**示例 — 生命周期请求：**

```json
{"id":"init-1","method":"initialize","input":{"protocolVersion":"0.3","clientInfo":{"name":"my-agent"}}}
```

**示例 — 工具调用：**

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

### 响应

```typescript
interface Response {
    id: string;
    type: "result";
    result: object;
}
```

**示例：**

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

### 错误响应

```typescript
interface ErrorResponse {
    id: string | null;
    type: "error";
    code: ErrorCode;
    error: string | object;
}
```

**示例 — 安全错误：**

```json
{
    "id": "req-42",
    "type": "error",
    "code": "security/sandbox_denied",
    "error": "path \"../etc/passwd\" is outside of workdir \"/home/user/project\""
}
```

**示例 — 执行超时的结构化错误：**

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

**示例 — 解析错误（`id` 为 null）：**

```json
{
    "id": null,
    "type": "error",
    "code": "protocol/parse_error",
    "error": "invalid JSON: unexpected end of JSON input"
}
```

### 事件

```typescript
interface Event {
    type: "event";
    event: string;
    data: object;
}
```

**示例 — 内置进程事件：**

```json
{
    "type": "event",
    "event": "process/pending",
    "data": {
        "process_id": "proc-abc123",
        "running_time": "10s",
        "stdout": "Running migrations...\n",
        "stderr": ""
    }
}
```

**示例 — 扩展事件：**

```json
{"type":"event","event":"x/build_complete","data":{"status":"success","duration_ms":3420}}
```

### 初始化

```typescript
interface InitializeParams {
    protocolVersion: string;
    clientInfo?: {
        name: string;
        version?: string;
    };
    auth?: {
        token: string;  // 无法设置 HTTP 头时使用（如浏览器 WebSocket）
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
         * 若服务器支持 sandbox 方法（§3.2）则存在。
         * 列出服务器支持的沙箱特性。
         * 客户端必须只配置此列表中出现的特性。
         */
        sandbox?: SandboxFeature[];
    };
    /** 当前工作目录的绝对路径。 */
    workdir: string;
}

/** 内置沙箱特性名称。扩展特性使用 "x/" 前缀。 */
type SandboxFeature = "read" | "write" | "network" | `x/${string}`;

/** 基于路径特性的单条访问控制规则。 */
interface PathRule {
    action: "allow" | "deny";
    path:   string;   // 绝对路径
}

/**
 * sandbox 请求 input：以特性名称为键的扁平对象。
 * 只有 capabilities.sandbox 中声明的特性可配置。
 */
interface SandboxParams {
    read?:    PathRule[];  // 有序规则列表；首个匹配获胜；默认拒绝
    write?:   PathRule[];  // 有序规则列表；首个匹配获胜；默认拒绝
    network?: boolean;     // true = 允许出站网络；false = 阻止所有
    [feature: string]: unknown;
}

interface SandboxResult {
    // 成功时为空对象
}
```

**示例 — 完整工具集：**

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

**示例 — 只读 Agent（排除写和执行工具）：**

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

**示例 — 显式白名单的最小 Agent：**

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

**示例 — 响应（服务器支持沙箱）：**

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

**示例 — sandbox 请求（限制读取为 `/project/src`，写入为 `/project/out`）：**

```json
{"id": "sb-1", "method": "sandbox", "input": {
    "read":  ["/project/src", "/project/docs"],
    "write": ["/project/out"]
}}
```

**示例 — sandbox 响应：**

```json
{"id": "sb-1", "type": "result", "result": {
    "workdir": "/project",
    "read":  ["/project/src", "/project/docs"],
    "write": ["/project/out"]
}}
```

**示例 — 版本不兼容错误：**

```json
{
    "id": "init-1",
    "type": "error",
    "code": "protocol/parse_error",
    "error": "unsupported protocol version \"1.0\"; server supports \"0.3\""
}
```

### 关闭

**请求：**

```json
{"id":"shutdown-1","method":"shutdown","input":{}}
```

**响应：**

```json
{"id":"shutdown-1","type":"result","result":{}}
```

### 工具定义

```typescript
interface ToolDefinition {
    name: string;
    description: string;
    input_schema: JSONSchema;
}
```

**示例：**

```json
{
    "name": "read_file",
    "description": "读取并返回文件内容。用于检查源代码、配置文件或工作目录内的任何文本文件。",
    "input_schema": {
        "type": "object",
        "properties": {
            "file_path": {"type": "string", "description": "要读取的文件路径。"},
            "start_line": {"type": "integer", "description": "起始行号（从 1 开始）。"},
            "end_line":   {"type": "integer", "description": "结束行号（从 1 开始，包含）。"}
        },
        "required": ["file_path"]
    }
}
```