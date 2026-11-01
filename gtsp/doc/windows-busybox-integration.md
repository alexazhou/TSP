# Windows 平台：集成 busybox 与子进程自动退出

> **状态**：方案 B（release 附带独立 busybox）**已实现并发布**（gtsp v0.4.19）。
> Windows CI 三个平台全绿，进程树清理问题经实测已随 busybox 解决，Job Object **未实施**（作为后备方案保留在 §4）。
> 文中路径行为、`-X` 效果等条目均为 GitHub 托管 `windows-latest` runner 上的**实测**结果（见 §3.4 与 `test/integration/busybox_windows_probe_test.go`）。
> **适用版本**：gtsp **v0.4.19 及之后**（v0.4.18 不含 busybox，其 Windows 版 `execute_bash` 无可用 shell）
> **撰写日期**：2026-11-01（2026-11-01 更新为实测结论）

---

## 0. 一句话结论

> **实施状态（2026-11-01）**：**第 1 层已实现并提交**（busybox 随 release 分发 + 平台分派 +
> `isUnix()` 恒真修复 + Windows 端两条新测试）。**第 2 层（Job Object）暂不实施**，
> 作为「若 `TestWindowsEOFKillsBackgroundTree` 在 CI 上失败」时的后备方案，设计保留在 §4。

gtsp 原先硬编码 `bash`，Windows 上「关 stdin 后清理后台进程」是**失效**的（CI 已实测证实，§1）。修法分两层：

1. **壳层（已做）**：用 `busybox-w32` 让 Windows 上有一个自带命令集的 POSIX sh，并作为
   独立文件随 release 分发（§3）
2. **清理（后备）**：用 **Job Object** 让子进程在内核层面跟随 gtsp 一起退出（§4）。
   引入 busybox 后 `ash` 在复合命令下作为父进程存活，`taskkill /T /F` 大概率够用，
   故先不实施；由 Windows 端的新测试来判定是否真的够用。

**只做第 1 层为什么可能够**：见 §1.2.1 的实测。
**若第 1 层不够**：`KillAll()` 依赖「gtsp 有机会跑清理代码」，被 `kill -9` 时全泄漏（§1.4），
那时必须上第 2 层。

---

## 1. 问题现状（已实测）

### 1.1 CI 实测证据

`Test gtsp` / `windows-amd64` 在 `bdceb52`（v0.4.18）上 **failure**，日志原文（`gh run view 41274512587`）：

```
=== RUN   TestServeStdioKillsBackgroundProcessesOnEOF
2026/11/01 04:49:34 stdin closed (EOF), server exiting
2026/11/01 04:49:35 stdin closed (EOF), server exiting
ERROR: The process "7768" not found.
taskkill warning: exit status 128
ERROR: The process "7768" not found.
taskkill warning: exit status 128
    stdio_limits_test.go:233: stdin EOF 后后台进程仍在运行（未被 KillAll 清理）
--- FAIL: TestServeStdioKillsBackgroundProcessesOnEOF (5.28s)
```

三条从日志直接读出的事实：

| 观察 | 结论 |
|---|---|
| `stdin closed (EOF), server exiting` 出现 | **`ServeStdio` 在 Windows 上是正常的**，退出路径没问题 |
| 测试走到了 `:233`（而不是 `:206`/`:213`） | 说明 `execute_bash` **拿到了 `process_id`**，即 `bash` **启动成功了** |
| `taskkill` 报 `process not found` | 记录的 PID **已经不是活着的进程** |

→ 「Windows 上没有 bash 所以启动失败」这个解释是**错的**。命令真跑起来了，坏的是清理。

### 1.2 根因：清理按 PID，而 PID 在清理那一刻已失效

> **勘误（2026-11-01，gtsp 测试复核后更正）**：本节初版写的是「`bash` 解析到 WSL 的
> `System32\bash.exe`，它交给 Linux 虚拟机后立刻退出」。该描述**未经验证**，且与
> CI 现场的若干细节不自洽（GitHub 托管 Windows runner 默认**不装 WSL**）。以下为
> 复核后的、与日志一致的机制描述。

CI 日志里能直接确认的三件事：

| 观察 | 含义 |
|---|---|
| `TestExecuteBashHandler` 等 **122 个子测试 PASS**，`executable file not found` 出现 **0 次** | `bash` **启动成功**，命令真跑了 |
| 失败行号是 `:233`（`KillAll` 后进程仍在跑），而非 `:208`（拿不到 background 结果） | 启动、注册、返回 `process_id` 全部正常 |
| `ERROR: The process "7768" not found.` + `taskkill warning: exit status 128` 各出现 **2 次**，且都是同一个 PID | `KillAll` **确实被执行了**，但按 PID 的清理**打空了** |

机制链：

1. Windows 上 `pal.KillProcessGroup` 走 `taskkill /T /F /PID <pid>`，**以 PID 存在性为前提**；
   PID 一旦失效，`/T` 也找不回整棵树。
2. 而子进程仍活着，并且**继承了 stdout 管道写端**。
3. `exec.Cmd.Wait()` 对非 `*os.File` 的 Stdout 会等内部 copy goroutine 读到 EOF；
   只要写端还开着就**永不返回**。
4. `Wait()` 不返回 → `waitChan` 不关闭 → `:233` 的 `case <-target.WaitChan()` 超时 5s → Fatal。
5. Unix 走 `kill(-pgid, SIGKILL)`，**依赖进程组标识而非某个 PID 存活**，所以同样
   「壳进程已退出」也能清干净 → Linux/macOS 双绿。

> 「Windows 上 PID 为什么在清理时已经失效」的**最后一环尚未实证**（推断是
> `C:\Program Files\Git\bin\bash.exe` 作为 wrapper 再拉起 `usr\bin\bash.exe`，
> wrapper 先退出）。但**两条修法都不依赖这一环**：busybox 让 shell 作为父进程存活，
> Job Object 则根本不按 PID 清理。

### 1.2.1 引入 busybox 为何能改善这一点

实测（Linux 容器内的同版本 busybox，行为与 w32 版一致）：

```
sh -c 'sleep 30'        → 只有 1 个进程（sh 被 exec 替换成 sleep）
sh -c 'sleep 30; true'  → 2 个进程：sh(父) + sleep(子)
```

复合命令时 **ash 作为父进程存活**，所以 `KillAll` 执行的那一刻 PID 是有效的，
`taskkill /T /F` 能沿父子关系命中整棵树。这正是 `busybox_windows_eof_test.go` 里
`TestWindowsEOFKillsBackgroundTree` 断言的内容。

**但这是「大概率可行」而非「机制保证」** —— 若该测试在 CI 上失败，说明按 PID 清理
仍不可靠，那就必须上 Job Object（§4）。二者不是互斥方案，而是「先便宜后彻底」。

### 1.3 代码位置（v0.4.18）

| 文件 | 行 | 内容 |
|---|---|---|
| `src/tools/execute_bash.go` | 87 | `cmd := exec.Command("bash", "-c", p.Command)` —— **硬编码，无 `runtime.GOOS` 分支** |
| `src/tools/execute_bash.go` | 88 | `pal.SetProcessGroup(cmd)` —— Windows 实现是**空函数** |
| `src/pal/pal_windows.go` | 12–20 | `KillProcessGroup` → `taskkill /T /F /PID` |
| `src/pal/pal_windows.go` | 22–25 | `SetProcessGroup` → **空实现**（注释：「Windows 没有 Unix 式进程组」） |
| `src/api/process.go` | 64 | `BackgroundProcess.Kill()` → `pal.KillProcessGroup(pid)` |
| `src/api/dispatcher.go` | 315/437 | `GlobalProcessRegistry.KillAll()`（shutdown / EOF 两条路径） |
| `src/main.go` | 189 | 信号处理里 `KillAll()` |

### 1.4 「靠 gtsp 自己清」这件事本身就是漏洞

现状的自动退出依赖 `KillAll()` 被执行。但：

| 场景 | `KillAll()` 会跑吗 |
|---|---|
| 正常退出 | ✅ |
| stdin EOF | ✅（`dispatcher.go:437`） |
| 收到 SIGINT/SIGTERM | ✅（`main.go:189`） |
| **被 `kill -9` / SIGKILL** | ❌ **一句都不跑** |
| **panic 导致进程直接中止** | ❌ |
| **Windows 任务管理器「结束任务」/ `taskkill /F`** | ❌ |

→ 只要 gtsp 不是优雅退出，后台进程 100% 泄漏。**这是设计缺陷，不是实现 bug。**

---

## 2. 三种 bash 来源的语义差异

Windows 上 `exec.LookPath("bash")` 可能命中的三个东西，语义完全不同。**这是选型的前提。**

### 2.1 WSL bash（`C:\Windows\System32\bash.exe`）

| 维度 | 行为 |
|---|---|
| 命令跑在哪 | **WSL2：独立轻量虚拟机内的真 Linux 内核** |
| Windows 能否看到该进程 | ❌ `tasklist` 查不到，只有 `bash.exe` 一个「传话筒」 |
| 命令的根目录 | `/` = WSL 的 `ext4.vhdx` 虚拟磁盘 |
| Windows 的 `C:\Users\x` 在命令里 | **`/mnt/c/Users/x`**（drvfs 挂载，可能慢或挂载失败） |
| `pwd` 默认 | `/home/<user>`，与 Windows 用户目录无关 |
| 与 gtsp 的 `read_file`/`write_file` 是否同一路径空间 | ❌ **不是** |
| Job Object 能覆盖 | ❌ **不能**（Linux 进程不在 Windows 内核里） |
| `taskkill /T /F` 能覆盖 | ❌ **不能**（见 §1.2） |

> **WSL1 例外**：WSL1 用 `lxcore.sys` 做系统调用翻译，无虚拟机；Linux 进程以「pico 进程」形式**出现在 Windows 进程列表里**，PID 能对上。但文件系统视图**仍与 Windows 分离**。
> 判据：`wsl.exe -l -v` 看 `VERSION` 列。**本 runner 上是 1 还是 2，待实测**（§6 T1）。

**这是最危险的一种**：命令「能跑通」，但用户以为在操作当前目录，实际在另一个文件系统里。**静默跑错地方**比直接报错难查得多。

### 2.2 Git Bash（`C:\Program Files\Git\bin\bash.exe`）

| 维度 | 行为 |
|---|---|
| 命令跑在哪 | **Windows 原生进程**，与 gtsp 同一内核 |
| Windows 能否看到 | ✅ `tasklist` 可见 |
| 命令的根目录 | `/` = Git 安装目录；`C:\` = **`/c/`** |
| 与 gtsp 的路径空间 | ✅ **同一套** |
| Job Object 能覆盖 | ✅ **能**（全树进 Job） |
| `taskkill /T /F` 能覆盖 | ✅ 通常能（父进程活着时） |

**注意**：`bash.exe` 依赖同目录的 `msys-2.0.dll` 与 `/usr/bin`，**单独拷一个 exe 过去跑不起来**。要求用户走正规安装，错误提示里必须写清。

### 2.3 busybox-w32（本文推荐，集成进 gtsp）

| 维度 | 行为 |
|---|---|
| 命令跑在哪 | **Windows 原生进程**（WIN32 API，非模拟层） |
| shell | `ash`，**POSIX 兼容**，不支持 bash 专属语法（`[[ ]]`、数组） |
| 命令集 | 内置 **200+ applet**（`ls`/`cat`/`grep`/`sed`/`awk`/`find`/`sleep`/`ps`/`kill`…） |
| 与 gtsp 的路径空间 | ✅ **同一套 `C:\`** |
| Job Object 能覆盖 | ✅ **能** |
| 体积 | **约 +1MB** |
| 许可 | **GPLv2**（需法务确认，见 §5.4） |

### 2.4 对照总表

| | WSL bash | Git Bash | busybox-w32 |
|---|---|---|---|
| 命令实际运行位置 | Linux VM | Windows | Windows |
| Windows `tasklist` 可见 | ❌ | ✅ | ✅ |
| 与 `read_file`/`write_file` 同一路径空间 | ❌ | ✅ | ✅ |
| Job Object 有效 | ❌ | ✅ | ✅ |
| 命令集保证存在 | ✅ | ✅ | ✅（内置） |
| 需要用户额外安装 | runner 自带 / 用户需装 WSL | **需要** | **不需要** |
| 体积 | 0 | 0 | **+1MB** |
| 许可 | 无 | 无 | **GPLv2** |

**结论**：只有 busybox-w32 与 Git Bash 两条路是「同一路径空间 + Job Object 有效」的。差别在于 busybox 能打进二进制（零安装），Git Bash 要用户装。

---

## 3. busybox-w32 集成设计

### 3.1 为什么需要它

Windows 原生没有 bash。若只用纯 Go 解释器（如 `mvdan.cc/sh`），**只解决语法解析**，`ls`/`cat`/`sleep` 依然要靠 PATH 里恰好有 —— 而 Windows 的 `PATH` 里通常**一个都没有**。busybox 把 shell **和** 命令集一起带上，一次解决两层。

### 3.2 PATH 与环境变量行为

busybox-w32 是原生 PE 程序，**读的就是 Windows 的 `PATH`**：

- Windows 环境变量全部**转成大写**
- `PATH` **同时接受 `;`（Windows）和 `:`（POSIX）**两种分隔符
- **PATH 里的 `.exe` / `.bat` / `.cmd` 可以直接调用**，跑起来是原生 Windows 进程

→ `git`、`node`、`python`、`cargo` 这些用户装的程序**能直接调到**。

### 3.3 ⚠️ 内置 applet 会「抢名字」

官方文档原话：

> **busybox-w32 prefers built-in applets to external programs when running commands.**

含义：

| 用户敲 | 实际执行 |
|---|---|
| `ls` | busybox 内置 `ls`，**不是** `C:\Program Files\Git\usr\bin\ls.exe` |
| `grep` / `sed` / `awk` / `find` / `sleep` / `ps` / `kill` … | 同上，全部内置优先 |

- **好处**：`ls` 一定存在，不依赖用户装了啥
- **代价**：用户以为跑的是 Git 的 `ls`，实际是 busybox 的 `ls`；**选项支持度、输出格式可能有差异**
- **想抢回外部程序**：设环境变量 `BB_OVERRIDE_APPLETS="ls grep"`（空格分隔的 applet 名列表）

### 3.4 路径行为（**已实测，2026-11-01，windows-latest / busybox64u**）

> 本节数据来自 `test/integration/busybox_windows_probe_test.go` 在 GitHub 托管
> `windows-latest` runner 上的真实执行结果（run 41281531655）。此前本节的三条说法
> **全部被实测推翻或修正**，已按下表改写 —— 教训：路径行为不能靠社区文章推断。

| 写法 | 实测结果 | 结论 |
|---|---|---|
| `cd /c/` | rc=2 `can't cd to /c/: No such file or directory` | ❌ **不支持**（Cygwin/MSYS 形式在 busybox-w32 上无效） |
| `cd c:/` | rc=0，`pwd` → `C:/` | ✅ 可用 |
| `cd c:\` | rc=0，`pwd` → `C:/` | ✅ 可用 |
| `cd C:`（只有盘符） | rc=0，`pwd` → `C:/` | ✅ **可用**（旧文说会被当程序执行，错） |
| `test -d 'c:/Windows'` | `YES` | ✅ 绝对路径 A 形式可用 |
| `test -d '/c/Windows'` | `NO` | ❌ 同上 |
| `test -d '//c/Windows'` | `NO` | ❌ UNC 形式也不接受盘符 |
| `pwd` | `D:/a/TSP/TSP/gtsp/test/integration` | 输出即 **`D:/...` 正斜杠形式**，盘符保留 |
| `echo 'C:\Windows\System32'` | `C:\Windows\System32`（原样） | 引号内不转换 |
| `printf '%s\n' C:\Windows\System32`（不加引号） | `C:WindowsSystem32` | ⚠️ **反斜杠被 shell 当转义吃掉了**，不是 busybox 转换的问题 |
| `cygpath` 是否可用 | `YES` | 可用（但无需用它，见下） |
| `realpath c:/Windows` | `C:/Windows` | 正斜杠形式全程贯通 |
| `$TMPDIR` / `$TEMP` / `$TMP` | `TMPDIR` 空；`TEMP`/`TMP` = `C:/Users/RUNNER~1/AppData/Local/Temp` | ⚠️ 用 `$TMPDIR` 会拿到空串 |

**唯一需要记住的三条**：

1. **用 `c:/path`，不要用 `/c/path`**。前者可用，后者确定不可用（rc=2）。
2. **不加引号的反斜杠会被 shell 吞掉**（`C:\Windows` → `C:WindowsSystem32`）。
   这跟 busybox 的 `-X` 无关，是 shell 的转义规则。要么用正斜杠，要么加引号。
3. **`-X` 的真实效果有限**：实测 `sh -X -c 'printf "%s\n" C:\Windows\System32'`
   与不加 `-X` 输出完全一致（都是 `C:WindowsSystem32`），因为反斜杠在进 busybox
   之前就被 shell 处理掉了。**`-X` 只在「反斜杠作为字面量传进参数」时才起作用**，
   不是「关掉它路径就对了」的开关。文档与工具描述都不应承诺它是通用解药。

**结论性建议**：工具描述里只承诺 `c:/path`。不要提 `/c/` —— 它会失败。

### 3.5 集成方式选择（已定：方案 B，随 release 分发）

> **决策记录（2026-11-01，Operator 拍板「方案 B」）**：不嵌入 gtsp 二进制，改为
> **release 中附带独立文件 `busybox64u.exe` + `COPYING.busybox`**。理由见本节末尾的
> GPL 分析。§4 的 Job Object 仍未实施，作为「按 PID 清理在 CI 上失败」时的后备。

| 方式 | 说明 | Windows 体积 | GPL 风险 | 取值 |
|---|---|---|---|---|
| **B（已采用）** | release 附带独立 exe，与 gtsp.exe 同目录 | **0** | ✅ **零**（教科书级 aggregate） | ✅ |
| A | `//go:embed` 进 exe，运行时落盘 | +670 KB（未压缩）/ +411 KB（gzip） | ⚠️ 有争议（同一文件） | 备选 |
| C | 不打包，检测不到给下载指引 | 0 | ✅ 零 | 备选 |

**为什么 A 的体积估算曾写成 +1MB**：初稿没实测。现测值（同 go1.26.1 基线对比）：
直接 embed `busybox64u.exe`（683,520 B）→ **+670.5 KB**；gzip 后 → **+411.0 KB**。

**为什么选 B**：GPLv2 的 `mere aggregation` 条款明确允许「两个独立程序在同一介质上
一起分发」互不传染，FSF 官方 FAQ 也认可「fork + exec + 管道通信」= 聚合。而
「编译进同一可执行文件」是最黑的灰色地带（FSH 立场是衍生作品）。
→ **落盘/独立文件对 GPL 更友好，不是更糟。** 代价只是「单文件部署」变「双文件」。

**平台隔离不变**：即便将来切换回 A，`//go:embed` 也必须放在 `//go:build windows`
文件里，否则 Linux/macOS 二进制也会白胖。

### 3.6 shell 调用点的改动（已实现）

```go
// src/tools/execute_bash.go（原第 87 行）
//   cmd := exec.Command("bash", "-c", p.Command)
cmd, err := pal.ShellCommand(p.Command)
if err != nil {
    return nil, fmt.Errorf("failed to prepare shell: %v", err)
}
pal.SetProcessGroup(cmd)
```

平台分派（新增 `src/pal/shell_unix.go` / `src/pal/shell_windows.go`）：

| 平台 | 行为 |
|---|---|
| Unix | `exec.LookPath("bash")` → `bash -c script`（与改动前完全一致，零回归） |
| Windows | 按 `GTSP_BUSYBOX` → exe 同目录 → `PATH` → 回退 `bash`（拒绝 WSL）→ `busybox sh -c script` |

**契约变更**：`execute_bash` 的 `command` 在 Windows 上是 **POSIX sh**（busybox ash），
不支持 `[[ ]]`、数组、`{1..10}` 花括号展开、`$'...'`。已在 `README.md` 的
「Windows: the shell and `busybox64u.exe`」一节写明，并列出保证可用的 applet。

> **顺带修掉一处 bash 依赖**：`test/unit/shell_test.go` 的 truncation 用例原本用
> `for i in {1..2000}`，brace expansion 在 ash 下不展开 → 只输出 1 行 →
> 「输出被截断」的断言在 Windows 上会**静默失去意义**（不是失败，是空跑）。已改为
> POSIX `while` 循环。

---

## 4. Job Object 设计

### 4.1 Job Object 是什么

Windows 内核的**进程容器**。把进程塞进去，效果两条：

1. **它生的所有后代自动进同一个 Job** —— 不需要追踪 PID、不需要遍历进程树
2. Job 上开 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE(0x2000)` 后，**Job 的最后一个句柄被关闭 = 内核立刻杀掉里面全部进程**

第 2 条是「自动退出」的实现核心。gtsp 持有句柄，**gtsp 一死（正常退出 / panic / `kill -9` / 崩溃 / 任务管理器强杀），Windows 回收其内核句柄 → Job 关闭 → 里面全死**。

### 4.2 与现状的能力对比

| 场景 | 现状 `KillAll()` | + Job Object |
|---|---|---|
| gtsp 正常退出 | ✅ | ✅ |
| gtsp 被 `kill -9` | ❌ **全泄漏** | ✅ **内核兜底** |
| gtsp panic | ⚠️ defer 未必来得及 | ✅ |
| 父进程先退出、孙子还在 | ❌ `taskkill /T` 靠父子关系，关系断则失能 | ✅ **靠内核记账** |
| 子进程再 fork 出孙子 | ❌（进程组在 Windows 不存在） | ✅ 自动覆盖 |

### 4.3 需要的四个 API

| API | 作用 |
|---|---|
| `CreateJobObjectW(nil, nil)` | 建匿名 Job，返回句柄（默认 `bInheritHandle=FALSE`） |
| `SetInformationJobObject(h, JobObjectExtendedLimitInformation(9), &info, size)` | 设 `LimitFlags = 0x2000` |
| `AssignProcessToJobObject(hJob, hProc)` | 把进程塞进 Job；`hProc` 必须带 `PROCESS_SET_QUOTA(0x0100)` + `PROCESS_TERMINATE(0x0001)` |
| `TerminateJobObject(hJob, 1)` | 主动连锅端（替代 `taskkill`，不依赖外部命令、不看父子关系） |

**零新依赖方案**：用 `syscall.NewLazyDLL("kernel32.dll")` + `NewProc` 手调上述 API。
> 当前 `gtsp/go.mod` **只有** `doublestar/v4` 和 `gorilla/websocket`，**没有 `golang.org/x/sys`**。引入 x/sys 会新增依赖，与「零依赖单文件」定位冲突 → 用手写 `LazyDLL` 方案，保持零依赖。

### 4.4 接口时序调整

`pal` 现在的契约是「`Start` 前设一次、退出时按 PID 杀」。Job 需要**进程句柄**才能塞进去，而句柄 `Start()` 之后才有。所以新增两个钩子：

```go
// pal.go（两个平台都要有；unix 全是 no-op）

func AttachProcess(cmd *exec.Cmd) error   // Start() 之后调用
func ReleaseProcess(pid int)              // Wait 返回后调用，释放句柄
```

`execute_bash.go` 只加两处：

```go
if err := cmd.Start(); err != nil { ... }

// 新增：失败不阻塞 tool 调用，降级到 taskkill
if err := pal.AttachProcess(cmd); err != nil {
    log.Printf("job object attach failed (fallback to taskkill): %v", err)
}
```

`process.go` 的 Wait goroutine 里加 `defer pal.ReleaseProcess(pid)`，否则**每起一个后台进程泄漏一个句柄**。

### 4.5 五个必踩的坑

#### 坑 1：`AssignProcessToJobObject` 报 `ERROR_ACCESS_DENIED(5)`

**原因**：gtsp 自己已在某个 Job 内，且不允许嵌套。
**现实**：GitHub Actions runner、Windows 服务、部分终端**都在 Job 内**。Win8+ 支持嵌套 Job，一般能成功，**但不能假设**。
**处置**：失败**不得**让 tool 调用报错 —— 降级 `taskkill` + 打 warning。功能退化而非功能挂掉。

#### 坑 2：句柄提前关闭 → 子进程秒死

`KILL_ON_JOB_CLOSE` 是「最后一个句柄关闭就杀」。句柄必须挂在**进程生命周期容器**里（如 `map[pid]syscall.Handle`），只在 `ReleaseProcess` 里关。**一旦提前关 = 立刻杀掉全部子进程。**

#### 坑 3：`Start()` 之后才绑 → 已 fork 的孙子漏网

`Start()` 返回时子进程可能已 spawn 后代，**已存在的进程不会自动进新 Job**。
- 现行窗口：微秒~毫秒（`sleep 30` 至少几毫秒才 fork）
- **严格解**：`CreationFlags |= CREATE_SUSPENDED(0x4)` → 绑定 → `ResumeThread`。但 Go 不暴露主线程句柄，需 `CreateToolhelp32Snapshot` + `Thread32First` 枚举，约 +40 行
- **建议**：先上简单版，当作已知的微小窗口记录；实测漏网再补

#### 坑 4：子进程继承了 Job 句柄 → `KILL_ON_JOB_CLOSE` 永不触发

「最后一个句柄关闭」才触发。`CreateJobObject` 默认不继承，**但别在 `SysProcAttr.AdditionalInheritedHandles` 里传进去**。

#### 坑 5：**Job Object 管不到 WSL**（最重要）

WSL2 是独立 VM，Linux 进程在另一个内核。Job 里只有 `bash.exe` 传话筒。传话筒死了，里面 `sleep` 照样跑到天亮。
**处置**：`exec.LookPath("bash")` 命中 `System32\bash.exe` 时**明确报错**，给出两条出路（用 linux 版进 WSL / 装 Git Bash）。**这件事必须和 Job Object 一起做，否则白干。**

> **采用 busybox 后，这个坑自动消失** —— 因为不再调 `LookPath("bash")`，走的是自带的 busybox 路径。但仍建议保留 WSL 检测作为**显式错误**：若 `ShellCommand` 降级回退到 `LookPath`，必须拒绝 WSL。**防御「未来有人图省事又改回 LookPath」。**

---

## 5. 实施计划

### 5.1 分阶段（含实施状态）

| 阶段 | 内容 | 状态 |
|---|---|---|
| **P0 测试修** | `isUnix()` → `runtime.GOOS != "windows"` | ✅ **已完成** |
| **P1 集成 busybox** | `third_party/busybox/` + `pal.ShellCommand` 平台分派 + release 附带二进制 | ✅ **已完成** |
| **P1 契约更新** | schema 描述 / README 改成 POSIX sh + applet 说明 | ✅ **已完成** |
| **P1 测试护栏** | `TestBusyboxBinaryUsable`（sha256 + applet + POSIX 语义）、`TestWindowsEOFKillsBackgroundTree`（清理是否真生效）、`pal` 单测（WSL 检测 / GTSP_BUSYBOX 覆盖不静默失效） | ✅ **已完成** |
| **P2 后备：Job Object** | 见 §4。**仅当 `TestWindowsEOFKillsBackgroundTree` 在 CI 上失败时启动** | ⏸ **未实施** |
| **P2 加固** | `CREATE_SUSPENDED` 版本、`BB_OVERRIDE_APPLETS` 相关文档 | ⏸ 未实施 |

**顺序说明**：先修 `isUnix()` 让 CI 恢复可读（否则改动期间一直红，看不出新改动是不是又
引入了失败 —— 这正是「恒真断言」类问题的温床，见 §7）。Job Object 从「必做」降级为
「由测试结果决定」，因为引入 busybox 改变了进程结构（§1.2.1）。

### 5.2 `isUnix()` 那一行的说明（已修）

`test/integration/stdio_limits_test.go`：

```go
// 修前
func isUnix() bool {
    return true // 该测试文件仅在 unix 构建中跑（依赖 sleep / 进程组）
}

// 修后
func isUnix() bool {
    return runtime.GOOS != "windows"
}
```

**原来那句注释是错的**：该文件**没有任何 build tag**，Windows 照样编译执行，`return true`
让 skip 永远不生效。
- 为什么不用 build tag：文件里另外两个测试（64KB 上限、超限退出）**平台无关**，加 tag 会一并排除
- 所以「函数内跳过」的思路是对的，**实现是错的**
- **这是第 8 类「注释声称的约束没人兑现」**（§7）

同仓库其它 5 处早就是正确写法（`shell_test.go:396/405/418`、`sandbox_test.go:42`、
`read_image_test.go:32`），说明不是「不会写」，是这一处写错。

### 5.3 实际代码量

| 项 | 实际 |
|---|---|
| `src/pal/shell_windows.go` | 130 行（含注释） |
| `src/pal/shell_unix.go` | 19 行 |
| `src/pal/shell_windows_test.go` | 82 行 |
| `test/integration/busybox_windows_test.go` | 96 行 |
| `test/integration/busybox_windows_eof_test.go` | 89 行 |
| `execute_bash.go` | 净 +3 行（`exec.Command` → `pal.ShellCommand`） |
| `third_party/busybox/` | `busybox64u.exe`（683,520 B）+ `COPYING` + `README.md` |
| 新增 Go 依赖 | **0** |
| 二进制体积 | Linux/macOS/darwin **+0 字节**（embed 方案已否决）；仓库 +683 KB |
| Linux/macOS 回归风险 | **零**（新文件是 `//go:build !windows`；测试已全绿） |

### 5.4 GPLv2：已通过「方案 B」规避

busybox 是 **GPLv2**，gtsp 是 **MIT**。**不嵌入**二进制、**作为独立文件**随 release 分发，
属于 GPLv2 `mere aggregation`（聚合），确认不触发传染：

- `busybox64u.exe` 是独立可执行文件，由 gtsp `fork + exec` 调用、经管道通信
- FSF GPL FAQ 认可「exec + 管道」为聚合而非合并
- release 同时提供 `COPYING.busybox`（GPLv2 全文），满足 GPLv2 §3 的源码/许可告知义务
- 文件来源、sha256、更新步骤写在 `third_party/busybox/README.md`

**若将来要改成嵌入（方案 A），这个结论失效**，必须重新做法务确认。

---

## 6. 验证计划（落地前必须全部跑完）

> **原则**：文档里的推断不算证据。每条都要在 Windows runner 上**现跑现看**。

| # | 验证项 | 命令 / 方法 | 判定标准 |
|---|---|---|---|
| **T1** | runner 上 WSL 是 1 还是 2 | `wsl -l -v` | 记录 `VERSION` 值 |
| **T2** | 命令到底跑哪一侧 | `tasklist \| findstr sleep` + `wsl -e ps -ef \| grep sleep` | 两侧是否都有 |
| **T3** | 现有 `taskkill /T /F` 的真实覆盖 | 起 `sleep 30` → `taskkill` → 两侧各查一次 | 记录残留 |
| **T4** | busybox `-X` 选项影响 | 带路径参数调外部 `.exe`，开关 `-X` 各一次 | 哪个能跑通 |
| **T5** | `BB_OVERRIDE_APPLETS` 是否真能抢回 `ls` | 放个假 `myls.exe` 进 PATH | 能否调到外部程序 |
| **T6** | Job Object 覆盖进程树 | 起 bash → 拉 2 个孙进程 → `TerminateJobObject` | 5s 内全消失 |
| **T7** | **`KILL_ON_JOB_CLOSE` 是否真兜底** | Job 建好后 `kill -9` 掉 gtsp 进程 → 查子进程 | **子进程必须死**。这是整套方案的核心假设 |
| **T8** | Git Bash 场景重复 T2/T3/T6 | runner 自带 Git Bash | 拿到「同屋」场景确定性结论 |
| **T9** | 体积实测 | `GOOS=windows go build` 前后对比 | 与 +1MB 估算是否一致 |

**T7 是命门**：如果 `KILL_ON_JOB_CLOSE` 在 gtsp 被强杀时不生效，整个方案的价值就只剩「杀得比 taskkill 干净」，而不是「内核兜底」。**这条没验证，方案不算成立。**

---

## 7. 方法论教训（本次踩到的）

写在这里是因为**它们会重犯**：

1. **注释不能替代机制**
   `isUnix() { return true // 该文件仅在 unix 构建中跑 }` —— 注释声称的约束**没有任何代码兑现**。同类问题已出现 8 次。**规则：凡是注释里写「仅在 X 下」「不会发生 Y」，必须指出兑现它的具体代码行，否则视为恒真。**

2. **命令没跑起来，不能读成「通过」**
   早期把 Windows 失败归因为「没有 bash」，实际日志显示 `process_id` 已返回、命令**真跑了**。**规则：结论必须能指出日志里的具体行；归纳性描述不算证据。**

3. **「能跑」和「跑对了」是两件事**
   WSL bash 让命令「能跑」，但跑在另一个文件系统。**静默错误比显式失败危害大一个量级。**

4. **改坏会不会红 / 保持原样会不会绿 / 保护措施生效点在前还是后**
   新增测试前必须自问这三条。`TestServeStdioKillsBackgroundProcessesOnEOF` 就是「保护措施生效点在平台之外」的典型。

5. **只认落盘后现算的终值**
   过程中打印的、推断的、文档抄的，一律标注「待验证」，不得直接写入结论。

---

## 8. 待决策项（需 Operator 拍板）

| # | 问题 | 备选 |
|---|---|---|
| **D1** | GPLv2 是否可接受？ | 接受 → busybox embed；不接受 → Git Bash + 要求用户安装 |
| **D2** | Windows 定位是「能用就行」还是「一等公民」？ | 前者 → 只做 Job Object + 拒绝 WSL；后者 → 上 busybox |
| **D3** | 工具契约改成 POSIX sh 是否可接受？ | 影响 `execute_bash` schema 与全部文档 |
| **D4** | 体积 7MB → 8MB 是否可接受？ | Windows 版单独增大，Linux/macOS 不变 |

**在 D1 澄清前，不实现 §3（busybox 集成）。D4 之外的 §4（Job Object）可以独立推进，与 D1 无关。**
