# API Reference

[中文版](API.zh.md)

---

## TSPClient

### Constructor

```python
TSPClient.from_stdio(command: str, request_timeout_sec: int = 30) -> TSPClient
```
Factory method. Start TSP server from command.

### Properties

```python
tsp.tools: List[dict]    # Tool schemas (Anthropic format)
tsp.workdir: str         # TSP working directory
```

### Methods

```python
await tsp.start() -> TSPClient
```
Connect + initialize. Returns self for chaining.

```python
await tsp.call_tool(name: str, input: dict) -> ToolResult
```
Execute tool on server.

```python
await tsp.shutdown()
```
Close connection gracefully.

---

## Adapter

Create adapter for LLM integration.

```python
tsp.for_openai() -> TspOpenAIAdapter
tsp.for_anthropic() -> TspAnthropicAdapter
```

### Methods

```python
adapter.tools: List[dict]
```
Tool schemas in LLM API format.

```python
adapter.parse_tool_calls(response) -> List[ToolCall]
```
Extract tool calls from LLM response.

```python
await adapter.execute_tool_calls(response) -> List[ToolResult]
```
Execute all tool calls via TSP.

```python
adapter.to_tool_messages(results) -> List[dict]
```
Convert results to LLM message format.

---

## Data Classes

### ToolResult

| Field | Type | Description |
|-------|------|-------------|
| `call_id` | str | Tool call ID |
| `name` | str | Tool name |
| `output` | str | Result (JSON string) |

### ToolCall

| Field | Type | Description |
|-------|------|-------------|
| `id` | str | LLM assigned ID |
| `name` | str | Tool name |
| `input` | dict | Tool parameters |

---

## Exceptions

### TSPException

| Field | Type | Description |
|-------|------|-------------|
| `code` | str | Error code (e.g. `resource/not_found`) |
| `message` | str | Error message |