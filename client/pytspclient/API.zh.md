# pyTSPClient API 参考

[English](API.md)

## TSPClient

### `__init__(command: List[str], request_timeout_sec: int = 30)`
- `command`: 启动 TSP 服务器的 shell 命令。
- `request_timeout_sec`: 每个请求的超时时间。

### `async connect()`
启动 TSP 服务器进程，并启动内部读取循环处理 `stdout` 和 `stderr`。

### `async disconnect()`
强制终止进程并使所有待处理请求失败。

### `async initialize(...) -> TSPInitializeResult`
与服务器握手。协议版本内部设置为 `0.3`。
参数：
- `client_info`: 可选的客户端元数据。
- `include`: 可选的启用工具列表。
- `exclude`: 可选的禁用工具列表。

返回 `TSPInitializeResult` 对象。

### `async tool(tool_name: str, input_params: Dict[str, Any]) -> Dict[str, Any]`
在服务器上执行指定工具。

### `async sandbox(config: Dict[str, Any]) -> Dict[str, Any]`
配置服务器的沙箱/工作区环境。

### `async shutdown()`
发送 `shutdown` 请求，然后调用 `disconnect()`。

### `add_event_handler(handler: Callable[[TSPEvent], None])`
注册服务器发送事件的回调函数。
```python
def on_event(event: TSPEvent):
    print(f"收到事件: {event.event}，数据: {event.data}")

client.add_event_handler(on_event)
```

---

## Adapter（LLM 格式支持）

pyTSPClient 提供适配器，无缝对接不同的 LLM API 格式。

### `TSPClient.for_openai() -> OpenAIAdapter`
返回 OpenAI 兼容 API 的适配器。

### `TSPClient.for_anthropic() -> AnthropicAdapter`
返回 Anthropic API 的适配器。

### LLMAdapter 方法

#### `tools: List[Dict[str, Any]]`
返回 LLM API 期望格式的工具 Schema。

#### `parse_tool_calls(response: Any) -> List[ToolCall]`
从 LLM 响应中提取工具调用，转换为统一格式。

#### `execute_tool_calls(response: Any) -> List[ToolResult]`
通过 TSP 执行响应中的所有工具调用。

#### `to_tool_messages(results: List[ToolResult]) -> Any`
将工具结果转换为 LLM API 期望的消息格式。

---

## 数据类

### `TSPInitializeResult`
- `protocol_version`: str
- `capabilities`: Dict[str, Any]
- `server_info`: Dict[str, Any]

### `ToolCall`
- `id`: str — LLM 分配的调用 ID
- `name`: str — 工具名称
- `input`: dict — 工具参数

### `ToolResult`
- `call_id`: str — 对应 ToolCall.id
- `name`: str — 工具名称
- `output`: str — JSON 字符串结果

---

## 异常

### `TSPException`
服务器返回错误响应时抛出。
- `code`: 错误码（如 `tsp/error`、`tool/not_found`）。
- `message`: 人类可读的错误信息。