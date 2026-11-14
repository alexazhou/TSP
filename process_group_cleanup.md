# execute_bash 进程组清理改造方案

> 状态：待实施（已合并评审意见）
> 日期：2026-11-14
> 来源：桌面版线上事故（8 个孤儿进程以 ~700% CPU 空转 2.5 小时）+ 代码级评审发现的 3 个实现盲区
> 说明：本文档合并自原 `improve.md`（超时机制改造，已落地，移除）与 `orphan_process_cleanup_design.md`（事故记录），只保留事故记录与待实施项。

## 一、事故记录

### 1.1 现象

用户报告 TogoSpace 整机 CPU 占用很高。排查发现：

- 后端进程（TogoSpace-backend）本身空闲（0.8%–5%），日志无忙轮询，排除框架嫌疑。
- 真正的 CPU 消耗来自 **8 个 `bash load230.sh` 孤儿进程**（PID 41376–41383），每个 ~90% CPU，合计约 700%，从 16:21 起持续空转近 2.5 小时，`PPID=1`（launchd 收养）。
- 进程来源：Agent「李哥」在其工作区（`~/.togospace/workspace/软件研发团队/agent/李哥/`）通过 `execute_bash` 运行的压测脚本，脚本内故意 fork 8 个忙等子进程模拟 CPU 负载（`while :; do :; done`）。

### 1.2 时间线（依据 gtsp 会话日志）

| 时间 | 事件 |
|------|------|
| 16:19:50 | Agent 用 write_file 写入 `load230.sh`。脚本第 38 行 fork 8 个忙等子进程，第 39 行 `LOADPIDS=$(jobs -p)` 收集 PID，第 47 行 `kill $LOADPIDS` 清理 |
| 16:19:56 | **第 1 次** execute_bash：`bash load230.sh 2>&1 \| tail -25`。正常执行完毕，`kill` 生效（stdout 记录 8 条 `Terminated`），**无泄漏** |
| 16:20:58 | **第 2 次** execute_bash：`bash load230.sh 2>&1 \| head -12`，`timeout_action: kill`，`task_timeout: 600` |
| 16:21:30 | 该调用仅 32 秒即返回：`head -12` 读满退出，脚本向已关闭的管道写 stdout 收到 SIGPIPE **中途死亡**，未执行到第 49 行清理 |
| — | 脚本死后，8 个忙等子进程孤儿化（PPID→1），与 gtsp 的进程树关系断裂，持续空转 |
| 19:15:03 | Agent 自己通过 `ps` 发现残留，执行 `pkill -f "bash load230.sh"` 清理，问题解除 |

### 1.3 根因归纳

1. **直接诱因**：`| head -N` 之类的管道截断使脚本收到 SIGPIPE 提前死亡，后台子任务未被清理。
2. **框架缺陷（核心）**：gtsp 只在「超时 kill」和「process_stop」两条路径上清理进程组，**同步正常完成路径（`bp.WaitChan()` 命中后直接 return）不做任何清理**。脚本无论因何种原因退出——正常结束、SIGPIPE、外部信号——gtsp 都认为调用已结束，组内残余子进程从此不可见。本次事故脚本虽还在同一进程组内，但没有任何代码去 kill 这个组。
3. **风险放大**：Agent 产出的脚本内容不受控，这次只是空循环烧 CPU；下次可能是写文件、占端口、fork 炸弹级别的负载。

## 二、已具备的基础（不再重复设计）

以下能力已在现有代码落地，本方案直接复用：spawn 时 `pal.SetProcessGroup` 建立独立进程组（execute_bash.go:120）；默认服务端超时 180s 与 `timeout_action`（background/kill）；超时 kill 与 `process_stop` 均通过 `pal.KillProcessGroup` 清理整组；输出自身截断（50KB / 1000 行）。注意：进程组 kill 仅在 Unix 上是真实组语义，Windows 目前以 `taskkill /T` 近似且存在失效场景（见 3.3）。历史设计见 git 中的 `improve.md`（已删除）。

## 三、待实施改动

> 结构说明：3.2（管道解耦）与 3.3（Windows Job Object）是 3.1 能真正生效的两个前置条件，评审发现；3.4 为跨平台独立 bug 修复。

### 3.1 同步完成路径：残余后代检测与移交（不自动杀）

**设计动机（评审第三轮修订，第四轮去掉自动兜底）**：原设计「同步完成后自动杀组内残余」有一个不可判定问题——框架无法区分「脚本遗留的垃圾子进程」与「Agent 故意启动的后台服务」（如 `server &`）。自动杀会误伤后者。修订为：**检测 + 报告 + 移交处置**，杀不杀只由 Agent 决定，框架不做任何自动杀（含 GC 兜底）。

`ExecuteBashHandler` 的同步完成分支：

```go
select {
case <-bp.WaitChan():
    if !bp.HasLeftoverProcesses() {      // Unix: kill(-pgid, 0) 探测；Windows: Job 进程列表
        bp.WaitIO(...)                 // 管道自然 EOF，完整抽干输出
        bp.Close()                     // 释放平台清理句柄
        return buildSyncResult(bp), nil
    }
    // 有存活后代：登记进注册表，移交 Agent 处置（不杀）
    api.GlobalProcessRegistry.Register(bp)
    bp.WaitIO(500 * time.Millisecond)  // 有界等待：EOF 不会到来，只带走已有输出
    result := buildSyncResult(bp)
    result.ProcessID = bp.ID
    result.LeftoverProcesses = EnumerateLeftoverProcesses(bp) // 结构化进程树快照，见下
    result.Message = "command exited but descendant process(es) are still running; " +
        "use process_stop to terminate, or process_output to poll; " +
        "unclaimed processes persist until server shutdown"
    return result, nil
...
```

要点：

- **探测原语**：Unix 先用 `kill(-pgid, 0)`（signal 0 存在性检查，无副作用，组长已死依然有效）判断组内是否还有存活成员；**确认有存活时再枚举成员构建进程树**（枚举成本高，无存活时不做）。Windows 直接用 `QueryInformationJobObject` 进程列表。
- **成员枚举（平台差异，评审第五轮细化）**：
  - **Unix（macOS/Linux）走慢路径 `ps` 子进程**：枚举只在「主进程已退出且 `kill(-pgid, 0)` 探测命中」的异常路径触发（正常命令毫秒级短路返回），此时直接执行 `ps -ax -o pid=,ppid=,pgid=,state=,args=` 按 pgid 过滤（排除 `state == Z` 僵尸）——~20 行纯 Go、耗时 ~3ms，且跨平台拿到**完整带参数的命令行**。不走 sysctl 的原因：macOS `kinfo_proc` 只有 16 字节截断进程名 `p_comm`，完整命令行需逐 PID 调 `KERN_PROCARGS2` 解析，纯 Go 实现晦涩且受架构对齐影响，不值得。
  - **Windows 两段拼装**：`QueryInformationJobObject(JobObjectBasicProcessIdList)` 只给 PID 数组，拿到集合后调一次 `CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS)` + `Process32First/Next`（<1ms）补齐 `th32ParentProcessID`（ppid）与 `szExeFile`（命令名），拼出完整树。
- **结构化报告 `leftover_processes`**：以执行的 shell 为根的进程树快照（shell 自身 alive=false），存活后代按 ppid 挂到对应父节点；ppid 不在快照集合内的（祖先已死）挂到根下。字段定义与示例见工具规格 `gtsp/doc/tool_spec/execute_bash.md` §5。
- **移交复用现有工具**：Register 后 Agent 可直接用 `process_stop`（杀整组，依赖 3.4 的短路修复）/ `process_output` 查看输出，`ExecuteBashResult` 新增可选 `process_id`、`leftover_processes` 字段承载编号与进程树。
- **快照语义**：`leftover_processes` 是返回时刻的一次性快照，存在固有竞态（快照后进程可能退出/新起）；Agent 需要最新状态时用 `process_output` 重新拉取（同样返回刷新后的树），或直接 `process_stop`（幂等，快照过期无碍）。
- **Register 使进程进入注册表视野**（原设计中同步路径不注册、`KillAll`/`process_stop`/`process_list` 都扫不到——修订后移交的 survivor 可被 Agent 与退出清理管理，见下）。
- **处置责任完全在 Agent，框架不自动杀**（评审第四轮修订，去掉 GC 兜底）：Agent 不调用 `process_stop` 的话，存活后代一直运行，直到 gtsp 退出时 `KillAll` 统一清理。这是有意为之的取舍——框架不替 Agent 做「杀」的决定，宁可留到显式处置或进程退出。
- 与事故的关系：本次事故模式（Agent 无意遗留 8 个忙等进程）在此设计下**不会被任何自动机制杀掉**，仅返回值中带编号提示；若 Agent 忽略提示，空转进程会留存至 gtsp 退出。接受该风险以换取「绝不错杀有意留下的服务」。
- **两个前置条件**：Unix 上需先做 3.2 的管道解耦（否则后台子进程持有 stdout 写端时根本走不到本分支）；Windows 上需先做 3.3 的 Job Object 改造（否则探测与杀组原语不可靠）。

#### 设计边界（关键决策）

**框架不猜测意图：同步完成后有存活后代 = 报告 + 移交；后台路径语义不变。**

- 同步模式语义 = 「要这个命令的结果」；命令退出后组内是否该有活物，只有 Agent 知道，因此只报告不处置。
- 后台模式语义（`run_in_background`、超时转 background）= 允许长驻，行为不变：`process_stop` / 会话级清理（3.6）/ gtsp 退出负责回收，框架不按时间自动杀。
- 已知边界：子进程 `setsid` 逃出进程组后，`kill(-pgid, 0)` 探测不到它（组外），既不报告也不可组杀——与原设计相同，属平台能力边界，写入工具描述即可。
- 可选增强（首版不做）：允许 Agent 在调用时声明「本命令有意留后台进程」以跳过提示，或声明「请自动清理」以走杀组路径；等真实使用数据出现再定参数形态。

### 3.2 stdout/stderr 管道解耦（os.Pipe 自管管道）

**现状陷阱**：execute_bash.go:123–124 把自定义 `*ProcBuffer` 直接赋给 `cmd.Stdout/cmd.Stderr`。Go 标准库 `os/exec` 对非 `*os.File` 的 Writer 会：创建匿名管道 + 启动 copy goroutine 持续搬运；**`cmd.Wait()` 不仅等主进程退出，还会死等 copy goroutine 结束（即管道 EOF）**。

后果推演（评审发现的核心盲区）：

- 事故命令 `bash load230.sh 2>&1 | head -12` 能退出：Go 管道的写端在外层 shell（和 head）手里，shell 死则 EOF 触发，`Wait()` 正常返回——所以事故是「走完了同步分支但没清理」，而不是「走不到」。
- 但更常见的 `bash test.sh`（内含 `python worker.py &` 且未重定向 stdout）：worker 继承并持有 Go 管道写端，shell 退出后 EOF 永不到来 → `cmd.Wait()` 阻塞 → `WaitChan()` 永不触发 → **同步分支卡满 180s 直到超时**，3.1 的清理代码根本没机会执行。

**改造**（`api/process.go` `NewProcess`，或 execute_bash 构建处）：

```go
// stdout 与 stderr 各自独立一对 pipe（结果结构区分两个字段，不能合流）
outR, outW, _ := os.Pipe()
errR, errW, _ := os.Pipe()
cmd.Stdout = outW   // *os.File：exec 直接传 fd，Wait() 只等主进程退出，不等 EOF
cmd.Stderr = errW
cmd.Start()
outW.Close()        // 父进程立即关写端，EOF 判定权交给子进程自己
errW.Close()
ioDone := make(chan struct{})  // stdout/stderr 两个 io.Copy 都结束并 Close 后 close
go func() { io.Copy(stdoutBuf, outR); outR.Close() }()
go func() { io.Copy(stderrBuf, errR); errR.Close() }()
// copy 计数归零后 close(ioDone)
```

**必须处理的次生问题（评审第二轮补充）：**

1. **输出抢跑竞态**：`cmd.Wait()` 在主进程退出瞬间返回，此时内核管道缓冲区里最后几 KB 可能还没被 copy goroutine 搬进 buffer——直接 `buildSyncResult` 读 `bp.Stdout.String()` 会**偶发丢尾部输出**（如 `cat file.txt` 截掉结尾）。
2. **读端 FD 泄漏**：`outR/errR` 用完必须 `Close()`，否则每次执行漏 2 个读端 FD。

**正确时序**（同步返回路径，`BackgroundProcess` 增加 `ioDone` chan 与 `WaitIO(timeout)` 方法；是否杀组由 3.1 决定，此处只定义 I/O 机制）：

```go
case <-bp.WaitChan():              // 主进程退出即触发，毫秒级，不卡 180s
    // 无存活后代（3.1 探测为否）：管道自然 EOF，WaitIO 完整抽干 → Close → 返回
    // 有存活后代（3.1 移交场景）：EOF 不会到来，WaitIO(500ms) 有界返回，只带走已有输出
    bp.WaitIO(500 * time.Millisecond)
    bp.Close()                     // 释放平台清理句柄（移交场景由 process_stop / 退出清理后续释放）
    return buildSyncResult(bp), nil
```

效果链：shell 一退出 `Wait()` 立即返回（不再被隐式管道绑架）→ 无残余时输出 100% 完整后再构建返回值 → 有残余时 copy goroutine 在后台继续搬运（存活后代死掉后其继承的 pw 副本被内核关闭，EOF 自然到来），返回值携带 process_id 与提示。**同时解决「同步脚本里 `cmd &` 导致 execute_bash 假死 180s」的经典顽疾与本次 SIGPIPE 孤儿不可见问题。**

- **一致性要求**：`process_output` 阻塞等待完成的路径（`<-bp.WaitChan()` 触发时）同样调 `bp.WaitIO(100ms)`，确保最后一口输出进 buffer 再返回给 Agent。
- **内存上限**：存活后代持续向继承的 pw 写输出时（如 chatty daemon），copy goroutine 会持续填充 `ProcBuffer`——给 buffer 加上限（复用 `maxOutputBytes` 量级），超限丢弃并标记 `truncated`，防长驻后代把内存吃穿。
- 超时转 background 的路径无需 `WaitIO`（后台语义下 copy goroutine 自行收尾，后续 process_output 拉取时数据自然齐了，且 buffer 读取与写入有锁保护）。

### 3.3 跨平台清理原语改造（Windows Job Object）

现状：`pal_windows.go` 的 `SetProcessGroup` 是空操作，`KillProcessGroup` 用 `taskkill /T /F /PID` 近似「整树杀」。`/T` 依赖**调用时刻的父子链快照**遍历，有两个失效场景：

1. **组长先退、孙进程孤儿化**——父子链从已退出的中间节点断掉，快照遍历不到孙进程。这正是本次事故的模式：Unix 上 `kill(-pgid)` 对已死组长依然有效，Windows 上这个安全网完全不存在。
2. **对已退出 pid 调用 taskkill → 静默失败**——随后 `process_stop` 等 `WaitChan()`（早已关闭）即返回「process terminated」成功，实际孙进程全部存活，**向 Agent 假报成功**（另见 3.4 的跨平台短路问题）。

改造：Windows 上用 **Job Object** 替代 taskkill（Unix 保持进程组 kill 不变，接口统一改名 `KillProcessTree` 以正名）：

```go
// pal_windows.go
func SetProcessTree(cmd *exec.Cmd) *JobHandle {
    job := CreateJobObject(...)
    // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE：gtsp 死亡 → 句柄关闭 → 内核自动杀光 Job 内全部进程
    SetInformationJobObject(job, ...LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE)
    return &JobHandle{job: job}
}
// cmd.Start() 返回后第一行立即 AssignProcessToJobObject(job, cmd.Process)

func KillProcessTree(handle) { TerminateJobObject(handle.job, 1) }
```

**绑定时机（评审第二轮修正，原设计不可行）**：第一版方案写的「CREATE_SUSPENDED 启动 → 绑定 → ResumeThread」在 Go 标准库下**落不了地**——`syscall.StartProcess`（exec_windows.go）在返回前就 `defer CloseHandle(pi.Thread)` 关闭了主线程句柄，且不暴露给上层，父进程拿不到句柄去 `ResumeThread`，进程会永远挂起。落地方式取其一：

- **方式 A（推荐，主方案）**：不加 CREATE_SUSPENDED，`cmd.Start()` 返回后第一行立即 `AssignProcessToJobObject`。子进程（busybox sh / bash.exe）自身初始化至少毫秒级，远慢于父进程紧接着的一次系统调用，实际不可能在绑定前逃逸；绑定后检查进程是否已退出，已退出则兜底清理并报错。
- **方式 B（极致零窗口，非必需）**：维持 CREATE_SUSPENDED，恢复时绕开线程句柄限制——通过 ntdll 的 `NtResumeProcess(进程句柄)` 按进程恢复，或 `CreateToolhelp32Snapshot` 遍历线程逐个 `ResumeThread`。复杂度高，仅在方式 A 的理论逃逸窗口实测构成问题时考虑。

要点：

- `TerminateJobObject` 直接对 Job 内**所有进程**生效，不依赖父子链——孤儿化的孙进程同样被杀，失效场景 1、2 一并解决。
- `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` 附加收益：gtsp 自身被 `kill -9` / 崩溃时，句柄由内核关闭，Job 内进程被内核**自动**清理——即「硬杀免疫」在 Windows 平台上顺带解决。
- Win8+ 支持嵌套 Job，被外层 Job 包住（Tauri / PyInstaller 场景）不冲突。
- **句柄生命周期（评审补充）**：每个 execute 都会 `CreateJobObject`，不 `CloseHandle` 会长期泄漏内核句柄。必须在进程彻底销毁时关闭：同步路径与超时 kill 分支清理后（3.1）、注册表 GC 移除时（3.5）、退出 `KillAll` 后。
- 同步改动：`BackgroundProcess` 持有清理句柄（Unix 存 pgid / Windows 存 Job handle），`bp.Kill()` 改走新原语。

### 3.4 process_stop 提前短路修复（跨平台，评审发现）

现状（process_stop.go:52–58）：

```go
if bp.IsDone() {
    ... return ... "process already exited"
}
bp.Kill()
```

后台任务的主 shell 进程已退出、但组内孙进程还在跑时，`process_stop` 在这里直接返回，**无论 Unix 还是 Windows 都不会调 `Kill()`**——Agent 明确要求终止却清不掉残留。

修复：`IsDone()` 分支同样执行一次 `Kill()`（组级 kill 与 `TerminateJobObject` 都是幂等的，对已死成员无副作用），确保组内残余被清干净，再返回结果。

### 3.5 注册表卫生回收（不含任何自动杀）

现状问题：`GlobalProcessRegistry` 只进不出——已结束的进程条目永远留在 map 里（内存缓慢泄漏）。

**边界（评审第四轮修订）**：GC **只做卫生回收（移除已死条目），不做任何杀**。运行中的进程即使无人查询也永不自动终止——客户端超时遗留、会话结束遗留的进程，处置责任同样在 Agent（`process_stop` / 3.6 的会话级清理），框架只在 gtsp 退出时统一清理。

方案：给 `BackgroundProcess` 增加 `doneAt` 时间戳，注册表启动常驻 reaper 协程。

```go
type BackgroundProcess struct {
    ...
    StartedAt time.Time
    doneAt    time.Time  // cmd.Wait 返回时记录
}
```

（评审第五轮修正：原方案的 `LastQueriedAt` 字段在 R2 去掉后已无消费方——R1 只看 `doneAt` 与组是否为空——字段与各查询路径的刷新逻辑一并删除；若将来 `process_list` 需要展示最后查询时间再加回。）

Reaper 周期（建议 5 分钟）单条规则：

| 规则 | 条件 | 动作 |
|------|------|------|
| R1 | `IsDone()` 且组内**无**存活成员，距 `doneAt` 超过 30 分钟 | 仅从注册表移除（进程与组都已死，无需 kill）；**Windows 上同时 CloseHandle 关闭 Job 句柄** |

**明确不做的事（评审第四轮修订）**：

- 「done 但组内仍有存活成员」的条目（含 3.1 移交的 survivor 组）不自动杀——R1 因组非空而不会移除，条目留存至 Agent `process_stop` 或 gtsp 退出 `KillAll`；
- 「运行中但无人查询」的进程不自动杀（原方案 6h 规则已去掉）——处置责任在 Agent 与会话级清理（3.6）；
- 注册表条目与 Windows Job 句柄随之保持占用，这是「绝不错杀」原则的直接代价，内存/句柄量级可忽略（单条目 + 单句柄）。

- R1 TTL（30min）为常量，放 `api/process.go` 顶部并注明可调。
- 组内存活探测：Unix `kill(-pgid, 0)`；Windows 枚举 Job 进程列表（复用 3.1 的探测原语）。
- gtsp 进程退出时已有 `KillAll` 兜底（stdin EOF / 信号 / shutdown RPC 三条路径均覆盖），本节只解决注册表卫生问题。

### 3.6 父进程猝死防线与会话级清理（评审补充）

gtsp 被硬杀（kill -9 / 崩溃）时，信号处理器不执行，现有三条 `KillAll` 路径全部失效。按平台补防线：

| 平台 | 机制 | 说明 |
|------|------|------|
| Windows | 已由 3.3 的 `KILL_ON_JOB_CLOSE` 覆盖 | 内核自动清理，无需额外动作 |
| Linux | `SysProcAttr.Pdeathsig: SIGKILL`（pal_linux.go） | **局限**：仅对 gtsp 直接创建的子进程生效（孙进程不继承该属性），且语义是「父线程死亡」；作为快速斩断命令树根节点的补充手段，树内残余仍依赖常规组清理 |
| macOS | 无内核级等价机制 | 靠下述会话级清理缩小暴露面 |

**会话级清理（跨模式补缺口，并承接 GC 去兜底后的职责）**：stdin EOF 模式已在主循环退出时 `KillAll`（dispatcher.go:315），但 **WebSocket 模式下客户端断连不会清理该会话的后台进程**（websocket.go 无对应处理）——原方案的 6h GC 规则去掉后，断连遗留的进程将永续存活，会话级清理成为**唯一**的自动回收手段。改造：`BackgroundProcess` 关联 `Session`，WS 断连 / Session 关闭时清理该会话名下的进程（`KillAll` 增加按会话过滤的变体）。这同时缩小了 macOS 上硬杀场景的残留窗口。

### 3.7 配套：工具 schema 描述更新

`execute_bash` 的 description 需要新增两条语义说明（与 3.1 的边界决策一致）：

- 同步模式：命令退出时若其 fork 的后代进程仍在运行（含 `&`、daemon），**不会自动终止**——返回值将携带 `process_id` 与提示，由 Agent 自行 `process_stop` 终止、`process_output` 查看输出；不处置则一直运行，直到 gtsp 退出统一清理；
- 预期长驻的任务（启动服务、监听端口等）优先使用 `run_in_background: true`，可直接获得 process_id 且不受同步移交语义影响。

同步更新 `gtsp/doc/tool_spec/execute_bash.md`。

**可选后续（不在首版）**：`process_stop` / 超时 kill 从直接 SIGKILL 升级为 SIGTERM → 100–200ms 宽限 → SIGKILL，避免构建工具（go build、git 等）被强杀留下锁文件。

### 3.8 process_list / process_output 对移交条目的可见性（评审第五轮补充）

3.1 的移交条目有个特殊状态：**主 shell 已退出（`IsDone() == true`）但后代仍在跑**。现有两个查询工具按「done = 结束」处理，会把移交条目当成已结束对待，移交机制的提示文案（"use process_output to poll"）就落空了。联动修复：

**process_list.go:41**——过滤条件放行移交条目：

```go
// 现状：if bp.IsDone() { continue }           // 移交条目直接被跳过，列表里看不见
// 改为：
if bp.IsDone() && !bp.HasLeftoverProcesses() {
    continue
}
// 移交条目的 status 显示 "leftover"（主进程已退出、后代存活），区别于 "running"
```

**process_output.go**——三处改动：

1. **`running` 语义**：`running := !bp.IsDone()` 改为 `running := !bp.IsDone() || bp.HasLeftoverProcesses()`——移交条目（8 个后代还在跑）必须报 `running: true`，否则 Agent 会误判进程组已死。
2. **新增 `leftover_processes` 字段**：`ProcessOutputResult` 目前没有该字段，与 3.1 / `execute_bash.md` §5.1 承诺的「调 process_output 返回刷新后的进程树」对齐——每次查询重新枚举并返回当前树。
3. **`block` 分支的等待目标**：`if block && !bp.IsDone()` 等 `WaitChan` 对移交条目无效（`WaitChan` 早已关闭，立即穿透返回）。改为：`IsDone() && HasLeftoverProcesses()` 时轮询组内存活状态（周期 ~100ms，复用探测原语），组空即返回，`timeout` 兜底——**不能**改等 `IoDoneChan`：存活后代持有管道写端时 copy goroutine 不结束，等它等于变相无限等。

### 3.9 退出统一清理覆盖移交条目（含句柄释放）

`KillAll` 遍历注册表逐个 `bp.Kill()`——移交条目已 Register，天然覆盖 ✅。补充一点：`KillAll` 杀组后同样要 `Close()` 释放平台句柄（与 3.5 R1 的移除路径一致），避免退出路径漏关 Job 句柄。

## 四、影响范围

| 组件 | 改动 | 风险 |
|------|------|------|
| `gtsp/src/tools/execute_bash.go` | 同步分支增加残余后代检测与移交（探测 + Register + 返回 `process_id`/提示），结果结构新增可选 `process_id` 字段 | 低：不改变既有成功路径的行为，仅在检测到存活后代时追加信息 |
| `gtsp/src/api/process.go` | **os.Pipe 管道解耦（替换隐式 exec 管道）**；`HasLeftoverProcesses` 探测原语；`ProcBuffer` 内存上限；`BackgroundProcess` 持平台清理句柄 + GC 字段 + reaper 协程 | **中**：输出通路重构，需确认 stdout/stderr 独立语义与截断行为不变 |
| `gtsp/src/pal/pal_windows.go` | taskkill 近似改为 Job Object（含 suspended-start 绑定与 CloseHandle 生命周期）；`pal_unix.go` 接口改名 `KillProcessTree` | **中**：Windows 清理行为整体增强，需覆盖两个失效场景的回归 |
| `gtsp/src/tools/process_stop.go` | `IsDone()` 分支补一次 `Kill()` | 低 |
| `gtsp/src/tools/process_output.go` / `process_list.go` | **移交条目可见性（3.8）**：list 放行 done-but-leftover 条目并标 `leftover` 状态；output 的 `running` 语义扩展、新增 `leftover_processes` 字段、block 分支改按组空轮询；阻塞等待完成路径加 `WaitIO` | 低 |
| `gtsp/src/api/websocket.go` / dispatcher | 会话级进程清理（WS 断连 / Session 关闭） | 低 |
| 工具 schema / tool_spec 文档 | 描述更新 | 无 |
| 同步命令中有意/无意留后台子进程的 Agent 调用 | 行为变化：不自动杀也不误杀——返回 `process_id` + 提示，完全由 Agent 处置（`process_stop`），gtsp 退出时统一清理；同步命令含后台子进程时**不再假死 180s** | **低**：语义上最保守（框架绝不主动杀同步残余），只有提示措辞需要工具描述讲清楚 |

## 五、测试与验收

### 单元测试

1. **同步命令 fork 后台子进程后退出 → 立即返回**（不是卡满超时），返回值携带存活后代提示、`process_id` 与 `leftover_processes` 进程树（断言树结构与 alive 标注正确），且**不杀**后代。此用例同时覆盖 3.2 管道解耦（改造前该用例会卡 180s，是评审盲区的直接回归测试）。
2. **输出完整性（抢跑竞态回归）**：同步命令输出大批数据后退出 → 返回的 stdout/stderr 尾部无丢失；压测多次（管道缓冲边界附近的数据量）排除偶发截尾。
3. 同步命令被管道截断（复现事故：`bash xxx.sh 2>&1 | head -1`，脚本内 fork 忙等子进程）→ 返回 `process_id` 与 `leftover_processes` 树（孙进程因父已死挂根节点下、alive=true）；`process_stop` 清理整组；**全程不调用 process_stop 时后代一直存活（验证无自动杀兜底）**，gtsp 退出时被 `KillAll` 清理。
4. 超时 kill 与 process_stop 路径回归（清理行为不变）。
5. `run_in_background` 路径回归：主进程退出、子进程存活 → 行为不变（长驻合法，不受移交语义影响）。
6. **process_stop**：主进程已退出但组内孙进程存活 → 仍能终止孙进程（修复 3.4，移交场景的主要杀路径）。
7. **Windows 专项**：组长先退、孙进程存活 → Job Object 全部终止；探测（Job 进程列表）与杀组均可用；Job 句柄在移交回收 / 超时 kill / GC 移除后均被 CloseHandle（句柄数回归）。
8. Reaper：done 且组空 → 30min 后移除；done 但组内有存活成员（survivor 组）→ **不移除也不杀**（条目留存）；运行中进程**永不自动终止**（验证无 6h 规则）。
9. **WS 断连**：会话内后台进程随断连被清理；未断连会话不受影响。
10. **FD 泄漏回归**：连续执行 N 次 execute_bash 后，进程 FD 数量恒定（覆盖 3.2 的 outR/errR 关闭与 Job 句柄关闭）。
11. **chatty survivor 内存上限**：移交场景下存活后代持续写输出 → `ProcBuffer` 超限后丢弃并标记 `truncated`，内存不无界增长。
12. **移交条目查询可见性（3.8）**：`process_list` 显示移交条目（status=`leftover`，不再被 `IsDone` 跳过）；`process_output` 对移交条目返回 `running: true` 与刷新后的 `leftover_processes` 树；`block: true` 时按组空轮询至返回（而非因 `WaitChan` 已关立即穿透）。

### 集成验收（复用事故脚本）

- 用事故脚本 `load230.sh`（内容见 §1.2 时间线）：
  - `| tail -25`（正常跑完、脚本自行清理）→ 返回无 `process_id`、无 `leftover_processes`（与现状一致）；
  - `| head -12`（管道截断、后代孤儿化）→ **断言返回 `process_id` 且 `leftover_processes` 树中挂 8 个存活后代**（此时后代是故意保留的，不做无杀断言）→ 紧接 `process_stop(process_id)` → 断言无存活子进程。
- `process_list` 全程可见移交条目（status=leftover）；`process_output(process_id)` 返回 `running: true` 与刷新后的树。
- 压测：连续 100 次「脚本 fork 后台进程 + 管道截断 + process_stop」，执行后系统无新增 PPID=1 的孤儿，FD/句柄数恒定。

## 六、实施顺序

1. **3.2 管道解耦 + 3.3 Windows Job Object**——3.1 的两个前置条件，先行实施（单测 1 的「改造前卡 180s」现象即验证依据）
2. **3.1 同步路径残余检测与移交 + 3.4 process_stop 短路修复 + 3.8 查询工具可见性**（单测 1/2/3/4/5/6）——消除事故根因（检测 + 移交语义），移交条目在 process_list / process_output 中完整可见
3. 3.7 schema 与 tool_spec 文档（随上批同发）
4. 3.5 注册表卫生回收（单测 8）与 3.6 猝死防线/会话级清理（单测 9）——独立改动，可并行
