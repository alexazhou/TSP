# execute_bash 超时机制改造方案

## 一、现状分析

### 1.1 当前超时架构

超时控制分布在两层，各自独立运作：

```
┌─────────────────────────────────────────────────────────┐
│  Agent (tspDriver.py)                                    │
│    pytspclient.call_tool()                               │
│      → request_timeout_sec = 65s  ← 客户端超时            │
│        → asyncio.wait_for(future, timeout=65s)           │
├─────────────────────────────────────────────────────────┤
│  gtsp 服务端 (execute_bash.go)                            │
│    execute_bash_handler()                                │
│      → 无全局超时，默认 WaitChan() 无限等待                 │
│      → task_timeout > 0 时才有限时逻辑                     │
└─────────────────────────────────────────────────────────┘
```

### 1.2 三种执行路径（gtsp 服务端）

```go
// 路径 1：run_in_background=true
Register(bp); return process_id     // 立即后台，有 process_id

// 路径 2：task_timeout > 0
select {
case <-bp.WaitChan(): return result   // 超时前完成，返回结果
case <-timer.C: Register(bp); return process_id  // 超时，提升后台
}

// 路径 3：都不传（默认）
<-bp.WaitChan(); return result        // 无限等待
```

### 1.3 存在的问题

| # | 问题 | 严重程度 |
|---|------|---------|
| 1 | **默认无超时保护**：不传 `task_timeout` 时 gtsp 无限等待，完全依赖客户端 65s 超时兜底 | 高 |
| 2 | **孤儿进程**：客户端超时后抛异常，但 gtsp 进程和子命令继续运行，Agent 无法获取结果也无法终止 | 高 |
| 3 | **process_output 受限**：`process_output` 也经过 pytspclient 调用，同样受 65s 客户端超时限制，长时间等待也会超时 | 中 |
| 4 | **文档与实现不符**：`task_timeout` 描述为"默认 10s"，实际代码中不传即为 0（无限等待） | 中 |
| 5 | **超时行为不可控**：超时后只能转后台，无法选择直接终止进程 | 低 |

### 1.4 超时行为实测记录

| 测试场景 | 预期 | 实际 |
|---------|------|------|
| 20s 命令，默认超时 | 10s 后提升后台 | 20s 后同步返回（无超时触发） |
| 80s 命令，默认超时 | 10s 后提升后台 | 65s 后 pytspclient 超时报错，进程继续运行 |
| 50s 命令，后台执行 | 立即返回 process_id | ✅ 正常，可通过 process_output 获取结果 |
| process_output 等待 90s | 等待 90s | 65s 后客户端超时（受 pytspclient 限制） |
| task_timeout=5，20s 命令 | 5s 后提升后台 | ✅ 正常，返回 process_id |

---

## 二、改造方案

### 2.1 核心原则

**服务端应先于客户端返回**：gtsp 的超时应小于 pytspclient 的 65s，确保客户端永远不会收到超时错误。

### 2.2 参数设计

#### 新增参数：`timeout_action`

控制超时后的行为，与 `task_timeout` 配合使用。

```json
{
  "command": "echo hello && sleep 120",
  "task_timeout": 60,
  "timeout_action": "background"
}
```

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `task_timeout` | int | 60 | 超时时间（秒）。0 表示使用默认值 60s |
| `timeout_action` | string | `"background"` | 超时后行为：`"background"` 提升为后台，`"kill"` 终止进程 |
| `run_in_background` | bool | false | 保留，等价于 `task_timeout: 0, timeout_action: "background"` 的语法糖 |

#### 参数组合与行为

| 场景 | 参数组合 | 行为 |
|------|---------|------|
| 立即后台执行 | `run_in_background: true` | 注册进程，立即返回 process_id |
| 限时等待，超时转后台 | `task_timeout: 60, timeout_action: "background"` | 等待完成返回结果；超时则注册后台返回 process_id |
| 限时等待，超时终止 | `task_timeout: 60, timeout_action: "kill"` | 等待完成返回结果；超时则杀掉进程，返回部分输出 |
| 全部默认 | 不传任何超时参数 | 使用默认 60s，超时转后台 |

### 2.3 gtsp 服务端改动（execute_bash.go）

```go
const defaultTaskTimeout = 60 // 默认超时 60 秒

type ExecuteBashParams struct {
    Command         string `json:"command"`
    TaskTimeout     int    `json:"task_timeout,omitempty"`      // 超时秒数，0 或不传使用默认值
    TimeoutAction   string `json:"timeout_action,omitempty"`    // "background"（默认）| "kill"
    RunInBackground bool   `json:"run_in_background,omitempty"` // 语法糖：立即后台
    Description     string `json:"description,omitempty"`
}

func ExecuteBashHandler(session api.Session, params json.RawMessage) (interface{}, error) {
    // ... 启动命令 ...

    // 路径 1：立即后台
    if p.RunInBackground {
        Register(bp)
        return BashBackgroundResult{ProcessID: bp.ID, Status: "running"}, nil
    }

    // 确定超时时间
    timeout := p.TaskTimeout
    if timeout <= 0 {
        timeout = defaultTaskTimeout
    }

    // 确定超时行为
    action := p.TimeoutAction
    if action == "" {
        action = "background"
    }

    // 路径 2：限时等待
    timer := time.NewTimer(time.Duration(timeout) * time.Second)
    defer timer.Stop()

    select {
    case <-bp.WaitChan():
        // 超时前完成
        return buildSyncResult(bp), nil
    case <-timer.C:
        // 超时
        switch action {
        case "kill":
            cmd.Process.Kill()
            <-bp.WaitChan() // 等待进程退出
            return buildSyncResult(bp), nil
        default: // "background"
            Register(bp)
            return BashBackgroundResult{
                ProcessID: bp.ID,
                Status:    "running",
                Stdout:    stdoutBuf.String(),
                Stderr:    stderrBuf.String(),
            }, nil
        }
    }
}
```

### 2.4 超时层级协调

改造后的超时层级：

```
┌──────────────────────────────────────────────┐
│  pytspclient 客户端                            │
│    request_timeout_sec = 65s  （兜底，正常不触发）│
├──────────────────────────────────────────────┤
│  gtsp 服务端                                   │
│    defaultTaskTimeout = 60s  （主控超时）        │
│    task_timeout 可自定义覆盖                     │
├──────────────────────────────────────────────┤
│  Agent 调用方                                   │
│    可通过 task_timeout 自定义更短的超时           │
└──────────────────────────────────────────────┘
```

时间线保证：
```
0s ──────────── 60s ──── 65s
      命令执行     gtsp超时   pytspclient超时（兜底，不应触发）
                    ↓
            返回结果或 process_id
```

### 2.5 同步修改：工具说明文档

更新 `execute_bash` 的 schema description：

```json
{
  "task_timeout": {
    "type": "integer",
    "description": "Optional: Timeout in seconds. Defaults to 60s if not specified or set to 0. When the command exceeds this timeout, the action taken depends on 'timeout_action'."
  },
  "timeout_action": {
    "type": "string",
    "enum": ["background", "kill"],
    "description": "Optional: Action to take when task_timeout expires. 'background' (default) promotes the process to background and returns a process_id. 'kill' terminates the process and returns partial output."
  },
  "run_in_background": {
    "type": "boolean",
    "description": "Optional: Start the process immediately in the background. Equivalent to task_timeout=0 with timeout_action='background'. Defaults to false."
  }
}
```

---

## 三、影响范围

| 组件 | 改动 | 风险 |
|------|------|------|
| gtsp `execute_bash.go` | 新增默认超时 + `timeout_action` 参数 | 低：向后兼容，不传新参数走默认行为 |
| gtsp 工具 schema | 更新 description | 无 |
| pytspclient | 不改动 | — |
| tspDriver.py | 不改动（65s 兜底仍有效） | — |
| 现有 Agent 调用 | 不受影响，行为改善（短命令无变化，长命令不再孤儿） | 低 |

## 四、后续可选优化

1. **pytspclient 增加连接状态检测**：超时后通知 gtsp 终止对应进程，避免真正的孤儿
2. **process_output 解耦客户端超时**：考虑 gtsp 直接通过事件推送进程完成状态，而非客户端轮询
3. **gtsp 增加进程清理机制**：定期检查已注册但长时间无客户端查询的后台进程，自动清理
