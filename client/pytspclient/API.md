# pyTSPClient API Reference

[中文版](API.zh.md)

## TSPClient

### `__init__(command: List[str], request_timeout_sec: int = 30)`
- `command`: The shell command to run the TSP server.
- `request_timeout_sec`: Timeout for each request.

### `async connect()`
Spawns the TSP server process and starts the internal read loops for `stdout` and `stderr`.

### `async disconnect()`
Forcefully terminates the process and fails all pending requests.

### `async initialize(...) -> TSPInitializeResult`
Handshake with the server. Protocol version is internally set to `0.3`.
Parameters:
- `client_info`: optional metadata about the client.
- `include`: optional list of tools to enable.
- `exclude`: optional list of tools to disable.

Returns a `TSPInitializeResult` object.

### `async tool(tool_name: str, input_params: Dict[str, Any]) -> Dict[str, Any]`
Executes a specific tool on the server.

### `async sandbox(config: Dict[str, Any]) -> Dict[str, Any]`
Configures the server's sandbox/workspace environment.

### `async shutdown()`
Sends a `shutdown` request and then calls `disconnect()`.

### `add_event_handler(handler: Callable[[TSPEvent], None])`
Registers a callback for server-sent events.
```python
def on_event(event: TSPEvent):
    print(f"Received event: {event.event} with data: {event.data}")

client.add_event_handler(on_event)
```

---

## Adapter (LLM Format Support)

pyTSPClient provides adapters to seamlessly integrate with different LLM API formats.

### `TSPClient.for_openai() -> OpenAIAdapter`
Returns an adapter for OpenAI-compatible APIs.

### `TSPClient.for_anthropic() -> AnthropicAdapter`
Returns an adapter for Anthropic APIs.

### LLMAdapter Methods

#### `tools: List[Dict[str, Any]]`
Returns the tool schemas in the format expected by the LLM API.

#### `parse_tool_calls(response: Any) -> List[ToolCall]`
Extracts tool calls from the LLM response into a unified format.

#### `execute_tool_calls(response: Any) -> List[ToolResult]`
Executes all tool calls from the response via TSP.

#### `to_tool_messages(results: List[ToolResult]) -> Any`
Converts tool results into messages format expected by the LLM API.

---

## Data Classes

### `TSPInitializeResult`
- `protocol_version`: str
- `capabilities`: Dict[str, Any]
- `server_info`: Dict[str, Any]

### `ToolCall`
- `id`: str — LLM assigned call ID
- `name`: str — tool name
- `input`: dict — tool parameters

### `ToolResult`
- `call_id`: str — corresponds to ToolCall.id
- `name`: str — tool name
- `output`: str — JSON string result

---

## Exceptions

### `TSPException`
Raised when the server returns an error response.
- `code`: The error code (e.g., `tsp/error`, `tool/not_found`).
- `message`: Human-readable error message.