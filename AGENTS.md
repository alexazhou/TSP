# TSP 项目说明

本项目是 TSP (Tool Service Protocol) 协议的实现仓库。

## 项目结构

```
TSP/
├── spec/              # 协议规范（英文/中文）
├── gtsp/              # Go 语言参考实现
├── client/pytspclient/   # Python 客户端
├── examples/          # 示例代码
└── tsp_gui_tester/    # GUI 测试工具
```

## 关键文件

- `spec/TSP.md` / `spec/TSP.zh.md` — 协议概述
- `spec/Protocol.md` / `spec/Protocol.zh.md` — 详细协议规范
- `spec/tools/` — 各工具定义文档
- `gtsp/src/main.go` — Go 服务端实现（单文件）
- `client/pytspclient/` — Python 客户端

## 开发约定

- Go 服务端：单文件实现，位于 `gtsp/src/main.go`
- Python 客户端：支持 Anthropic/OpenAI 格式适配
- 提交规范：简洁的 commit message，不加 Co-Authored-By

## CI/CD

- 推送 `pytspclient-v*` tag 触发构建和发布
- 自动构建 gtsp（Linux/macOS/Windows）
- 自动发布 pytspclient 到 PyPI