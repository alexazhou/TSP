# gtsp 工具输出截断与 `process_output` 增量读取优化方案

> 状态：终稿（简化架构 + 面向后续文件缓冲演进）
> 涉及模块：`gtsp/src/api/process.go`、`gtsp/src/tools/execute_bash.go`、`gtsp/src/tools/process_output.go`、`spec/tools/execute_bash.md`、`spec/tools/process_output.md`

---

## 1. 背景与问题定位

在执行长耗时、高输出命令（如 PyInstaller、Cargo、Tauri 构建或全量测试）时，Agent 调用 `execute_bash` + `process_output` 会同时遇到**输出被截断看不到结果**和**轮询反复拉取大段重复输出**的问题。

结合当前代码实现，定位到 4 个核心问题：

1. **缓冲层写满即丢弃新数据（`gtsp/src/api/process.go`）**：`ProcBuffer` 上限 `256 KB`，写满后直接丢弃后续所有新写入，导致构建/测试的尾部结果与错误栈在源头永久丢失。
2. **截断策略为“纯保头丢尾”，且存在 UTF-8 截断风险（`gtsp/src/tools/execute_bash.go`）**：`truncateOutput()` 只保留前 `1000` 行 / `50 KB`，且 `s[:maxOutputBytes]` 直接按字节切片可能切断多字节 UTF-8 字符。
3. **`process_output` 每次返回全量历史，不支持增量或尾部/状态查询（`gtsp/src/tools/process_output.go`）**：每次轮询都重拉全量，逼迫 Agent 用 `sleep + pgrep` 绕开 `process_output`。
4. **`execute_bash` 超时转后台时未做输出截断（`gtsp/src/tools/execute_bash.go`）**：`BashBackgroundResult` 直接返回原始缓冲区内容，单次最高可能吐出 512 KB。

---

## 2. 核心设计思想：统一连续区间 + 按需有界窗口接口

本方案围绕 Phase 1 的**单一连续 `512 KB` 内存环形缓冲**展开（去掉复杂的双段 `head + gap + ring` 模型），并在接口设计上天然兼容后续 Phase 2 文件缓冲演进：

1. **消灭 gap 状态机**：Phase 1 采用单一连续环形缓冲 `[startOffset, totalBytes)`（其中 `startOffset = max(0, totalBytes - 512KB)`），与天然连续的文件流模型同构，不存在中间空洞（gap），代码与测试复杂度直接减半。
2. **固定单次分配开销为 `≤ 100 KB`**：若每次 `process_output` 轮询都把 `512 KB` 环形缓冲全量拷出成 `string` 再截断，双流每次轮询就是 ~1 MB 内存分配。通过暴露**有界窗口读取接口**（`ReadFrom`、`ReadHeadTail`、`ReadTail`、`Stats`），每次调用只拷贝真正需要返回的窗口（`≤ 100 KB`，`include_output: false` 更是零拷贝）；且这套接口日后切到文件缓冲（`ReadAt`/`Seek`）时同样无需修改上层代码。

---

## 3. 详细设计方案

### 3.1 缓冲层抽象：`ProcBuffer`

#### Phase 1 实现（单一连续 `512 KB` 内存环形缓冲，渐进增长）
- 常量：`DefaultProcRingBytes = 512 * 1024`（`512 KB`，支持通过 `ProcBuffer.MaxBytes` 在测试中覆盖）。
- 字段：
  - `MaxBytes int`（`<= 0` 时使用 `DefaultProcRingBytes`）
  - `totalBytes int64`：累计写入总字节数（单调递增）
  - `buf []byte`：初始为 `nil`，按需 `append` 渐进增长至 `cap == maxBytes` 后转为环形覆写（`head` 维护逻辑起点）
- 可用区间：**永远是单一连续区间 `[startOffset, totalBytes)`**，其中 `startOffset = max(0, totalBytes - int64(maxBytes))`，`overflow = startOffset > 0`。

#### `Write(p []byte)` 与 `Close()` 契约
- **`Write(p []byte)`**：永远返回 `len(p), nil`（绝不短写或报错），确保 `io.Copy` 持续排空子进程管道。当单次 `len(p) > maxBytes` 时，不分配超过 `maxBytes` 的内存，只保留 `p[len(p)-maxBytes:]` 并更新 `totalBytes += int64(len(p))`。
- **`Close()`**：释放资源钩子（Phase 1 为 no-op；Phase 2 用于关闭并删除临时日志文件，由 `ProcessRegistry` reaper / `KillSession` 在移除条目时调用）。

#### 有界窗口读取接口（只取窗口、不做截断策略；锁内原子拷贝，单次分配 `≤ 100 KB`）

1. **`ReadFrom(offset int64, maxReadBytes int) ReadSliceResult`**（供显式 `offset` 增量分页；非 rune 对齐的 `offset` 会被对齐到下一字符边界，跳过的字节统一计入 `skippedBytes`）：
   - `offset >= totalBytes`：返回 `data = nil`、`nextBaseOffset = offset`（**原样回显，避免游标倒退**）、`offsetExpired = false`、`skippedBytes = 0`。
   - `offset < startOffset`（**唯一过期条件**）：置 `offsetExpired = true`；起点从 `startOffset` 开始（做 `utf8Ceil` 对齐得 `nextBaseOffset`，并将覆盖与对齐跳过的总字节数计入 `skippedBytes = nextBaseOffset - offset`），按逻辑顺序拷贝最多 `maxReadBytes` 字节。
   - `startOffset <= offset < totalBytes`：对 `offset` 做 `utf8Ceil` 对齐得 `nextBaseOffset`，**同样将对齐跳过的字节计入 `skippedBytes = nextBaseOffset - offset`**，拷贝 `[nextBaseOffset, min(totalBytes, nextBaseOffset + maxReadBytes))`，`offsetExpired = false`。
   - 返回：`data []byte`、`nextBaseOffset int64`、`totalBytes int64`、`skippedBytes int64`、`overflow bool`、`offsetExpired bool`。

2. **`ReadHeadTail(headMaxBytes, tailMaxBytes int) HeadTailSnapshot`**（供 `execute_bash` 同步/转后台）：
   - Buffer 层只按字节窗口读取，**不扫描 `\n`、不做行数或 50 KB 策略裁剪**：
   - 若可用区间长度 `totalBytes - startOffset <= headMaxBytes + tailMaxBytes`：返回连续切片 `full`（起点做 `utf8Ceil`，若 `overflow` 则将跳过字节计入 `evictedBytes`），`split = false`。
   - 若超过：只拷贝可用区间最前 `headMaxBytes`（`head` 起点做 `utf8Ceil`）与最后 `tailMaxBytes`（`tail` 起点做 `utf8Ceil`），记录中间未读字节数 `middleBytes`（**不再扫描计算 `middleLines`**，避免锁内 512 KB 扫描），`split = true`。
   - 返回：`full []byte` / `head []byte` + `tail []byte`、`split bool`、`middleBytes int64`、`evictedBytes int64`、`totalBytes int64`、`overflow bool`。

3. **`ReadTail(tailMaxBytes int) TailSnapshot`**（供 `process_output` 默认模式与 `tail_lines` 使用）：
   - 只拷贝可用区间最后 `tailMaxBytes`（起点做 `utf8Ceil`），返回 `data []byte`、`beforeBytes int64`（`data` 起点之前的总字节数，含缓冲覆盖与窗口外前部；避免与 `evictedBytes` 同名混淆）、`totalBytes int64`、`overflow bool`。
   - 传 `64 KB` 窗口即可满足不变式：**窗口 (`64 KB`) ≥ 输出上限 (`50 KB`) + marker 预算**，因此尾模式永远不会因为读取窗口不足而少给内容，只会因单行过长超出 50 KB 预算而少给行数（无需按 `tail_lines` 动态放大窗口）；Phase 2 只需从文件末尾 `Seek`。

4. **`Stats() BufferStats`**（供 `include_output: false` 使用）：
   - 仅返回 `totalBytes int64`、`startOffset int64`、`overflow bool`，零拷贝 / 零磁盘 I/O。

5. **兼容方法**：保留 `String() string`（**仅限测试/调试用，生产轮询路径严禁调用**）与 `Truncated() bool`（返回 `overflow`）。

- **Memory vs 复杂度权衡**：Phase 1 保留 `1×` 内存占用（每流上限 `512 KB` 环形缓冲，而非 `2×` 平移数组），并通过**表驱动 wrap 单元测试**保证环形逻辑正确；同时注明当前 `512 KB × 2 × N` 个后台/leftover 进程尚无全局进程数上限（后续可作为独立治理项）。
- **`Close()` 生命周期**：Phase 1 为 no-op，接口注释明确其**幂等性**；`ProcessRegistry` 在 reaper、`KillSession()` 以及 `KillAll()`（`api/process.go`）移除条目时均必须调用 `Close()`。

---

### 3.2 确定性截断与组装算法（单遍完成，无需循环重切）

> **模式归属**：`Head+Tail` = `execute_bash` 同步/转后台专用；`Prefix` = 显式 `offset`；`Tail` = `process_output` 默认尾部模式与 `tail_lines`；`Status` = `include_output: false`。

所有截断在锁外对独立拷贝执行。统一上限：`maxOutputLines = 1000`，`maxOutputBytes = 50 * 1024`（50 KB），**单流固定预留 `markerBudget = 256` 字节**（所有提示文本格式均远小于 256 字节，因此一遍切完即可保证总长 `<= 50 KB`，无需循环重切）。

#### 统一行切分口径与 UTF-8 辅助
- `splitLines(s string) []string`（仅用于 `Head+Tail` 与 `Tail` 模式）：按 `\n` 切分，丢弃末尾尾随 `\n` 产生的空串；半行按 1 行计。
- `utf8Floor(b []byte, limit int) int` / `utf8Ceil(b []byte, start int) int`：保证切片边界不切断多字节 UTF-8 字符。

#### 统一 `clipped` 判定与 `truncated` 口径
所有模式统一返回布尔值 `clipped`（表示**本次请求范围内的输出文本被裁剪**），最终 `truncated = clipped || overflow`：
- **Head+Tail 模式**：发生 Head/Tail 拆分或行/字节裁剪时 `clipped = true`（`!split && !overflow` 且未超限直接返回时 `clipped = false`）。
- **Prefix 模式**：`hasClip = (next_offset < totalBytes)`（有剩余未交付字节即触发 trailing marker 与 `clipped = true`）。
- **Tail 模式**：发生 50 KB 字节裁剪（`hasByteTrunc`），或受 `64 KB` 窗口/`512 KB` 缓冲限制导致返回行数少于 `min(目标行数, 进程历史总行数)`（即 `beforeBytes > 0 && returnedLines < targetLines`）时，`clipped = true`；若请求 `tail_lines: 100` 且这 100 行完整落在 `64 KB` 窗口与 `50 KB` 限制内，则 `clipped = false`（仅当 `overflow == true` 时 `truncated` 才因 `overflow` 为 `true`）。

#### 三种模式的确定性步骤（所有裁剪策略集中在 tools 层单遍完成）

1. **Head+Tail 模式（专用于 `execute_bash` 同步与超时转后台）**：
   - 调用 `ReadHeadTail(50 * 1024, 50 * 1024)`。
   - 若 `!split && !overflow` 且 `len(splitLines(full)) <= 1000 && len(full) <= maxOutputBytes`：直接原样返回 `full`（`clipped = false`）。
   - 若 `!split && overflow` 且 `len(splitLines(full)) <= 1000 && len(full) <= maxOutputBytes - markerBudget`（快速路径）：直接前置 `... (<evictedBytes> bytes earlier output evicted by buffer) ...\n` + `full`（`clipped = false`，`truncated` 因 `overflow` 为 `true`）。
   - 否则（`clipped = true`）按 `avail = maxOutputBytes - markerBudget` 分配 `headBudget = avail * 20 / 100`、`tailBudget = avail - headBudget`（单侧未用满不转给另一侧）：
     - `head` 侧（来自 `full` 或 `head`）先取最多前 `200` 行，再 `utf8Floor` 到 `<= headBudget`；
     - `tail` 侧（来自 `full` 或 `tail`）先取最多后 `800` 行，再 `utf8Ceil` 到 `<= tailBudget`；
     - 合并为单个 marker：`headPrefix + marker + tailSuffix`。marker 只报字节数（`... (<omittedBytes> bytes omitted) ...`，若 `overflow` 则同时注明 `evictedBytes`），不扫描统计 `middleLines`。

2. **连续前缀模式（显式传 `offset` 增量分页）**：
   - 调用 `ReadFrom(offset, maxOutputBytes)`。
   - **直接在源字节切片 `data` 上按 `\n` 位置定位行边界**（不经 `splitLines` + `Join` 重组），确保 `deliveredBytes` 等于保留前缀在源流中覆盖的**精确字节数（含最后一行行尾 `\n`）**。
   - 若 `hasEviction = readResult.offsetExpired` 或超 `1000` 行或超 `maxOutputBytes`，预留 `markerBudget`（`avail = maxOutputBytes - markerBudget`）。
   - 先定位前 `1000` 行的字节结束位置 `lineEnd`，再取 `cut = min(lineEnd, avail)` 并用 `utf8Floor` 对齐得 `deliveredBytes`。
   - **严格前进兜底**：当 `len(data) > 0` 且 `deliveredBytes == 0`（例如极罕见场景）时，至少前进 1 个完整 rune；最终令 `next_offset = min(readResult.nextBaseOffset + deliveredBytes, totalBytes)`（当 `offset >= totalBytes` 时 `next_offset = offset`）。
   - 令 `hasClip = (next_offset < totalBytes)`，即 `clipped = hasClip`。
   - 若 `hasEviction`，开头加 `... (<skippedBytes> bytes evicted by buffer before this point) ...\n`；若 `!hasEviction && readResult.skippedBytes > 0`（非 rune 对齐跳过），开头加 `... (<skippedBytes> bytes skipped for UTF-8 alignment) ...\n`（同时计入 `markerBudget`）；若 `hasClip`，末尾加 `\n... (<remainingBytes> bytes remaining in buffer) ...`（`remainingBytes = totalBytes - next_offset`）。

3. **纯尾部模式（`process_output` 默认模式 & 显式传 `tail_lines`）**：
   - 同时作用于 `stdout` 和 `stderr`；**只按 `\n` 分行**（`\r` 刷新型进度条不拆行）。
   - 设 `targetLines`：传了 `tail_lines` 时为 `tail_lines`，默认不传时为 `1000`。
   - 调用 `ReadTail(64 * 1024)`，取至多最后 `targetLines` 行，且总字节严格 `≤ 50 KB`（**若单行很长，受 50 KB / 64 KB 窗口上限约束，实际返回行数可能少于 `targetLines`**）。
   - 若选中行字节数超过 `avail`（存在 `beforeBytes > 0` 或字节超限时扣除 `markerBudget`），用 `utf8Ceil` 取后缀，记 `hasByteTrunc = true`，并把跳过的字节计入总前部未返回字节 `omittedBefore`。
   - `clipped = hasByteTrunc || (beforeBytes > 0 && returnedLines < targetLines)`。
   - 若 `omittedBefore > 0`（包括 `beforeBytes > 0` 且无法确认完整覆盖），在开头输出**单一合并 marker**：
     `... (<omittedBefore> bytes earlier output omitted/evicted) ...\n`。
   - **引导说明**：`process_output` 默认采用此模式（直接看最新结果，不再重复拉取 20% 头部）；若需查看开头，显式传 `stdout_offset: 0` / `stderr_offset: 0`。

---

### 3.3 `execute_bash` 返回结果改造

- **`ExecuteBashResult`**（同步完成 / handover）：
  - `Truncated = 响应裁剪 || stdout_overflow || stderr_overflow`。
  - 新增 `StdoutOverflow bool`、`StderrOverflow bool`（`omitempty`）。
  - handover 路径（`ProcessID != ""` 时）填充 `StdoutOffset *int64`、`StderrOffset *int64`（`omitempty`，值为快照时刻的 `totalBytes`）。
- **`BashBackgroundResult`**：
  - 超时转后台：`Truncated = 响应裁剪 || stdout_overflow || stderr_overflow`，填充 `StdoutOffset`、`StderrOffset`（快照 `totalBytes`）及 `StdoutOverflow`、`StderrOverflow`（`omitempty`）。
  - `run_in_background: true` 立即返回：`Stdout: ""`, `Stderr: ""`, `Truncated: false`, `StdoutOffset: 0`, `StderrOffset: 0`。
- 默认尾部模式、handover、转后台、`tail_lines` 返回的 `*_offset` 均为“从当前最新位置继续增量追踪的续读游标”；如需回补未被覆盖的更早输出，可显式传 `0` 或更早 `offset`。

---

### 3.4 `process_output` 协议与参数组合矩阵

#### 入参（同步更新 `ProcessOutputSchema.InputSchema`）

| 参数名 | 类型 | 约束 / 默认值 | 说明 |
|---|---|---|---|
| `process_id` | `string` | 必填 | `execute_bash` 返回的进程句柄。 |
| `block` | `boolean` | 默认 `true` | 是否等待进程/残留后代结束。 |
| `timeout` | `integer` | 默认 `30000` | 最大等待毫秒数（`block: false` 时忽略；`> 60000` 静默钳制到 `60000`；`<= 0` 立即超时）。 |
| `stdout_offset` | `integer` | `minimum: 0`，默认 `null` | 显式增量读取起点。不传时 `stdout` 走默认尾部模式。 |
| `stderr_offset` | `integer` | `minimum: 0`，默认 `null` | 显式增量读取起点。不传时 `stderr` 走默认尾部模式。 |
| `tail_lines` | `integer` | `minimum: 1, maximum: 1000`，默认 `null` | 同时作用于 `stdout`/`stderr`，仅看最后 N 行（`> 1000` 静默钳制到 `1000`）。与 `*_offset` 互斥。 |
| `include_output` | `boolean` | 默认 `true` | 设为 `false` 时不返回输出并跳过期间积累的输出（游标推进到最新）；进程结束后需要看结果请用 `tail_lines`，或传更早的 `*_offset`。 |

#### 返回字段

| 字段名 | 类型 | 说明 |
|---|---|---|
| `process_id` | `string` | 回显 `process_id`。 |
| `stdout` / `stderr` | `string` | 截取后的输出文本（可能含 marker，故 `len(stdout)` 不等于 `deliveredBytes`，**必须使用返回的 `*_offset` 作为下次游标，不可用字符串长度推算**）。 |
| `running` | `boolean` | `!bp.IsDone() \|\| hasLeftover`。 |
| `exit_code` | `integer \| null` | 仅当 `running == false` 时返回退出码；主进程已退出但仍有后代存活时为 `null`。 |
| `truncated` | `boolean` | 流级完整标志：`本次响应被裁剪 \|\| stdout_overflow \|\| stderr_overflow`。 |
| `stdout_offset` / `stderr_offset` | `integer` | 续读游标。 |
| `stdout_overflow` / `stderr_overflow` | `boolean` | `omitempty`。对应流缓冲是否发生过覆盖（Phase 2 文件缓冲全量保留时为 `false`）。 |
| `stdout_offset_expired` / `stderr_offset_expired` | `boolean` | `omitempty`。传入的 `offset` 是否落入已被覆盖的 `[0, startOffset)` 区间。**即使为 `true`，本次返回的 `*_offset` 依然是有效新游标，下次应继续传入该值（不要重置回 0）。** |
| `leftover_processes` | `object` | `omitempty`。`{alive, tree: {pid, ppid, command, children}}`。 |

#### 参数组合行为矩阵

| 参数组合 | `stdout` / `stderr` 行为 | `*_offset` 返回值 | `truncated` | `*_offset_expired` | 校验与报错 |
|---|---|---|---|---|---|
| 均不传（默认尾部模式） | 各自走纯尾部模式（至多最新 1000 行且 ≤ 50 KB） | `totalBytes` | `裁剪 \|\| *_overflow` | `false`（省略） | `process_id == ""` → `protocol/invalid_params` |
| 仅传 `stdout_offset` | `stdout` 按前缀分页；`stderr` 走默认尾部模式 | `stdout_offset = min(nextBaseOffset + deliveredBytes, totalBytes)`（`offset >= totalBytes` 时原样回显）；`stderr_offset = stderr_total` | `裁剪 \|\| *_overflow` | `stdout` 按 `offset < startOffset` 判定；`stderr` 省略 | `offset < 0` → `protocol/invalid_params` |
| 传 `tail_lines` | 各自取最后 N 行（仅按 `\n` 分行），超 50 KB 做纯尾部截断 | `totalBytes` | `裁剪 \|\| *_overflow` | `false`（省略） | `tail_lines <= 0` → `protocol/invalid_params` |
| `tail_lines` 与任一 `*_offset` 同传 | — | — | — | — | 直接返回 `protocol/invalid_params` |
| `include_output: false`（可配合法 `offset` 或 `tail_lines`） | 跳过截断与过期判定，直接返回 `""` | 调用 `Stats()` 零拷贝返回 `max(passedOffset, totalBytes)`（未传 `offset` 时为 `totalBytes`） | **`stdout_overflow \|\| stderr_overflow`** | 恒为 `false`（省略） | 仍做参数范围与互斥校验（非法报 `protocol/invalid_params`） |

> **校验与钳制汇总**：`process_id == ""` / `offset < 0` / `tail_lines <= 0` / `tail_lines` 与 `*_offset` 互斥 → 返回 `protocol/invalid_params`；`tail_lines > 1000` 静默钳制到 `1000`；`timeout > 60000` 静默钳制到 `60000`；`timeout <= 0` 立即超时；`block: false` 忽略 `timeout`。该表同步写入 `spec/tools/process_output.md`。

---

## 4. Spec 与 Schema 同步计划

### 4.1 `spec/tools/process_output.md`
- **历史对齐修复**：
  - `process_id` 产生条件补充 handover 场景；示例 ID 从 `proc-abc123` 改为 `proc_1`（并注明 opaque）；
  - `stdout` / `stderr` 描述修正为受缓冲容量与单次截断上限（1000 行 / 50 KB）约束；
  - 补齐 `leftover_processes`（`{alive, tree: {pid, ppid, command, children}}`）与 `leftover` 状态；
  - 明确 `running` / `exit_code: null` 语义（主进程已退出但有存活后代时 `running: true`、`exit_code: null`，`block: true` 以 100ms 间隔轮询）；
  - 补齐 `process_id` 生命周期与作用域（`GlobalProcessRegistry.Get` 不校验 session，reaper 每 5 分钟扫描移除完成且无存活后代超 30 分钟的条目，WebSocket 关闭时 `KillSession` 清理）；
  - 明确 `timeout` 钳制到 `60000`、`<= 0`（含 `0`）立即超时、`block: false` 忽略；
  - 参数解析与校验错误统一返回 `protocol/invalid_params`。
- **新增 API 规范**：Parameters 表补齐 4 个新参数与约束、Output Limits（三种模式；明确尾模式为“至多 N 行且 ≤ 50 KB”）、参数组合矩阵、Response 示例、Error Cases，以及“不可用 `len(stdout)` 推算游标”、“分页页边界可能落在行中间，**续读直接从返回的 `stdout_offset`/`stderr_offset` 开始即为正确拼接**（勿自行按行对齐）”、“`offset_expired: true` 仍应使用返回的新 `offset` 续读”。

### 4.2 `spec/tools/execute_bash.md` & Go Schema
- `spec/tools/execute_bash.md`：更新 Head+Tail 新 marker 文案，补充普通同步 `overflow`、handover offsets、`BashBackgroundResult` 新字段。
- `ProcessOutputSchema`：在 `InputSchema` 中注册 4 个新参数（含 `minimum`/`maximum` 与 `description`）；重写 `Description` 明确 `process_id` 三种来源、`leftover_processes` 语义，并引导“轮询用 `include_output: false`、增量传 `*_offset`、看结尾传 `tail_lines`”。
- `executeBashDescription()`：同步更新截断与续读游标说明。

---

## 5. 实施与测试清单

- [ ] `gtsp/src/api/process.go`：改造 `ProcBuffer`（`DefaultProcRingBytes = 512KB` 渐进增长环形缓冲；`Write` 永远返回 `len(p), nil` 且单次超 `maxBytes` 不超分配；`ReadFrom`、`ReadHeadTail`、`ReadTail`、`Stats`、`Close` 接口与 UTF-8 起点对齐；保留兼容 `String()` / `Truncated()`；`ProcessRegistry` 清理条目时调用 `Close()`）。
- [ ] `gtsp/src/tools/execute_bash.go`：实现单遍确定性截断（`Head+Tail` 与 `Tail` 共用 `splitLines`；`Prefix` 直接在源切片上按 `\n` 字节边界扫描，不经 `splitLines`+`Join` 重组；单流 `markerBudget = 256` + UTF-8 安全切片）；改造 `buildSyncResult`、`BashBackgroundResult`、`executeBashDescription()`。
- [ ] `gtsp/src/tools/process_output.go`：更新 `ProcessOutputSchema`、修复 `block: true` 绝对 `deadline`（避免 leftover 轮询忽略超时）、参数校验（返回 `api.ErrInvalidParams`）、组合矩阵调度与按流标志。
- [ ] `spec/tools/execute_bash.md` & `spec/tools/process_output.md`：完成第 4 节全部文档同步。
- [ ] `gtsp/test/unit/` & `gtsp/test/e2e/`：
  - 更新 `shell_test.go` 中的 `TestExecuteBash/truncation` 断言与 `gtsp/test/e2e/process_test.go` P2 marker 断言；
  - 适配 `process_cleanup_test.go` 中的 `TestProcBufferCapsMemoryAndMarksTruncated`（`MaxBytes: 64` 写 100 字节保留末尾 64 字节、`Truncated() == true`、`Write == 100`），并加强 `TestSyncOutputCompletenessNearPipeBuffer`（断言 `END_MARKER` 必存在）；
  - 新增测试：
    - **分页等价性测试（按层拆分）**：
      - `ProcBuffer.ReadFrom` 单测：随机分块 `Write` 已知字节流（含多字节 UTF-8、>50 KB 超长单行、恰好 512 KB 边界），从 `offset=0` 连续翻页到 `totalBytes`，断言各页按序拼接后与原始流逐字节相等（未溢出）或等于原始流尾部（溢出）；
      - `ProcessOutputHandler` 单测：含 marker 的连续分页断言 `next_offset` 严格递增（含单行 1 MB 时 `next_offset > offset`）、终值 `== totalBytes`、页数有界（不比拼接文本）；
    - `Write` 全量返回与单次超 `maxBytes` 保护、渐进增长与**表驱动**物理 wrap 逻辑顺序、非自然起点落在 UTF-8 rune 中间、`offset < startOffset` 过期跳转与 `offset > totalBytes` 原样回显不倒退、`tail_lines`（含 `\r` 进度条不拆行与合并 marker）、`include_output: false` 零拷贝与 `truncated` / `offset > totalBytes`、handover/同步/后台结果新字段、leftover 绝对 `deadline` 超时、`process_id: ""` / 参数互斥 / 类型错误 `protocol/invalid_params`、schema/description 断言、`KillAll()` 调用 `Close()`、`go test -race` 并发读写。
