# TSP - Tool Service Protocol

[English Readme](README.en.md) 

---

**演示：只需 10 行代码打造一个自主行动的 agent**

![演示10行代码打造一个自主行动的 agent](image/demo.gif)

## 使用 TSP 构建的产品

- [**TogoSpace**](https://github.com/alexazhou/TogoSpace) — 多Agent 自由协作，让 Agent 组团帮你干活

## 什么是 TSP？

以下分为两部分:

1. **TSP 协议**：是 **Tool Service Protocol**（工具服务协议）的缩写，定义了一种标准化协议，将工具能力和 Agent 业务代码完全解耦，使得 Agent 的”大脑”和”手”可以分别实现。

2. **gTSP 实现**：按照 TSP 协议实现的一套高质量工具服务。单文件，零依赖，跨平台，全面覆盖 Agent 需求。你可以轻松集成到自己的应用中，**让你 10 行代码就可以完成一个自主行动的 Agent**，从而可以专注在业务开发而不是底层工具上。

## 快速开始

1. **获取服务端**：下载 [gTSP 二进制文件](https://github.com/alexazhou/TSP/releases)（或进入 `gtsp/` 目录通过 `go build` 自行构建）。
2. **安装客户端**：安装对应语言的 SDK（如 Python：`pip install pytspclient`）。
3. **运行示例**：参考 `examples/` 目录下的示例代码即可快速启动你的 Agent。

## 适用场景

TSP 为不同层次的 AI 应用提供了标准化的执行能力，主要适用于以下场景：

1. **开发具有自主“行动能力”的 Agent**
   - 快速构建如**编码助手**、**自动化运维工具**、**数据分析机器人**等。TSP 提供了一套开箱即用的“手”，让 Agent 能够像人一样直接操作文件系统和命令行，完成从规划到执行的闭环。

2. **作为大模型应用的标准工具层**
   - 如果你的应用已经在调用大模型处理文件或系统任务，你可以直接用 TSP **替换应用中内嵌的工具逻辑**。这样可以显著降低开发和维护成本，同时获得更专业、安全（带沙箱）且高性能的工具实现。

3. **集成到业务系统中实现远程操作**
   - 将 TSP 集成到企业级系统中，作为 Agent 或管理员远程管控机器的标准化接口。通过协议化的交互，不仅能简化跨平台操作，还能确保远程操作的安全边界与可追溯性。

## 为什么需要 TSP？

### m×n 问题

没有标准协议时，每个需要系统工具的 AI Agent 都必须自己实现这些工具。如果有 **m** 个 Agent 和 **n** 个工具，那就是 **m×n** 个独立实现——每个都有自己的开发成本、bugs，且工具实现与 Agent 代码耦合，难以维护。

```
没有 TSP                       有 TSP

Agent A ──► read_file (实现 A)    Agent A ──┐
Agent A ──► exec_bash (实现 A)              │
Agent A ──► list_dir  (实现 A)    Agent B ──┼──► TSP Server ──► read_file
                                            |               ──► exec_bash
Agent B ──► read_file (实现 B)    Agent C ──┘               ──► list_dir
Agent B ──► exec_bash (实现 B)
Agent B ──► list_dir  (实现 B)

Agent C ──► ...

m×n 个实现                       m+n 个实现
```

TSP 将 m×n 矩阵分解为 **m+n**：每个 Agent 只需实现一次 TSP 客户端协议，每个工具在服务器中只实现一次，可以提供设计优良且高质量的工具实现。

### 其他好处

| 问题 | TSP 解决方案 |
|---|---|
| 每个 AI 框架都重新实现文件/Shell 工具 | 一个标准服务器，任意兼容客户端 |
| 工具逻辑与 Agent/推理逻辑纠缠 | 清晰的协议边界分离关注点 |
| 不同实现的安全策略不一致 | 工作区沙箱内建于协议 |
| 客户端无法发现有哪些工具可用 | Schema 通过 `initialize` 响应直接传递 |

### 与 MCP 的区别

一句话：**TSP 实现 Agent，MCP 拓展 Agent**。

- **TSP** 提供基础系统工具（文件读写、命令执行、搜索等），让 Agent 具备自主行动的核心能力，适合从头构建一个完整的 Agent
- **MCP** 提供外部服务接入能力（数据库、API、第三方工具），让现有 Agent 获得更多功能扩展，适合增强已有的 Agent 系统

两者可以配合使用：先基于 TSP 实现一个通用 Agent。再通过 MCP 添加定制能力满足个性化需求。

## TSP 特点

- **简洁易用**：10 行代码即可实现一个自主行动的 Agent
- **安全可控**：内置沙箱机制，限制文件访问范围
- **传输灵活**：支持 stdio、WebSocket 等多种传输模式
- **开箱即用**：提供高性能、跨平台、零依赖的 Go 语言服务端
- **开放定制**：全开源，可自由添加自定义工具

## 提供的工具

| 工具 | 功能 |
|------|------|
| `list_dir` | 列出目录结构 |
| `read_file` | 读取文件内容 |
| `write_file` | 写入文件 |
| `edit` | 精确替换文件内容 |
| `grep_search` | 代码搜索 |
| `glob` | 文件名匹配 |
| `execute_bash` | 执行 shell 命令 |
| `process_*` | 进程管理 |

详见 [spec/tools/](spec/tools/)

## 项目结构

- **`spec/`**: 协议规范与工具定义文档。
- **`gtsp/`**: 高性能 Go 参考实现（服务端），单文件、零依赖。
- **`client/`**: 多语言客户端 SDK（目前支持 Python）。
- **`examples/`**: 入门示例与演示代码，包含 10 行代码打造 Agent 的例子。
- **`tsp_gui_tester/`**: 用于可视化测试和调试 TSP 服务端的 GUI 工具。

## 快速开始

1. **获取服务端**：下载 [gTSP 二进制文件](https://github.com/alexazhou/TSP/releases)（或进入 `gtsp/` 目录通过 `go build` 自行构建）。
2. **安装客户端**：安装对应语言的 SDK。目前官方提供 **Python** 支持（`pip install pytspclient`）。
3. **运行示例**：参考 `examples/` 目录下的示例代码即可快速启动你的 Agent。

> 💡 **提示**：目前主要提供 Python 示例和客户端。如果你有其他编程语言的需求，欢迎[发起 Issue](https://github.com/alexazhou/TSP/issues) 联系作者，或者直接提交 PR 贡献代码。

## 交流群

微信扫码加入 TSP 交流群：

![WeChat QR Code](image/wechat.JPG)

## License

MIT
