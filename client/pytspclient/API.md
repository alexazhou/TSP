# API Reference

[中文版](API.zh.md)

---

## TSPClient

### Constructor

Factory method. Start TSP server from command.
```python
TSPClient.from_stdio(command: str, request_timeout_sec: int = 30) -> TSPClient
```

### Properties

```python
tsp.tools: List[TSPTool]    # Tool definitions
tsp.workdir: str            # TSP working directory
```

### Methods

Connect + initialize. Returns self for chaining.
```python
await tsp.start() -> TSPClient
```

Execute tool call.
```python
call = ToolCall(name="read_file", input={"file_path": "hello.txt"})
result = await tsp.call_tool(call) -> ToolResult
```

Close connection gracefully.
```python
await tsp.shutdown()
```

---

## Adapter

Create adapter for LLM integration.
```python
tsp.for_openai() -> TspOpenAIAdapter
tsp.for_anthropic() -> TspAnthropicAdapter
```

### Methods

Tool schemas in LLM API format.
```python
adapter.tools: List[dict]
```

Extract tool calls from LLM response.
```python
adapter.parse_tool_calls(response) -> List[ToolCall]  # response: OpenAI ChatCompletion | Anthropic Message
```

Execute all tool calls via TSP.
```python
await adapter.execute_tool_calls(response) -> List[ToolResult]
```

Convert results to LLM message format.
```python
adapter.to_tool_messages(results) -> List[dict]
```

---

## Data Classes

### TSPTool

| Field | Type | Description |
|-------|------|-------------|
| `name` | str | Tool name |
| `description` | str | Tool description |
| `input_schema` | dict | Input parameters schema |

### ToolResult

| Field | Type | Description |
|-------|------|-------------|
| `call_id` | str | Tool call ID |
| `name` | str | Tool name |
| `output` | str | Result (JSON string) |

### ToolCall

| Field | Type | Description |
|-------|------|-------------|
| `name` | str | Tool name |
| `input` | dict | Tool parameters |
| `id` | str | Call ID (optional, for result correlation) |

---

## Exceptions

### TSPException

| Field | Type | Description |
|-------|------|-------------|
| `code` | str | Error code (e.g. `resource/not_found`) |
| `message` | str | Error message |