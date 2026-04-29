# pyTSPClient

A lightweight Python client for the **Tool Server Protocol (TSP)**. It provides a simple, asynchronous way to interact with tool servers (like [gtsp](https://github.com/alexazhou/gTSP/tree/main/src/gtsp)) via the `stdio` mode.

## Features

- **Asynchronous IO**: Built with `asyncio`.
- **Full Protocol Support**: Methods for `initialize`, `tool`, `sandbox`, and `shutdown`.
- **Event Handling**: Easy subscription to server-sent events.
- **Error Handling**: Custom `TSPException` with error codes.
- **Logging**: Captures server `stderr` for easier debugging.

## Installation

### For Users
```bash
pip install pytspclient
```

### For Developers (Editable Mode)
If you are developing locally:
```bash
git clone https://github.com/alexazhou/gTSP.git
cd gTSP/pyTSPClient
pip install -e .
```

## Quick Start

```python
import asyncio
from pytspclient import TSPClient, TSPException

async def main():
    # command to launch the TSP server
    client = TSPClient(["./gtsp", "--mode", "stdio"])
    
    # 1. Connect (starts the subprocess)
    await client.connect()
    
    try:
        # 2. Initialize handshake
        # protocol_version is hardcoded to 0.3 internally
        init_data = await client.initialize(
            client_info={"name": "my-agent"},
            include=["read_file", "write_file"]
        )
        print(f"Connected to: {init_data.server_info.get('name')}")

        # 3. Call a tool
        try:
            result = await client.tool("read_file", {"file_path": "test.txt"})
            print(f"File content: {result['content']}")
        except TSPException as e:
            print(f"Tool failed: [{e.code}] {e.message}")

        # 4. Graceful Shutdown
        await client.shutdown()
        
    finally:
        # Ensure resources are cleaned up
        await client.disconnect()

if __name__ == "__main__":
    asyncio.run(main())
```

## API Reference

### `TSPClient`

#### `__init__(command: List[str], request_timeout_sec: int = 30)`
- `command`: The shell command to run the TSP server.
- `request_timeout_sec`: Timeout for each request.

#### `async connect()`
Spawns the TSP server process and starts the internal read loops for `stdout` and `stderr`.

#### `async disconnect()`
Forcefully terminates the process and fails all pending requests.

#### `async initialize(...) -> TSPInitializeResult`
Handshake with the server. Protocol version is internally set to `0.3`.
Parameters:
- `client_info`: optional metadata about the client.
- `include`: optional list of tools to enable.
- `exclude`: optional list of tools to disable.

Returns a `TSPInitializeResult` object.

#### `async tool(tool_name: str, input_params: Dict[str, Any]) -> Dict[str, Any]`
Executes a specific tool on the server.

#### `async sandbox(config: Dict[str, Any]) -> Dict[str, Any]`
Configures the server's sandbox/workspace environment.

#### `async shutdown()`
Sends a `shutdown` request and then calls `disconnect()`.

#### `add_event_handler(handler: Callable[[TSPEvent], None])`
Registers a callback for server-sent events.
```python
def on_event(event: TSPEvent):
    print(f"Received event: {event.event} with data: {event.data}")

client.add_event_handler(on_event)
```

### Data Classes

#### `TSPInitializeResult`
- `protocol_version`: str
- `capabilities`: Dict[str, Any]
- `server_info`: Dict[str, Any]

### Exceptions

#### `TSPException`
Raised when the server returns an error response.
- `code`: The error code (e.g., `tsp/error`, `tool/not_found`).
- `message`: Human-readable error message.

## License

MIT
