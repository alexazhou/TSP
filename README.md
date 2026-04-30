# TSP - Tool Service Protocol

[English Readme](README.en.md) 

---

**演示：只需 10 行代码打造一个自主行动的 agent**

![演示10行代码打造一个自主行动的 agent](image/demo.gif)

## 什么是 TSP？

**TSP** 是 **Tool Service Protocol**（工具服务协议）的缩写。

1. 它定义了一种标准化协议，将工具能力和大模型推理完全解耦，使得 Agent 的“大脑”和“手”可以分别实现。
2. 同时提供一套标准、高效、跨平台的工具实现。**让你 10 行代码就可以完成一个自主行动的 Agent**，从而可以专注在 Agent 业务开发上，而不需要花时间自己从头构建底层工具。

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

## 适用场景

TSP 专注于 AI Agent 开发，让 Agent 能够自主执行系统操作，可用于开发：

- **代码助手 Agent**：读取代码文件、搜索函数定义、编辑代码、运行测试，实现完整的代码编写和调试闭环
- **数据分析 Agent**：读取数据文件、执行数据处理脚本、生成报告，自动化数据分析流程
- **运维 Agent**：执行部署命令、查看日志文件、管理进程，实现自动化运维操作
- **文档处理 Agent**：读取文档、批量编辑内容、生成新文档，自动化文档管理任务
- **通用任务 Agent**：根据用户指令自主规划步骤，调用工具完成任务，无需人工介入每个细节
- 等其他场景

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

```
TSP/
├── spec/              # 协议规范
│   ├── TSP.md         # 协议概述（英文）
│   ├── TSP.zh.md      # 协议概述（中文）
│   ├── Protocol.md    # 协议详细规范（英文）
│   ├── Protocol.zh.md # 协议详细规范（中文）
│   └── tools/         # 工具定义文档
│
├── gtsp/              # Go 实现（参考实现）
│   ├── src/           # Go 源码
│   ├── dist/          # 构建产物
│   └── README.md      # 使用说明
│
├── client/            # 客户端实现
│   └── pytspclient/   # Python 客户端
│
├── examples/          # 示例代码
│   ├── demo_basic.py
│   └── demo_agent.py
│
└── tsp_gui_tester/    # GUI 测试工具
```

## License

MIT