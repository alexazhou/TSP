# pyTSPClient

A lightweight Python client for the **Tool Server Protocol (TSP)**. It provides a simple, asynchronous way to interact with tool servers (like [gtsp](https://github.com/alexazhou/TSP/tree/master/gtsp)) via the `stdio` mode.

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
git clone https://github.com/alexazhou/TSP.git
cd TSP/client/pytspclient
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

- [English](API.md)
- [中文版](API.zh.md)

## License

MIT
