# pyTSPClient API Reference

[中文版](API.zh.md)

---

## 1. Using Raw API

Call TSP tools directly, without LLM.

### Create Client

```python
from pytspclient import TSPClient

# Factory method
tsp = TSPClient.from_stdio("./gtsp")

# Chain call: connect + initialize
await tsp.start()
```

### Call Tools

```python
# Read file
result = await tsp.call_tool("read_file", {"file_path": "hello.txt"})
print(result.output)

# Write file
await tsp.call_tool("write_file", {"file_path": "output.txt", "content": "Hello"})

# Execute command
result = await tsp.call_tool("execute_bash", {"command": "ls -la"})
```

### Close Connection

```python
await tsp.shutdown()
```

### TSPClient Methods

| Method | Description |
|------|------|
| `from_stdio(command)` | Factory method, start TSP server from command |
| `start()` | Connect + initialize, returns self |
| `call_tool(name, params)` | Call tool, returns ToolResult |
| `shutdown()` | Close connection |

### Properties

| Property | Description |
|------|------|
| `tools` | Tool schema list (Anthropic format) |
| `workdir` | TSP working directory |

---

## 2. Using Adapter

Integrate with LLM, let Agent call tools automatically.

### Create Adapter

```python
tsp = await TSPClient.from_stdio("./gtsp").start()

# OpenAI format
adapter = tsp.for_openai()

# Anthropic format
adapter = tsp.for_anthropic()
```

### Full Agent Example

```python
from pytspclient import TSPClient
from openai import OpenAI

tsp = await TSPClient.from_stdio("./gtsp").start()
adapter = tsp.for_openai()
llm = OpenAI()
messages = [{"role": "system", "content": "You are a helpful assistant."}]

# User input
messages.append({"role": "user", "content": "Read hello.txt file"})

# Agent loop
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

### Adapter Methods

| Method | Description |
|------|------|
| `tools` | Return tool schema, pass directly to LLM |
| `parse_tool_calls(resp)` | Parse tool calls from LLM response |
| `execute_tool_calls(resp)` | Execute tool calls, return results |
| `to_tool_messages(results)` | Convert results to LLM message format |

---

## Data Classes

### ToolResult

| Field | Description |
|------|------|
| `call_id` | Tool call ID |
| `name` | Tool name |
| `output` | Execution result (JSON string) |

### ToolCall

| Field | Description |
|------|------|
| `id` | LLM assigned call ID |
| `name` | Tool name |
| `input` | Tool parameters |

---

## Exceptions

### TSPException

| Field | Description |
|------|------|
| `code` | Error code (e.g. `resource/not_found`) |
| `message` | Error message |