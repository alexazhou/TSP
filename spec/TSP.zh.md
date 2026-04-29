# 工具服务协议 (TSP)

**版本:** 0.3  |  **参考实现:** [gTSP](https://github.com/alexazhou/gTSP)

---

工具服务协议 (Tool Server Protocol, TSP) 定义了一个标准通信协议，用于将本地系统操作（文件读写、Shell 执行、搜索等）暴露给 AI Agent 和大语言模型 (LLM)。

TSP 受微软为 VS Code 创建的 [语言服务协议 (LSP)](https://microsoft.github.io/language-server-protocol/) 启发。两个协议遵循相同的架构哲学：**通过定义良好的、传输无关的协议，将能力提供者与消费者解耦**。

在 LSP 中，代码编辑器与语言服务器通信以获得代码智能（补全、诊断、重命名等）。在 TSP 中，AI Agent 与工具服务器通信以执行系统操作（读取文件、运行命令、搜索代码等）。

```
┌──────────────────┐        TSP 消息 (JSON)        ┌──────────────────┐
│    AI Agent      │  ──────────────────────────────►  │   Tool Server    │
│  (LLM / Host)    │  ◄──────────────────────────────  │(如 gtsp 二进制) │
└──────────────────┘    stdio  或  WebSocket           └──────────────────┘
```

## 为什么需要 TSP？

### m×n 问题

没有标准协议时，每个需要系统工具的 AI Agent 都必须自己实现这些工具。如果有 **m** 个 Agent 和 **n** 个工具，那就是 **m×n** 个独立实现——每个都有自己的 bug、安全漏洞和维护负担。

```
没有 TSP                       有 TSP

Agent A ──► read_file (实现 A)    Agent A ──┐
Agent A ──► exec_bash (实现 A)              │
Agent A ──► list_dir  (实现 A)    Agent B ──┼──► TSP Server ──► read_file
                                            │                ──► exec_bash
Agent B ──► read_file (实现 B)    Agent C ──┘                ──► list_dir
Agent B ──► exec_bash (实现 B)
Agent B ──► list_dir  (实现 B)

Agent C ──► ...

m×n 个实现                       m+n 个实现
```

TSP 将 m×n 矩阵分解为 **m+n**：每个 Agent 只需实现一次 TSP 客户端协议，每个工具在服务器中只实现一次。这正是 LSP 的洞见——LSP 之前，每个编辑器都要为每种语言单独实现支持（m 个编辑器 × n 个语言插件）；LSP 之后，每个编辑器写一个 LSP 客户端，每种语言写一个 LSP 服务器。

| | 没有标准协议 | 有 TSP |
|---|---|---|
| 需要构建的集成数 | m × n | m + n |
| 安全所在 | 每个 Agent（不一致） | TSP 服务器（一处） |
| 工具 Schema 格式 | 每个 Agent 自定义 | 标准化，LLM 可直接使用 |
| 添加新 Agent | 重实现所有 n 个工具 | 实现一次 TSP 客户端 |
| 添加新工具 | 更新所有 m 个 Agent | 实现一次 TSP 工具 |

### 其他好处

| 问题 | TSP 解决方案 |
|---|---|
| 每个 AI 框架都重新实现文件/Shell 工具 | 一个标准服务器，任意兼容客户端 |
| 工具逻辑与 Agent/推理逻辑纠缠 | 清晰的协议边界分离关注点 |
| 不同实现的安全策略不一致 | 工作区沙箱内建于协议 |
| 客户端无法发现有哪些工具可用 | Schema 通过 `initialize` 响应直接传递 |

## 与 LSP 的对比

| 方面 | LSP | TSP |
|---|---|---|
| 领域 | 代码智能 | 系统操作 |
| 消费者 | 代码编辑器（VS Code、Neovim 等） | AI Agent、LLM 主机 |
| 传输 | stdio (JSON-RPC 2.0) | stdio / WebSocket |
| 消息格式 | JSON-RPC 2.0 | TSP JSON（受 JSON-RPC 启发） |
| 能力发现 | `initialize` 握手 | `initialize` 握手 |
| 工具 Schema 传递 | 无 | 内联于 `initialize` 响应，可直接用于 LLM 注册 |
| 服务器生命周期信号 | `initialized` 通知 | 无（服务器在 `initialize` 后立即接受请求） |
| 关闭 | `shutdown` 请求 + `exit` 通知 | `shutdown` 请求 + 传输关闭 |
| 典型操作 | Hover、补全、跳转定义 | 读取、写入、执行、搜索、进程管理 |
| 并发 | 顺序（按文档） | 完全并发（按请求 `id`） |

## 目录

- [**协议规范**](./Protocol.zh.md) — 基础协议、消息格式、传输层、生命周期、工具调用 API
- [**内置工具参考**](./tools/README.md) — gTSP 提供的所有工具完整参考

## 协议一览

```
Client                                                    Server
  │                                                         │
  │                  (启动 / 连接)                           │
  │──────────────--────── initialize ──────────────────────►│
  │◄──────────────── result {tools, workdir} ───────────────│
  │                                                         │
  │              (将工具注册到 LLM)                          │
  │                                                         │
  │── {"id":"1","method":"tool","tool":"read_file",...} ───►│
  │── {"id":"2","method":"tool","tool":"list_dir",...} ────►│  (并发)
  │                                                         │
  │◄────────────── {"id":"2","type":"result",...} ──────────│
  │◄────────────── {"id":"1","type":"result",...} ──────────│
  │                                                         │
  │──────────────────────- shutdown ───────────────────────►│
  │◄────────────────────── result {} ───────────────────────│  (清理完成)
  │──────────────────── EOF / disconnect ──────────────────►│
  │                                                         │── exit
```