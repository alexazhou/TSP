# API 参考

[English](API.md)

---

## TSPClient

### 构造函数

```python
TSPClient.from_stdio(command: str, request_timeout_sec: int = 30) -> TSPClient
```
工厂方法。从命令启动 TSP 服务器。

### 属性

```python
tsp.tools: List[TSPTool]    # TSP 工具定义列表
tsp.workdir: str                      # TSP 工作目录
```

### 方法

```python
await tsp.start() -> TSPClient
```
连接 + 初始化。返回 self 支持链式调用。

```python
await tsp.call_tool(name: str, input: dict) -> ToolResult
```
在服务器上执行工具。

```python
await tsp.shutdown()
```
优雅关闭连接。

---

## Adapter

创建适配器对接 LLM。

```python
tsp.for_openai() -> TspOpenAIAdapter
tsp.for_anthropic() -> TspAnthropicAdapter
```

### 方法

```python
adapter.tools: List[dict]
```
LLM API 格式的工具 Schema。

```python
adapter.parse_tool_calls(response) -> List[ToolCall]
```
从 LLM 响应解析工具调用。

```python
await adapter.execute_tool_calls(response) -> List[ToolResult]
```
通过 TSP 执行所有工具调用。

```python
adapter.to_tool_messages(results) -> List[dict]
```
将结果转换为 LLM 消息格式。

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
| `id` | str | LLM 分配的 ID |
| `name` | str | 工具名称 |
| `input` | dict | 工具参数 |

---

## 异常

### TSPException

| 字段 | 类型 | 说明 |
|------|------|------|
| `code` | str | 错误码（如 `resource/not_found`） |
| `message` | str | 错误信息 |