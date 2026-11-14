# gtsp 子进程回收机制

> **适用版本**：gtsp **v0.4.19 及之后**
> **撰写日期**：2026-11-14
> **关联文档**：[仓库根 `process_group_cleanup.md`](../../process_group_cleanup.md)（改进方案，含 2026-11-14 孤儿进程事故记录与待实施项）、[windows-busybox-integration.md](./windows-busybox-integration.md)（Windows 平台壳层与进程树清理）
> **注意**：本文描述**当前已实现**的机制；标注 ⚠️ 的是已知缺口，均已在 `process_group_cleanup.md` 中立项，本文不重复方案细节。

---

## 0. 一句话结论

gtsp 对每条 execute_bash 命令做**独立进程组隔离**，通过四条回收路径清理：命令自然退出、超时处理、`process_stop` 显式终止、gtsp 退出时统一 `KillAll`。清理原语是「杀整组 / 整树」（Unix 进程组 kill / Windows `taskkill /T`），能覆盖命令 fork 出的全部子孙。当前仍有若干覆盖缺口（⚠️ §4），正在按 `process_group_cleanup.md` 补齐。

---

## 1. 进程模型：一条命令 spawn 时发生了什么

### 1.1 壳层与进程组

每次 `execute_bash` 调用（`src/tools/execute_bash.go`）：

1. **壳层分派**（`pal.ShellCommand`，按平台编译）：Unix 用 `bash -c <command>`；Windows 用 busybox `sh -c`（v0.4.19 起随 release 分发，见 windows-busybox-integration.md）。
2. **进程组隔离**（`pal.SetProcessGroup`，Start 前调用，execute_bash.go:120）：
   - **Unix**：`SysProcAttr{Setpgid: true}` —— 子 shell 建立独立进程组并当组长（pgid = 自身 pid），其后 fork 的一切子孙默认留在该组。这是组级 kill 的前提：既保证能一次杀整棵命令树，又不会误伤 gtsp 自身所在组。
   - **Windows**：空操作（无 Unix 式进程组概念），依赖杀进程时的树遍历（§3）。

### 1.2 输出捕获与等待语义 ⚠️

stdout/stderr 各接一个 `*ProcBuffer`（线程安全 bytes.Buffer，execute_bash.go:123–124）。由于是自定义 `io.Writer` 而非 `*os.File`，Go `os/exec` 会**隐式创建管道 + copy goroutine**，且 `cmd.Wait()` 的返回条件包含「copy goroutine 结束（即管道 EOF）」。

- 后果：任何继承了管道写端 fd 的子进程不死，EOF 不到，`Wait()` 不返回。
- `NewProcess`（`src/api/process.go:92`）启动的 wait goroutine 调 `cmd.Wait()`，结束后置 `done`、记录 exit code、`close(waitChan)`——**`WaitChan` 关闭 = Wait 返回**，两者同义。

### 1.3 注册表：谁会被统一清理跟踪到

`ProcessRegistry`（`src/api/process.go:71`）是全局单例。**只有后台路径会 `Register`**：

| 执行模式 | Register 时机 | 退出统一清理（KillAll）可见性 |
|---|---|---|
| `run_in_background: true` | 立即注册（execute_bash.go:136） | ✅ 可见 |
| 超时且 `timeout_action="background"` | 超时转后台时注册（execute_bash.go:177） | ✅ 可见 |
| 同步执行（默认 / `timeout_action="kill"`） | **不注册** | ❌ 不可见（正常情况下命令返回前已结束，无需清理；⚠️ 例外见 §4） |

注册表「只进不出」：已结束的进程条目不自动移除 ⚠️（GC 方案见 process_group_cleanup.md §3.5）。

---

## 2. 回收路径全景

四条路径，前两条针对单条命令，第三条针对显式请求，第四条是 gtsp 退出兜底：

| # | 路径 | 触发时机 | 代码入口 | 清理范围 |
|---|------|---------|---------|---------|
| 1 | **自然退出** | 命令（shell）自己退出，`cmd.Wait()` 返回 | `NewProcess` wait goroutine → `WaitChan` 关闭 | 仅 reap 主进程，**不清组内残余** ⚠️ |
| 2a | **超时转后台** | 同步等待超过 `task_timeout`（默认 180s，`timeout_action="background"` 默认） | execute_bash.go:176–184 | 不杀，登记为后台进程，继续运行 |
| 2b | **超时终止** | 同上但 `timeout_action="kill"` | execute_bash.go:168–175 | `KillProcessGroup` 杀整组 → 等 `WaitChan` → 返回部分输出 |
| 3 | **process_stop** | Agent 显式调用 `process_stop` 工具 | `src/tools/process_stop.go:62` → `bp.Kill()` | 杀整组；⚠️ 主进程已退出时提前 return 不杀（§4） |
| 4 | **退出统一清理（KillAll）** | 三个时机，见下 | `api/process.go:133` → 逐个 `bp.Kill()` | 遍历注册表（= 全部后台进程）逐个杀整组 |

路径 4 的三个触发时机：

| 触发时机 | 位置 | 说明 |
|---|---|---|
| 收到 SIGINT / SIGTERM | `src/main.go:189–195` | 信号处理器先 `KillAll` 再退出 |
| 客户端发 shutdown RPC | `src/api/dispatcher.go:437` | 显式协议级停机 |
| stdin EOF | `src/api/dispatcher.go:315` | 调用方进程消失/管道断开，注释明确说明动机：残留进程将成为调用方看不见的孤儿 |

### 2.1 清理原语

所有路径最终收敛到同一个原语（`src/pal/`）：

- **Unix**（pal_unix.go）：`KillProcessGroup(pid)` = `syscall.Kill(-pid, SIGKILL)`。负号 pid = 对整组发信号。**组长已退出时依然有效**：pgid 以组长 pid 命名，组长死后组仍存在，只要组内还有存活成员就能命中。
- **Windows**（pal_windows.go）：`taskkill /T /F /PID`——按调用时刻的父子链快照遍历整树强杀。**组长已退出时失效**（快照链断裂找不到孙进程），对已退出 pid 调用则静默失败 ⚠️（细节见 §3 与 process_group_cleanup.md §3.3）。

---

## 3. 平台差异小结

| 能力 | Unix | Windows |
|---|---|---|
| 组隔离 | ✅ Setpgid 真实进程组 | ❌ 无（SetProcessGroup 空操作） |
| 杀整树 | ✅ `kill(-pgid)`，组长死后仍有效 | ⚠️ `taskkill /T`，组长死后失效；对已退出 pid 静默失败 |
| gtsp 被硬杀（kill -9 / 崩溃） | ❌ 子进程全部孤儿化 | ❌ 同左（taskkill 依赖 gtsp 存活去执行） |
| 子进程主动 setsid 逃逸组 | ❌ 组级 kill 杀不到 | ❌ taskkill 快照在逃逸时刻之后同样找不到 |

Windows 的根治方案（Job Object，含 `KILL_ON_JOB_CLOSE` 的硬杀免疫）设计见 process_group_cleanup.md §3.3，未实施。

---

## 4. 已知缺口一览

以下缺口均已立项在 `process_group_cleanup.md`，此处仅列现象与影响：

| 缺口 | 现象 | 影响 |
|---|---|---|
| 同步完成路径不清理 | 命令正常/SIGPIPE 死亡后，组内 fork 的后台子进程存活 | **2026-11-14 事故的直接根因**：8 个忙等进程以 ~700% CPU 空转 2.5h |
| exec 隐式管道绑架 `Wait()` | 同步命令里 `cmd &` 且未重定向 stdout → 子进程持有管道写端，EOF 不到 | 同步调用假死到 180s 超时，清理代码走不到 |
| process_stop 提前短路 | 主进程已退出（IsDone）但孙进程存活 → 直接返回「already exited」 | Agent 显式终止请求清不掉残留（跨平台） |
| Windows taskkill 失效 | 组长先退 → 树链断裂；已退出 pid → 静默假成功 | 上述两类清理在 Windows 上不可靠 |
| gtsp 硬杀无防线 | kill -9 / 崩溃 → 注册表清理不执行 | 全部子进程孤儿化（Unix/macOS 无内核级兜底） |
| WebSocket 断连不清理 | WS 客户端断开但未发 shutdown | 会话后台进程挂到自然结束，无会话级回收 |
| 注册表只进不出 | 已结束进程条目不移除 | 内存缓慢泄漏；无人查询的运行中进程永久存活 |

---

## 5. 排障指引

怀疑有残留子进程时：

```bash
# Unix：按进程组查看（注意 PGID 一致的组员）
ps -eo pid,ppid,pgid,etime,pcpu,command | grep -v grep | head -30

# 找孤儿（PPID=1）
ps -eo pid,ppid,command | awk '$2==1'

# 按组清理（如确认是某次 execute 的残留）
kill -9 -<PGID>
```

- gtsp 会话日志：`STORAGE_ROOT/logs/backend/gtsp/gt-agent-*.log`，每条 execute 调用的入参与出参（含 stdout 的 Terminated 消息）都有记录，可反推泄漏来源与触发命令。
- Windows 上 `taskkill /T` 的失败会以 warning 形式输出到 gtsp 的 stderr（pal_windows.go），日志中可检索 `taskkill warning`。
