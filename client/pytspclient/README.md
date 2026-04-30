# pyTSPClient

轻量级 Python 客户端，用于连接 TSP 工具服务器。

## 安装

```bash
pip install pytspclient
```

### 国内网络环境

如遇网络问题，可使用镜像源：

```bash
# pip 使用清华源
pip install pytspclient -i https://pypi.tuna.tsinghua.edu.cn/simple
```

构建 gtsp 时需设置 Go 代理：

```bash
# Go module 代理
export GOPROXY=https://goproxy.cn,direct

# Go 下载镜像（如需）
# macOS: https://golang.google.cn/dl/
# Linux: https://golang.google.cn/dl/
```

## 使用方式

### 一、Normal API — 普通场景使用

自己构建参数来调用工具。完整示例见 [demo_basic.py](../../examples/demo_basic.py)。

```python
from pytspclient import TSPClient, ToolCall

# 启动并初始化
tsp = await TSPClient.from_stdio("./gtsp").start()

# 获取 TSP 信息
print(tsp.tools)    # 工具 Schema 列表
print(tsp.workdir)  # TSP 工作目录

# 调用工具
call = ToolCall(name="read_file", input={"file_path": "hello.txt"})
result = await tsp.call_tool(call)
print(result.output)

# 关闭
await tsp.shutdown()
```

### 二、Adapter API — 直接对接 LLM 使用

使用 LLM 返回的对象，直接对接 TSP。完整示例见 [demo_agent.py](../../examples/demo_agent.py)。

```python
from pytspclient import TSPClient
from openai import OpenAI

tsp = await TSPClient.from_stdio("./gtsp").start()
adapter = tsp.for_openai()
llm = OpenAI()
messages = [{"role": "user", "content": "读取 hello.txt"}]

while True:
    resp = llm.chat.completions.create(model="gpt-4o", messages=messages, tools=adapter.tools)
    messages.append(resp.choices[0].message)

    if adapter.parse_tool_calls(resp):
        results = await adapter.execute_tool_calls(resp)
        messages.extend(adapter.to_tool_messages(results))
    else:
        print(resp.choices[0].message.content)
        break
```

## API 参考

- [API.md](API.md) — English
- [API.zh.md](API.zh.md) — 中文版

## License

MIT