# TSP - Tool Service Protocol

[中文版Readme](README.md)  

---

**Build an autonomous agent in 10 lines of code:**

![Demo: Build an autonomous agent in 10 lines of code](image/demo.gif)

## Products Built with TSP

- [**TogoSpace**](https://github.com/alexazhou/TogoSpace) — Multi-Agent Collaboration, your AI teams, ready to go

## What is TSP?

It consists of two parts:

1. **TSP Protocol**: **Tool Service Protocol** defines a standardized protocol that completely decouples tool capabilities from agent business code, allowing the agent's "brain" and "hands" to be implemented independently.

2. **gTSP Implementation**: A high-quality tool service built according to the TSP protocol. Single-file, zero-dependency, cross-platform, comprehensively covering agent needs. Easily integrate it into your own applications to **build an autonomous agent in just 10 lines of code**, focusing on business logic instead of low-level tooling.

## Use Cases

TSP provides standardized execution capabilities for AI applications at different levels, primarily suitable for the following scenarios:

1. **Building Autonomous Agents with "Action Capabilities"**
   - Quickly develop tools like **coding assistants**, **automated DevOps tools**, or **data analysis bots**. TSP provides a ready-to-use set of "hands," enabling agents to operate file systems and shells directly like humans, completing the loop from planning to execution.

2. **Standardized Tool Layer for LLM Applications**
   - If your application already uses LLMs to process files or system tasks, you can **directly replace your custom tool logic** with TSP. This significantly reduces development and maintenance costs while providing more professional, secure (sandboxed), and high-performance tool implementations.

3. **Remote Machine Control in Business Systems**
   - Integrate TSP into enterprise-level systems as a standardized interface for agents or administrators to control machines remotely. Through protocol-based interaction, it not only simplifies cross-platform operations but also ensures security boundaries and traceability for remote actions.

## Why TSP?

### The m×n Problem

Without a standard protocol, every AI agent that needs system tools must implement them itself. If there are **m** agents and **n** tools, that's **m×n** independent implementations—each with its own development cost, bugs, and tool logic tightly coupled with agent code, making maintenance difficult.

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

- **`spec/`**: Protocol specifications and tool definition documentation.
- **`gtsp/`**: High-performance Go reference implementation (Server), single-file and zero-dependency.
- **`client/`**: Client SDKs for multiple languages (currently Python).
- **`examples/`**: Getting started examples and demo code, including the 10-line agent demo.
- **`tsp_gui_tester/`**: A GUI tool for visual testing and debugging of TSP servers.

## License

MIT
