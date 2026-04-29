# TSP - Tool Service Protocol

TSP 是一个工具服务协议，让任何程序都能快速获得强大的工具能力：文件操作、代码搜索、命令执行、进程管理等。

## 项目结构

```
TSP/
├── spec/              # 协议规范
│   ├── TSP.md         # 协议概述
│   ├── Protocol.md    # 协议详细规范
│   └── tools/         # 工具定义文档
│
├── gtsp/              # Go 实现
│   ├── src/           # Go 源码
│   ├── dist/          # 构建产物
│   └── README.md      # 使用说明
│
├── client/            # 客户端实现
│   └── pytspclient/   # Python 客户端
│       ├── pytspclient/
│       ├── examples/
│       └ README.md
│
└── tsp_gui_tester/    # GUI 测试工具
```

## 快速开始

### 使用 gtsp

```bash
# 下载二进制
cd gtsp/dist
./gtsp --help

# stdio 模式（默认）
./gtsp
```

### 使用 Python 客户端

```bash
pip install pytspclient

# 基础调用
from pytspclient import TSPClient

tsp = await TSPClient.from_stdio("gtsp").start()
result = await tsp.call_tool("read_file", {"file_path": "hello.txt"})
```

详见 [client/pytspclient/README.md](client/pytspclient/README.md)

## 协议特点

- **简洁**：10 行代码即可让 agent 获得完整工具能力
- **安全**：支持 sandbox 模式，限制文件访问范围
- **统一**：Anthropic 格式原生支持，OpenAI 格式自动转换
- **扩展**：支持 stdio、websocket 等多种传输模式

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

## 适用场景

1. **构建 AI Agent**：快速实现能自主行动的 agent
2. **自动化脚本**：统一的工具接口，无需重复实现
3. **开发辅助工具**：代码搜索、文件操作、命令执行
4. **测试与调试**：动态查看和修改文件

## License

MIT