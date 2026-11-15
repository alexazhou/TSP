# gtsp 工具描述评审：以 Agent 视角看「够不够理解工具行为」

> **评审人**：李哥（测试工程师）
> **撰写日期**：2026-11-15
> **评审对象**：**运行时实际注入给 Agent 的 tool description 字符串**（4 个工具：`execute_bash` / `process_list` / `process_output` / `process_stop`）
> **评审方法**：只以「Agent 手里拿到的描述文本」+「实机调用拿到的返回体」为证据，不参考实现代码。理由：Agent 做决策时**看不到代码**，描述里没写的信息 = 对使用者不存在。设计文档写了但描述没写，等于没写。
> **关联文档**：[`tool_spec/execute_bash.md`](../design/tool_spec/execute_bash.md)（工具规格）、[`child-process-reclamation.md`](../design/child-process-reclamation.md)（子进程回收机制）、[`process-output-optimization.md`](../design/process-output-optimization.md)（输出截断优化，本次不展开）

---

## 0. 一句话结论

主干能理解，但 **6 处描述与真实行为对不上**：其中 1 处（P0，进程组语义缺失）在 2026-11-15 直接导致我误判「gtsp 进程清理失效」并产出错误结论；另有 3 处（P1）是**返回体里有、描述里完全没有的字段/状态值**，使用者只能靠猜或靠偶然刷到。

---

## 1. 问题清单

| # | 工具 | 严重度 | 问题 | 后果 |
|---|---|---|---|---|
| 1 | `execute_bash` | **P0** | 未说明「后代检测与清理的单位是进程组」 | 用 `timeout`/`setsid` 等自建进程组的包装器时，后代完全隐形，会误判工具缺陷 |
| 2 | `process_list` | P1 | 未说明 `status` 有 `leftover` 取值；未说明已完成且无残留的条目会被过滤 | 看到 `leftover` 只能靠猜；「Lists all」与实际不符 |
| 3 | `process_output` | P1 | 返回体有 `truncated`、`leftover_processes`，描述未提；未提 `running` 在 leftover 时为 true；未提 `block` 会被残留拖住 | 排查残留的关键字段靠偶然发现；阻塞行为不可预期 |
| 4 | `process_stop` | P1 | 「已退出即 no-op」对 leftover 情形不成立；`message` 字段三种语义未提 | 以为无害，实际会杀残留并阻塞最多 2s |
| 5 | `execute_bash` | P1 | 截断只写「Automatically truncates」，未说是**保头丢尾** | 长构建的失败信息/产物路径在尾部被丢，容易得出「构建没问题」的错误判断 |
| 6 | `execute_bash` | P2 | `Working directory ... (execute_bash does not change it per call)` 表述绕 | 可被误读为「不要用 cd」，实际含义是「`cd` 不跨调用保留」 |

### 1.1 各条证据等级（区分「实测」与「读实现」）

写这份文档时仓库实现正在变动，故逐条标注证据来源，方便核对时知道哪几条需要重新实测：

| # | 证据 | 说明 |
|---|---|---|
| 1 | **实测** | `timeout 90 bash -c` 探针 + `sleep 60 &` 对照组，回收/告知两方向都跑过 |
| 2 | **半实测** | `status: "leftover"` 在返回体中实测出现过；「已完成且无残留的条目被过滤」是读实现得出的，未用「先跑完一条、再 process_list 看它是否消失」实测 |
| 3 | **半实测** | `truncated` / `leftover_processes` 两个 key 在返回体里实测出现；`running` 在 leftover 时为 true、`block` 被残留拖住**未实测**（读实现所得） |
| 4 | **未实测** | 三种 `message` 语义与「not found 走 success:false」均读实现所得，本次未逐一触发 |
| 5 | **实测** | PyInstaller 长输出被截断、尾部日志丢失真实发生；具体阈值（~1000 行 / 50KB）来自实现，属量级描述 |
| 6 | **主观** | 仅阅读体验，无行为事实 |

> 标注「未实测 / 读实现」的条目若与最新实现不符，以复核时的实测结果为准——但**无论实现怎么改，描述没写这些信息这件事本身是成立的**（描述文本可逐字核对）。

---

## 2. 逐条详述

### 2.1 【P0】`execute_bash`：进程组语义缺失（本次事故直接成因）

**描述现状**（原文）：
```
- Synchronous mode leftover descendants: if descendant processes forked by the command (e.g., '&' or daemons)
  are still running after the command exits, they are NOT automatically terminated; the result will include
  process_id, leftover_processes, and a message so you can terminate them with process_stop or inspect them
  with process_output (unclaimed processes persist until server shutdown)
```

**缺什么**：这段只说「有后代不会被自动终止、返回里会告诉你」，**没有说检测和 `process_stop` 的作用单位是进程组**。

**实测事故（2026-11-15）**：
- 我为了防忙等挂死，给每条命令套了 `timeout 90 bash -c '...'`。GNU `timeout` 会把自己和 COMMAND 放进**独占的新进程组**（`-f/--foreground` 才留在原组，代价是「children will not be timed out」）。
- 结果：后代逃出 gtsp 的进程组探测 ⇒ `leftover_processes` 不报、`process_stop` 也杀不到 ⇒ 我据此写下「gtsp 清理失效」的结论，写了半天报告，实际是**测试形态问题**。
- 同一份探针改成 `sleep 60 &`（不套 timeout）后，回收与告知**全部正常**：`process_stop` 整组 SIGKILL 后残留 0（含 `PPID=1` 的孤儿）；同步模式返回 `leftover_processes.alive=1` + `message`；异步模式 `process_list` 报 `status: "leftover"`。

**注意**：设计文档 [`tool_spec/execute_bash.md`](../design/tool_spec/execute_bash.md) §5.2 已经写了「探测不到逃逸进程：后代若主动 `setsid` 脱离进程组，报告不出也无法组杀」——**规格里有，注入给 Agent 的描述里没有**。这正是本条的价值：不是设计缺失，是对外契约披露缺失。

**建议补（可直接采用）**：
```
- Leftover detection and process_stop both act on the PROCESS GROUP, not on arbitrary descendants:
  wrappers that create their own process group/session (e.g. GNU `timeout`, `setsid`, some `nohup`/`sudo`
  combos) make their children invisible — no leftover_processes report, and process_stop cannot kill them.
  If you need a watchdog, keep it inside the same process group (e.g. `sleep N &` + explicit kill).
```

---

### 2.2 【P1】`process_list`：状态值域缺失 + 过滤规则与「Lists all」不符

**描述现状**：
```
- Lists all currently running background processes started by execute_bash
- Returns process_id, command, running_time, and status for each process
- Use this to check what background processes are active before calling process_output or process_stop
```

**问题 a**：`status` 有两个取值 `running` / `leftover`，描述只字未提 `leftover`。我实测拿到 `status: "leftover"` 时，只能反推它的含义（主体已退出、残留仍在）。这是**新增能力没有对外披露**（规格 [`tool_spec/execute_bash.md`](../design/tool_spec/execute_bash.md) §5 已覆盖 leftover 报告，但 `process_list` 这一侧的状态语义没写）。

**问题 b**：「Lists all currently running」不准确——主体已退出、无残留的条目**会被跳过**，即「列出仍在跑的 + 主体已退出但有残留的」，不是全部。

**建议补**：
```
- Lists background processes that are still running OR whose main shell has exited but descendants
  are still alive. Completed processes with no surviving descendants are omitted.
- status: "running" = main shell still running; "leftover" = main shell exited but descendants
  from the reported process tree are still alive (call process_stop to clean up).
```

---

### 2.3 【P1】`process_output`：两个返回字段缺失 + 两处反直觉语义未说明

**描述现状（Returns 部分）**：
```
- Returns stdout, stderr, running status, and exit code when complete
```

**问题 a**：返回体实际还有两个字段，描述完全没提：
- `truncated`（bool）——输出被截断。长构建场景下这是判断「我看到的是不是全量」的唯一依据。
- `leftover_processes`（object）——含 `alive` 计数 + 结构化进程树（每节点 `pid`/`ppid`/`command`/`alive`/`children`）。这是**排查残留的关键字段**，比 `process_list` 的 `status` 精确得多。

**问题 b**：`running` 的语义反直觉——**主体进程已退出、但有存活残留时，`running` 仍为 `true`**（`exit_code` 同时仍为 `null`）。直觉是「主体退出了就叫 finished」。

**问题 c**：`block: true` 时，若主体已退出但有残留，**会继续 poll 到残留退出或 `timeout` 耗尽**才返回。也就是说 `block` 会被 leftover 拖住，最长拖满 `timeout`。描述只说「wait for process completion」，读者会以为主体退出即返回。

**建议补**：
```
- Also returns:
  - truncated: true if stdout/stderr were clipped (see execute_bash output truncation)
  - leftover_processes: {alive, tree} when the main shell has exited but descendants are still alive
- running stays true (and exit_code null) while leftover descendants are alive, even after the main
  shell exits. With block:true, the call additionally waits for leftovers to exit or until timeout.
```

---

### 2.4 【P1】`process_stop`：leftover 情形下「no-op」的说法不成立

**描述现状**：
```
- Terminates a running background process
- If the process has already exited, this is a no-op and returns successfully
- Returns process_id, success (true/false), and exit_code
- Use this to clean up long-running background processes
```

**问题 a**：主体已退出但**有残留**时，它不是 no-op——会去杀残留并**最多等 2 秒**，再返回。

**问题 b**：`message` 字段在返回体里，但 Returns 里没写；而它恰好是区分三种情形的主要信号：

| `message` | 含义 | `success` |
|---|---|---|
| `process terminated` | 本次确实杀了（含「主体已退出、清理残留」情形） | true |
| `process already exited` | 真的无需做事 | true |
| `process kill timeout: did not terminate within 2 seconds` | 杀了但 2 秒内没死 | **false** |
| `process "<id>" not found` | 编号不存在（不是错误码，是 `success:false` 的返回体） | false |

最后一行值得特别注意：**「process not found」不是抛错，而是返回体里 `success:false`**——描述没写，容易被上层当成成功。

**建议补**：
```
- Returns process_id, success, exit_code, and message.
- On an already-exited process it also kills any surviving descendants (process group) and waits up
  to 2s; message tells which path was taken: "process terminated" / "process already exited" /
  "process kill timeout: did not terminate within 2 seconds" (success=false).
- An unknown process_id returns success=false with "process <id> not found" (not an error).
- Idempotent: calling again after cleanup is safe.
```

---

### 2.5 【P1】`execute_bash`：截断只写「会自动截断」，未说是保头丢尾

**描述现状**：
```
- Automatically truncates large outputs to protect context window
```

**问题**：实际是**保留开头、丢弃尾部**（我在 PyInstaller 构建时实测到约 1000 行 / 50KB 量级）。这条不是措辞小问题：长构建的**失败信息、产物路径都在尾部**，正好是被丢掉的那一段。使用者若不知道方向，就会拿着被截断的输出得出「构建通过/没报错」的错误结论。

**建议补**：
```
- Automatically truncates large outputs to protect context window: it KEEPS THE HEAD and DROPS THE TAIL
  (~1000 lines / 50KB); 'truncated' in the result flags it. For long builds, redirect to a log file
  and tail/grep it instead of relying on stdout.
```

（注：更彻底的解法在 [`process-output-optimization.md`](../design/process-output-optimization.md) 里，本次按要求不展开；此处只要求「把方向写清」。）

---

### 2.6 【P2】`execute_bash`：工作目录那句话的真实重点没说清

**描述现状**：
```
- Paths: Working directory: the directory gtsp was started from, or --workdir if given
  (execute_bash does not change it per call).
```

「does not change it per call」我第一遍读没抓住重点，且容易被读成「别用 `cd`」。真实含义是：**`cd` 的副作用不跨调用保留，每次调用都要在命令里自己 `cd`**。

**建议改为**：
```
- Paths: the working directory is where gtsp was started (or --workdir); a `cd` inside one call does
  NOT persist to the next call — prefix your command with `cd <dir> && ...` when needed.
```

---

## 3. 值得保留的写法（不要改回去）

- `process_output.timeout` 那条是全篇最好的描述：`Defaults to 30000, max 60000. Note: actual effective wait time is also limited by the client's transport request timeout (typically 30-65s); if you need to wait longer, use block:false and poll repeatedly.` —— 把「参数上限受外部约束」这个隐性事实写明了，我照着用没踩坑。
- `execute_bash` 三种执行模式的分段 + 「`run_in_background:true` 忽略 `task_timeout`/`timeout_action`」写得清楚，无歧义。
- `process_output` 里「`process_id` 只在转后台时返回，同步完成的命令没有」这条边界写得很好，直接避免了一次「拿不存在的 id 去查」。
- `task_timeout` 的「设为 0 表示用默认值，不是 0=立即后台」——括号里的澄清正是防止误用的范本。

---

## 4. 本次未评审的范围

- 仅覆盖「我这轮实际调用过的」4 个进程/执行类工具。`edit` / `read_file` / `write_file` / `glob` / `grep_search` / `list_dir` / `read_image` 未纳入（若需要全量评审请另行安排）。
- 未做平台差异（Windows/busybox）侧的实测；描述里关于 Windows 的语义（如「无 Unix 式进程组概念」）未验证。
- 未评审错误码体系（`ErrNotFound` 等以错误形式 vs 以返回体形式上报的不一致，见 §2.4 最后一行）。

---

## 5. 复验方法（改完描述后怎么确认）

1. **披露完整性**：把改后的 description 单独拿出来，让一个不看代码的人（或另一个 Agent）回答三个问题——「timeout 包装器里的后代能被清理吗」「主体退出后有残留时 running 是什么」「process_stop 对不存在的 id 会怎样」。答不上来即说明描述仍有缺口。
2. **字段一致性**：对同一组场景各跑一次，逐字段比对「返回体实际出现的 key 集合」与「描述里列出的 key 集合」，要求使用方可见的字段 100% 在描述中出现。
3. **行为一致性**：`leftover` 场景必须覆盖三种触发方式——同步模式留后代、异步模式主体退出留后代、`process_stop` 清理残留——逐一核对 `running` / `exit_code` / `message` 与描述相符。
