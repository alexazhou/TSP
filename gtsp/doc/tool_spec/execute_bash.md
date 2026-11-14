# 工具规格：execute_bash (命令执行)

## 1. 功能目标
在受控的环境中执行系统命令。它不仅是执行编译、测试和部署任务的核心，也是代理在当前系统内进行探索和操作的万能钥匙。

## 2. 核心功能效果
*   **安全沙箱感**：虽然直接在系统上运行，但应具备超时控制（Timeout），防止失控的死循环进程。
*   **智能输出截断 (Critical)**：
    *   **字节限制**：如果命令产生巨大的日志或数据（如 `cat` 一个巨型文件），工具必须在达到阈值（如 50KB）时自动截断。
    *   **行数限制**：防止返回过长的单行文本，破坏终端显示或上下文。
*   **交互性感知**：明确告知代理哪些命令是交互式的（如 `vim`, `ssh`），并建议使用非交互模式（如 `--yes`, `-m` 等标记）。
*   **后台进程支持**：支持以分离模式运行长时间运行的服务（如启动开发服务器），并返回进程组 ID。
*   **存活后代进程报告**：命令退出后若有后代进程仍在运行，在返回值中明确报告并提供编号，处置权交给代理（见 §5）。

## 3. 参数定义 (JSON)
*   **`command`** (string): 要执行的 shell 命令。具体解释器按平台而定：Unix 上是 `bash -c`，Windows 上是 busybox `ash`（POSIX sh，不是 bash）。
*   **`task_timeout`** (integer, 可选): 命令执行的最大秒数，默认 180 秒。设为 0 或省略都使用默认值（注意：**不是** 0 = 立即后台）。
*   **`timeout_action`** (string, 可选): 超时后行为。`"background"`（默认）提升为后台进程；`"kill"` 终止进程组并返回部分输出。
*   **`run_in_background`** (boolean, 可选): 是否在后台运行。若为 `true`，工具启动命令后立即返回进程信息。
*   **`description`** (string, 可选): 对该操作的简要说明，增强操作的可审计性。

## 4. 返回值结构

### 4.1 命令执行完毕（常规）

```json
{
  "success": true,
  "stdout": "...",
  "stderr": "...",
  "exit_code": 0,
  "truncated": false
}
```

*   `success`：exit_code == 0。
*   `truncated`：stdout 或 stderr 因字节/行数限制被截断时为 `true`。
*   超时且 `timeout_action="kill"` 时，返回部分输出，`message` 为 `"Command timed out after Ns and was killed"`。

### 4.2 后台执行 / 超时转后台

```json
{
  "process_id": "proc_42",
  "status": "running",
  "stdout": "...（提升前已捕获的部分输出）",
  "stderr": "..."
}
```

后续用 `process_output`（可阻塞等待）拉取输出与退出状态，用 `process_stop` 终止。

## 5. 存活后代进程的报告（设计见根目录 `process_group_cleanup.md` §3.1）

命令自身退出后，如果它 fork 的后代进程仍在运行（如脚本里的 `python worker.py &`、daemon），工具**不会自动终止它们**，而是在常规返回值上追加报告信息，包含编号与**结构化进程树快照**：

```json
{
  "success": true,
  "stdout": "...",
  "stderr": "...",
  "exit_code": 0,
  "process_id": "proc_43",
  "message": "command exited but descendant process(es) are still running; use process_stop to terminate, or process_output to poll; unclaimed processes persist until server shutdown",
  "leftover_processes": {
    "alive": 2,
    "tree": {
      "pid": 41020,
      "ppid": 40001,
      "command": "bash test.sh",
      "alive": false,
      "children": [
        {
          "pid": 41027,
          "ppid": 41020,
          "command": "python worker.py",
          "alive": true,
          "children": []
        },
        {
          "pid": 41028,
          "ppid": 41020,
          "command": "tail -f app.log",
          "alive": true,
          "children": []
        }
      ]
    }
  }
}
```

字段说明：

*   `process_id`：后台进程注册表中为该命令分配的编号。该命令的**全部存活后代**与这个编号关联，用 `process_stop` 终止时会连同后代一起清理（按进程组 / Job 整树终止）。
*   `message`：人类可读的提示，说明后代仍在运行及可用的处置动作。
*   `leftover_processes`：遗留进程树快照。
    *   `alive`：存活后代数量（不含根节点）。
    *   `tree`：以**执行的 shell 命令**为根的树；根节点 `alive` 恒为 `false`（命令已退出，仅用于承载层级结构）。每个节点含 `pid`、`ppid`、`command`（启动命令行）、`alive`、`children`。
    *   父进程已退出、ppid 不在快照内的后代挂到根节点下（孤儿化的孙进程同样出现在树中，这正是报告的价值所在）。
    *   **快照语义**：这是返回时刻的一次性快照，快照之后进程可能退出或新起。需要最新状态时调 `process_output`（返回刷新后的树）。
*   若命令退出后没有存活后代，返回结构与 §4.1 完全一致，**不出现** `process_id`、`message`、`leftover_processes`。

### 5.1 处置方式

| 动作 | 工具 | 效果 |
|---|---|---|
| 终止全部后代 | `process_stop`（传入返回的 `process_id`） | 杀整组（含已脱离主 shell 的孙进程），与树中标注的存活节点无关——即使快照过期也幂等安全 |
| 查看最新状态与输出 | `process_output`（可阻塞等待） | 返回已捕获的累计输出 + 刷新后的进程树快照 |
| 不处置 | — | 后代继续运行，直到 gtsp 退出时统一清理；框架不按时间自动回收 |

### 5.2 边界与注意

*   **长驻服务优先用 `run_in_background`**：同步模式的后代报告是「事后告知」，`run_in_background` 是「事前声明」——后者启动即返回编号、不阻塞，语义更清晰。
*   **探测不到逃逸进程**：后代若主动 `setsid` 脱离进程组（正规 daemon 化），报告不出也无法组杀，与平台能力边界一致。
*   **输出语义**：检测到存活后代时，返回的 stdout/stderr 是调用结束时刻已捕获的部分（采集仍在后台继续，可用 `process_output` 续看）。
