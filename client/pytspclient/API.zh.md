# pyTSPClient API 参考

[English](API.md)

---

## 一、使用 Raw API

直接调用 TSP 工具，不涉及 LLM。

### 创建客户端

```python
from pytspclient import TSPClient

# 工厂方法创建实例
tsp = TSPClient.from_stdio("./gtsp")

# 链式调用：连接 + 初始化
await tsp.start()
```

### 调用工具

```python
# 读取文件
result = await tsp.call_tool("read_file", {"file_path": "hello.txt"})
print(result.output)

# 写入文件
await tsp.call_tool("write_file", {"file_path": "output.txt", "content": "Hello"})

# 执行命令
result = await tsp.call_tool("execute_bash", {"command": "ls -la"})
```

### 关闭连接

```python
await tsp.shutdown()
```

### TSPClient 方法

| 方法 | 说明 |
|------|------|
| `from_stdio(command)` | 工厂方法，从命令启动 TSP 服务 |
| `start()` | 连接 + 初始化，返回 self |
| `call_tool(name, params)` | 调用工具，返回 ToolResult |
| `shutdown()` | 关闭连接 |

### 属性

| 属性 | 说明 |
|------|------|
| `tools` | 工具 Schema 列表（Anthropic 格式） |
| `workdir` | TSP 工作目录 |

---

## 二、使用 Adapter

对接 LLM，让 Agent 自动调用工具。

### 创建 Adapter

```python
tsp = await TSPClient.from_stdio("./gtsp").start()

# OpenAI 格式
adapter = tsp.for_openai()

# Anthropic 格式
adapter = tsp.for_anthropic()
```

### 完整 Agent 示例

```python
from pytspclient import TSPClient
from openai import OpenAI

tsp = await TSPClient.from_stdio("./gtsp").start()
adapter = tsp.for_openai()
llm = OpenAI()
messages = [{"role": "system", "content": "You are a helpful assistant."}]

# 用户输入
messages.append({"role": "user", "content": "读取 hello.txt 文件"})

# Agent 循环
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

### Adapter 方法

| 方法 | 说明 |
|------|------|
| `tools` | 返回工具 Schema，直接传给 LLM |
| `parse_tool_calls(resp)` | 从 LLM 响应解析工具调用 |
| `execute_tool_calls(resp)` | 执行工具调用，返回结果 |
| `to_tool_messages(results)` | 将结果转换为 LLM 消息格式 |

---

## 数据类

### ToolResult

| 字段 | 说明 |
|------|------|
| `call_id` | 工具调用 ID |
| `name` | 工具名称 |
| `output` | 执行结果（JSON 字符串） |

### ToolCall

| 字段 | 说明 |
|------|------|
| `id` | LLM 分配的调用 ID |
| `name` | 工具名称 |
| `input` | 工具参数 |

---

## 异常

### TSPException

| 字段 | 说明 |
|------|------|
| `code` | 错误码（如 `resource/not_found`） |
| `message` | 错误信息 |