# pyTSPClient

轻量级 Python 客户端，用于连接 TSP 工具服务器。

## 安装

```bash
pip install pytspclient
```

## 使用方式

### 一、Raw API — 直接调用工具

不涉及 LLM，直接调用 TSP 工具。

```python
from pytspclient import TSPClient

# 启动并初始化
tsp = await TSPClient.from_stdio("./gtsp").start()

# 调用工具
result = await tsp.call_tool("read_file", {"file_path": "hello.txt"})
print(result.output)

# 关闭
await tsp.shutdown()
```

### 二、Adapter — 对接 LLM Agent

让 LLM 自动调用工具。

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