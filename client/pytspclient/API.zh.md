# API 参考

[English](API.md)

---

## TSPClient

### 构造函数

工厂方法。从命令启动 TSP 服务器。
```python
TSPClient.from_stdio(command: str, request_timeout_sec: int = 30) -> TSPClient
```

### 属性

```python
tsp.tools: List[TSPTool]    # 工具定义列表
tsp.workdir: str            # TSP 工作目录
```

### 方法

连接 + 初始化。返回 self 支持链式调用。
```python
await tsp.start() -> TSPClient
```

执行工具调用。
```python
call = ToolCall(name="read_file", input={"file_path": "hello.txt"})
result = await tsp.call_tool(call) -> ToolResult
```

优雅关闭连接。
```python
await tsp.shutdown()
```

---

## Adapter

创建适配器对接 LLM。
```python
tsp.for_openai() -> TspOpenAIAdapter
tsp.for_anthropic() -> TspAnthropicAdapter
```

### 方法

LLM API 格式的工具 Schema。
```python
adapter.tools: List[dict]
```

从 LLM 响应解析工具调用。
```python
adapter.parse_tool_calls(response) -> List[ToolCall]  # response: OpenAI ChatCompletion | Anthropic Message
```

通过 TSP 执行所有工具调用。
```python
await adapter.execute_tool_calls(response) -> List[ToolResult]
```

将结果转换为 LLM 消息格式。
```python
adapter.to_tool_messages(results) -> List[dict]
```

---

## 数据类

### TSPTool

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | str | 工具名称 |
| `description` | str | 工具描述 |
| `input_schema` | dict | 输入参数 Schema |

### ToolResult

| 字段 | 类型 | 说明 |
|------|------|------|
| `call_id` | str | 工具调用 ID |
| `name` | str | 工具名称 |
| `output` | str | 结果（JSON 字符串） |

### ToolCall

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | str | 工具名称 |
| `input` | dict | 工具参数 |
| `id` | str | 调用 ID（可选，用于关联结果） |

---

## 异常

### TSPException

| 字段 | 类型 | 说明 |
|------|------|------|
| `code` | str | 错误码（如 `resource/not_found`） |
| `message` | str | 错误信息 |