# Demo 示例

| 文件 | 说明 |
|------|------|
| `demo_basic.py` | 直接调用工具的基本用法 |
| `demo_agent.py` | 交互式 agent（LLM + 工具） |

## 准备 gTSP

运行 demo 前需要先准备好 gTSP：

**方式一：下载构建产物**
```bash
# 从 GitHub Release 下载对应平台的二进制
# https://github.com/alexazhou/TSP/releases
# macOS: gtsp/dist/gtsp-darwin-amd64
# Linux: gtsp/dist/gtsp-linux-amd64
# Windows: gtsp/dist/gtsp-windows-amd64.exe
```

**方式二：本地构建**
```bash
cd gtsp/src
go build -o gtsp main.go
```

## 安装与运行

```bash
pip install pytspclient openai

# 配置环境变量
export OPENAI_API_KEY=your-key
export OPENAI_BASE_URL=https://api.openai.com/v1  # 或其他兼容的 API 地址

# 将 gtsp 放到当前目录，或修改 demo 文件中的 GTSP_PATH 变量指向实际路径
python examples/demo_basic.py
python examples/demo_agent.py
```