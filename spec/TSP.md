# Tool Server Protocol (TSP)

**Version:** 0.3  |  **Reference Implementation:** [gTSP](https://github.com/alexazhou/gTSP)

---

Tool Server Protocol (TSP) defines a standard communication protocol for exposing local system operations — file I/O, shell execution, search — to AI agents and Large Language Models (LLMs).

TSP is inspired by the [Language Server Protocol (LSP)](https://microsoft.github.io/language-server-protocol/) created by Microsoft for VS Code. Both protocols follow the same architectural philosophy: **decouple the capability provider from the consumer through a well-defined, transport-agnostic protocol**.

In LSP, a code editor talks to a language server to get code intelligence (completion, diagnostics, rename, etc.). In TSP, an AI agent talks to a tool server to perform system operations (read files, run commands, search code, etc.).

```
┌──────────────────┐        TSP Messages (JSON)        ┌──────────────────┐
│    AI Agent      │  ──────────────────────────────►  │   Tool Server    │
│  (LLM / Host)    │  ◄──────────────────────────────  │(e.g. gtsp binary)│
└──────────────────┘    stdio  or  WebSocket           └──────────────────┘
```

## Why TSP?

### The m×n Problem

Without a standard protocol, every AI agent that needs system tools must implement those tools itself. If there are **m** agents and **n** tools, that is **m×n** independent implementations — each with its own bugs, security gaps, and maintenance burden.

```
Without TSP                       With TSP

Agent A ──► read_file (impl A)    Agent A ──┐
Agent A ──► exec_bash (impl A)              │
Agent A ──► list_dir  (impl A)    Agent B ──┼──► TSP Server ──► read_file
                                            │                ──► exec_bash
Agent B ──► read_file (impl B)    Agent C ──┘                ──► list_dir
Agent B ──► exec_bash (impl B)
Agent B ──► list_dir  (impl B)

Agent C ──► ...

m×n implementations               m+n implementations
```

TSP breaks the m×n matrix into **m+n**: each agent implements the TSP client protocol once, and each tool is implemented once in the server. This is exactly the same insight that motivated LSP — before LSP, every editor had to implement support for every language separately (m editors × n languages plugins); after LSP, each editor writes one LSP client and each language writes one LSP server.

| | Without standard protocol | With TSP |
|---|---|---|
| Integrations to build | m × n | m + n |
| Where security lives | Each agent (inconsistent) | TSP server (one place) |
| Tool schema format | Ad hoc per agent | Standardized, LLM-ready |
| Adding a new agent | Re-implement all n tools | Implement TSP client once |
| Adding a new tool | Update all m agents | Implement TSP tool once |

### Other Benefits

| Problem | TSP Solution |
|---|---|
| Every AI framework re-implements file/shell tooling | One standard server, any compatible client |
| Tool logic tangled with agent/reasoning logic | Clean protocol boundary separates concerns |
| Inconsistent security across implementations | Workspace sandbox is built into the protocol |
| Clients can't discover what tools are available | Schema delivered inline via `initialize` response |

## Comparison with LSP

| Aspect | LSP | TSP |
|---|---|---|
| Domain | Code intelligence | System operations |
| Consumer | Code editors (VS Code, Neovim, ...) | AI agents, LLM hosts |
| Transport | stdio (JSON-RPC 2.0) | stdio / WebSocket |
| Message format | JSON-RPC 2.0 | TSP JSON (inspired by JSON-RPC) |
| Capability discovery | `initialize` handshake | `initialize` handshake |
| Tool schema delivery | N/A | Inline in `initialize` response, ready for LLM registration |
| Server lifecycle signal | `initialized` notification | N/A (server accepts requests immediately after `initialize`) |
| Shutdown | `shutdown` request + `exit` notification | `shutdown` request + transport close |
| Typical operations | Hover, completion, goto-def | Read, write, execute, search, process management |
| Concurrency | Sequential (per-document) | Fully concurrent (per-request `id`) |

## Table of Contents

- [**Specification**](./Specification.md) — Base protocol, message format, transport layers, lifecycle, and tool invocation API
- [**Built-in Tools Reference**](./tools/README.md) — Complete reference for all tools shipped with gTSP
- [**gTSP Implementation Guide**](./impl/gtsp.md) — The reference implementation: a zero-dependency, cross-platform single-binary written in Go with minimal resource footprint.

## Protocol at a Glance

```
Client                                                    Server
  │                                                         │
  │                  (launch / connect)                     │
  │──────────────--────── initialize ──────────────────────►│
  │◄──────────────── result {tools, workdir} ───────────────│
  │                                                         │
  │              (register tools with LLM)                  │
  │                                                         │
  │── {"id":"1","method":"tool","tool":"read_file",...} ───►│
  │── {"id":"2","method":"tool","tool":"list_dir",...} ────►│  (concurrent)
  │                                                         │
  │◄────────────── {"id":"2","type":"result",...} ──────────│
  │◄────────────── {"id":"1","type":"result",...} ──────────│
  │                                                         │
  │──────────────────────- shutdown ───────────────────────►│
  │◄────────────────────── result {} ───────────────────────│  (cleanup done)
  │──────────────────── EOF / disconnect ──────────────────►│
  │                                                         │── exit
```
