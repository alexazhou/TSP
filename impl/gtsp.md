# gTSP — Reference Implementation

**gTSP** is the reference implementation of the [Tool Server Protocol](../Specification.md), written in Go.

| | |
|---|---|
| **Zero dependencies** | No runtime libraries, package managers, or interpreters required. Drop the binary and run. |
| **Single binary** | The entire server ships as one self-contained executable. No install step, no config files needed to get started. |
| **Cross-platform** | Builds and runs on Linux, macOS, and Windows from a single codebase. |
| **Low resource footprint** | Minimal memory and CPU usage at idle. Suitable for embedding in resource-constrained environments or running alongside other processes. |

This document covers gTSP-specific details: CLI usage, startup flags, and offline tooling. For the protocol itself, see the [TSP Specification](../Specification.md).

---

## Installation

### Build from source

```sh
git clone https://github.com/alexazhou/gTSP
cd gTSP
./script/build.sh
# Binary: ./dist/gtsp
```

### Direct compile

```sh
go build -o gtsp src/main.go
```

---

## CLI Reference

```
Usage: gtsp [options] [command]

Commands:
  schema [-o file]       Print tool schemas to stdout (or file)

Options:
  -h, --help             Show this help message
  -v, --version          Show version
  --mode string          Transport mode: stdio | websocket  (default: stdio)
  --port int             WebSocket server port (required for websocket mode)
  --workspace path       Restrict all file operations to this directory
                         (default: current working directory)
  --log-path path        Directory to write log files (default: <binary-dir>/logs)
```

---

## Quick Start

### 1. Start the server

```sh
./gtsp --workspace /path/to/project
```

The server is ready immediately. Send an `initialize` request:

### 2. Initialize and discover tools

Send an `initialize` request. The response contains tool schemas ready to register with an LLM:

```json
{"id":"init-1","method":"initialize","input":{"protocolVersion":"0.3","clientInfo":{"name":"my-agent"}}}
```

```json
{"id":"init-1","type":"result","result":{"protocolVersion":"0.3","serverInfo":{"name":"gTSP","version":"v0.3.0"},"capabilities":{"tools":[...]},"workdir":"/path/to/project"}}
```

### 3. Invoke a tool

```sh
echo '{"id":"1","method":"tool","tool":"list_dir","input":{"dir_path":"."}}' | ./gtsp
```

### 4. Shut down gracefully

```json
{"id":"bye","method":"shutdown","input":{}}
```

The server cleans up resources and responds with `{}`, then the client closes the transport.

---

## Running the Server

### stdio mode (default)

The agent launches `gtsp` as a child process. Communication happens over stdin/stdout.

```sh
./gtsp --workspace /path/to/project
```

The server is ready immediately after launch. The agent sends an `initialize` request (see [TSP Specification §3.1](../Specification.md#31-initialize-request)).

### WebSocket mode

```sh
./gtsp --mode websocket --port 9000 --workspace /path/to/project
```

Each new connection must send `initialize` before invoking tools. Multiple agents can connect to the same server instance concurrently.

---

## Offline Schema Introspection

gTSP provides a `schema` subcommand that prints all registered tool definitions without starting the server in listen mode. This is useful for:

- Inspecting what tools are available before writing integration code
- Generating documentation
- CI pipelines that need tool schemas as build artifacts

```sh
# Print JSON array to stdout
./gtsp schema

# Write to a file
./gtsp schema -o tools.json
```

The output is a JSON array of `ToolDefinition` objects — the same format returned in `capabilities.tools` of an `initialize` response, but with no capability filter applied.

> This is a gTSP convenience feature. The TSP protocol does not define a schema subcommand; schemas are obtained at runtime via the `initialize` handshake (see [TSP Specification §4.1](../Specification.md#41-tool-definition)).

---

## Logging

gTSP writes structured logs to files, not to stderr, so that stderr stays clean for the parent process.

| Flag | Default | Description |
|---|---|---|
| `--log-path` | `<binary-dir>/logs/` | Directory where log files are written |

Log files are named by date: `gtsp-2024-01-15.log`. Each line is a timestamped entry from the Go standard `log` package.

To suppress logs entirely, point `--log-path` at `/dev/null` (Unix) or `NUL` (Windows).

---

## Workspace Security

All file-system tools enforce a workspace boundary set by `--workspace`. Paths outside the workspace root are rejected before any I/O is attempted. See [TSP Specification §5](../Specification.md#5-security-model) for the full path validation rules.

```sh
# Only files under /home/user/project are accessible
./gtsp --workspace /home/user/project
```

If `--workspace` is omitted, the process's current working directory at startup is used.

---

## Version

```sh
./gtsp --version
# v0.3.0
```

The version string follows [Semantic Versioning](https://semver.org/) and matches the `serverInfo.version` field in the `initialize` response.
