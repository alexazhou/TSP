# TSP - Tool Service Protocol

[中文版](README.zh.md)  |  **Version:** 0.3  |  **Reference Implementation:** [gTSP](https://github.com/alexazhou/TSP/tree/master/gtsp)

---

TSP (Tool Service Protocol) defines a standard communication protocol for exposing local system operations (file I/O, shell execution, search, etc.) to AI agents and Large Language Models (LLMs).

This repository includes:
- **TSP Protocol Specification** — Protocol definition and detailed documentation (English/Chinese)
- **examples** — Build an autonomous agent in 10 lines of code, see [demo_agent.py](examples/demo_agent.py)
- **gTSP** — Go reference implementation, high-performance, single-file, zero-dependency, cross-platform
- **pytspclient** — Python client with Anthropic/OpenAI format adapters
- **tsp_gui_tester** — GUI testing tool for visual TSP service debugging

## Why TSP?

### The m×n Problem

Without a standard protocol, every AI agent that needs system tools must implement them itself. If there are **m** agents and **n** tools, that's **m×n** independent implementations—each with its own development cost, bugs, and difficulty achieving optimal quality.

```
Without TSP                       With TSP

Agent A ──► read_file (impl A)    Agent A ──┐
Agent A ──► exec_bash (impl A)              │
Agent A ──► list_dir  (impl A)    Agent B ──┼──► TSP Server ──► read_file
                                            |               ──► exec_bash
Agent B ──► read_file (impl B)    Agent C ──┘               ──► list_dir
Agent B ──► exec_bash (impl B)
Agent B ──► list_dir  (impl B)

Agent C ──► ...

m×n implementations               m+n implementations
```

TSP breaks the m×n matrix into **m+n**: each agent implements the TSP client protocol once, each tool is implemented once in the server, enabling well-designed and high-quality tool implementations.

### Other Benefits

| Problem | TSP Solution |
|---|---|
| Every AI framework re-implements file/shell tools | One standard server, any compatible client |
| Tool logic tangled with agent/reasoning logic | Clean protocol boundary separates concerns |
| Inconsistent security across implementations | Workspace sandbox built into the protocol |
| Clients can't discover available tools | Schema delivered via `initialize` response |

### Difference from MCP

In one sentence: **TSP builds Agents, MCP extends Agents**.

- **TSP** provides core system tools (file I/O, command execution, search, etc.), enabling agents with autonomous action capabilities—ideal for building a complete agent from scratch
- **MCP** provides external service integration (databases, APIs, third-party tools), extending existing agents with more capabilities—ideal for enhancing established agent systems

They work together: first build a general agent based on TSP, then add custom capabilities through MCP for personalized needs.

## Use Cases

TSP focuses on AI Agent development, enabling agents to autonomously execute system operations:

- **Coding Assistant Agent**: Read code files, search function definitions, edit code, run tests—complete coding and debugging loop
- **Data Analysis Agent**: Read data files, execute processing scripts, generate reports—automate data analysis workflows
- **Operations Agent**: Execute deployment commands, view logs, manage processes—automate operations tasks
- **Document Processing Agent**: Read documents, batch edit content, generate new documents—automate document management
- **General Task Agent**: Plan steps based on user instructions, call tools to complete tasks without manual intervention
- And other scenarios

## TSP Features

- **Simple & Easy**: Build an autonomous agent in 10 lines of code
- **Secure & Controllable**: Built-in sandbox mechanism to limit file access scope
- **Flexible Transport**: Supports stdio, WebSocket, and other transport modes
- **Ready to Use**: High-performance, cross-platform, zero-dependency Go server
- **Open & Customizable**: Fully open source, freely add custom tools

## Provided Tools

| Tool | Function |
|------|------|
| `list_dir` | List directory structure |
| `read_file` | Read file content |
| `write_file` | Write to file |
| `edit` | Exact string replacement in file |
| `grep_search` | Code search |
| `glob` | File name pattern matching |
| `execute_bash` | Execute shell commands |
| `process_*` | Process management |

See [spec/tools/](spec/tools/) for details.

## Project Structure

```
TSP/
├── spec/              # Protocol specification
│   ├── TSP.md         # Protocol overview (English)
│   ├── TSP.zh.md      # Protocol overview (Chinese)
│   ├── Protocol.md    # Detailed protocol (English)
│   ├── Protocol.zh.md # Detailed protocol (Chinese)
│   └── tools/         # Tool definition docs
│
├── gtsp/              # Go implementation (reference)
│   ├── src/           # Go source
│   ├── dist/          # Built binaries
│   └── README.md      # Usage guide
│
├── client/            # Client implementations
│   └── pytspclient/   # Python client
│
├── examples/          # Example code
│   ├── demo_basic.py
│   └── demo_agent.py
│
└── tsp_gui_tester/    # GUI testing tool
```

## License

MIT