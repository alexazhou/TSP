# pytspclient — TSP 客户端 + LLM Adapter

## 核心设计

**两层分离**：
- `TSPClient` 只懂 TSP 协议，不涉及 LLM 格式
- `Adapter` 只懂 LLM 格式，屏蔽 API 差异

**中间类型**：
- `ToolCall`：统一的工具调用格式（id, name, input）
- `ToolResult`：统一的工具结果格式（call_id, name, output）

这使得 `execute_tool_calls` 在基类实现一次，所有 Adapter 共享。

## Demo

| 文件 | 说明 |
|------|------|
| `demo_basic.py` | 直接调用工具的基本用法 |
| `demo_agent.py` | 交互式 agent（LLM + 工具） |

## 安装与运行

```bash
pip install pytspclient openai
export OPENAI_API_KEY=your-key
export GTSP_PATH=/path/to/gtsp

python examples/demo_basic.py
python examples/demo_agent.py
```

## API

### 创建客户端

```python
# stdio 模式（启动子进程）
tsp = TSPClient.from_stdio("gtsp")

# websocket 模式（连接远程服务，暂未实现）
tsp = TSPClient.from_websocket("ws://localhost:8080/tsp")
```

### 基础调用

```python
tsp = await TSPClient.from_stdio("gtsp").start()

result = await tsp.call_tool("read_file", {"file_path": "hello.txt"})
print(result.output)
```

### 接 LLM

```python
tsp = await TSPClient.from_stdio("gtsp").start()
adapter = tsp.for_openai()

while True:
    resp = llm.chat.completions.create(model="gpt-4o", messages=messages, tools=adapter.tools)
    messages.append(resp.choices[0].message)

    calls = adapter.parse_tool_calls(resp)
    if calls:
        results = await adapter.execute_tool_calls(resp)
        messages.extend(adapter.to_tool_messages(results))
    else:
        print(resp.choices[0].message.content)
        break
```

### 属性

```python
tsp.tools     # 原始 TSP schema（Anthropic 格式）
tsp.workdir   # TSP 工作目录
```

## 扩展新 LLM

继承 `LLMAdapter`，实现 5 个方法：`tools`、`parse_tool_calls`、`get_text`、`to_tool_messages`。