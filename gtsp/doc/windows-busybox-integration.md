# Windows 平台：集成 busybox 与子进程自动退出

> **状态**：设计文档，**尚未实现**。文中 Win32 API 签名、busybox-w32 行为均出自官方文档，**未经本机实测**；所有「待实测」条目在达成前不得当作结论引用。
> **适用版本**：gtsp v0.4.18 及之后
> **撰写日期**：2026-11-01

---

## 0. 一句话结论

gtsp 目前硬编码 `bash`，Windows 上「关 stdin 后清理后台进程」是**失效**的（CI 已实测证实）。修法分两层，**必须一起做**：

1. **壳层**：用 `busybox-w32` 让 Windows 上有一个自带命令集的 POSIX sh（论据见 §3）
2. **清理**：用 **Job Object** 让子进程在内核层面跟随 gtsp 一起退出（论据见 §4）

**只做第 2 层不够**：如果 `bash` 解析到 `C:\Windows\System32\bash.exe`（WSL），进程跑在虚拟机里，Job Object 管不到（§2.3）。
**只做第 1 层也不够**：现在 `KillAll()` 依赖「gtsp 有机会跑清理代码」，被 `kill -9` 时全泄漏（§4.1）。

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

### 1.2 根因：只记了「传话筒」的 PID

- GitHub Actions 的 `windows-latest` runner **自带 `C:\Windows\System32\bash.exe`**（WSL 入口）
- `exec.Command("bash", "-c", ...)` 解析到这个文件 → 它是个 **WSL 启动器**
- 启动器把命令交给 WSL 虚拟机里的 Linux 内核执行，**自己立刻退出**
- gtsp 记下的是**启动器的 Windows PID**，启动器一退出这个 PID 就没了
- 真正的 `sleep 30` 在 Linux 内核里，它会继承 stdout 管道句柄 → `cmd.Wait()` 收不到 EOF → 永久阻塞 → 测试 5 秒后判失败

**`taskkill /T /F` 为什么也不行**：`/T` 靠**父子关系**找树。父进程退出后 Windows 里的父子关系就断了，`/T` 立刻失能。这不是参数调优能解决的，是机制上限。

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

### 3.4 两个必须知道的路径坑

1. **反斜杠会被转成正斜杠**
   busybox 默认把 `\` 换成 `/`。**有些 Windows 程序不接受正斜杠路径参数** → 表现为「程序跑了但说找不到文件」。
   官方解法：启动时加 **`-X`** 关掉转换。
   **但这会让 shell 里的路径变成反斜杠**，`cd /c/foo` 之类可能反过来出问题。**必须实测**（§6 T4）。

2. **`D:` 不认**
   在 `cmd.exe` 里 `D:` 是切盘；在 POSIX shell 里会被解析成「**执行名为 `D:` 的程序**」→ 权限拒绝。
   正确写法：`cd /d/` 或 `cd D:/`。
   从 cmd 迁过来的用户必踩，**文档和错误提示都要写**。

### 3.5 集成方式选择

| 方式 | 说明 | 体积 | 可取性 |
|---|---|---|---|
| `//go:embed` 嵌入 **Windows 版** busybox | 运行时解压到临时目录再执行 | +1MB（仅 windows 构建） | ✅ **推荐** |
| 要求用户自行安装 | 与 Git Bash 方案合并 | 0 | 备选 |
| 打进源码（C 编译链） | 复杂度爆炸 | — | ❌ |

**平台隔离**：`//go:embed` 必须放在 `//go:build windows` 文件里，**否则 Linux/macOS 二进制也会白胖 1MB**。这是关键约束。

**嵌入 gzip 压缩版**可降到约 700KB，但运行时需解压到 `%TEMP%` 并校验 —— 增加启动延迟与清理负担。**先用原始 embed 测体积，达标再优化。**

### 3.6 shell 调用点的改动

```go
// src/tools/execute_bash.go:87 现状
cmd := exec.Command("bash", "-c", p.Command)
```

改为平台分派：

```go
// src/pal/pal.go 增加（unix 返回 nil，表示用默认 bash）

// src/pal/pal_windows.go
func ShellCommand(script string) (*exec.Cmd, error) {
    shPath, err := ensureBusybox()   // embed 解压 + 校验，返回可执行路径
    if err != nil {
        return nil, err
    }
    return exec.Command(shPath, "sh", "-c", script), nil
}
```

`execute_bash.go` 侧：

```go
cmd, err := pal.ShellCommand(p.Command)   // unix 实现返回 exec.Command("bash","-c",script)
if err != nil {
    return nil, fmt.Errorf("failed to prepare shell: %v", err)
}
```

**契约变更（必须写进文档和 schema）**：`execute_bash` 的 `command` 描述从「bash command」改成「**POSIX sh command**」，并明确列出保证可用的 applet。

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

### 5.1 分阶段

| 阶段 | 内容 | 产出 | 风险 |
|---|---|---|---|
| **P0 止血** | `pal_windows.go` 上 Job Object + `ReleaseProcess`；`execute_bash.go` 加 2 行 | Windows 清理生效于 Git Bash 场景 | 低（有降级路径） |
| **P0 测试修** | `test/integration/stdio_limits_test.go:237` 的 `isUnix()` 改 `runtime.GOOS != "windows"` | CI 立即变绿 | 无 |
| **P1 集成 busybox** | `//go:embed` + `ensureBusybox()` + `ShellCommand` 平台分派 | Windows 无需外部 bash | 中（+1MB、GPL） |
| **P1 契约更新** | schema 描述 / `execute_bash.md` / README 改成 POSIX sh + applet 清单 | 用户预期对齐 | 无 |
| **P2 加固** | WSL 检测拒绝、`CREATE_SUSPENDED` 版本、`BB_OVERRIDE_APPLETS` 相关文档 | 消除静默错路径 | 低 |

**注意 P0 的顺序**：先让 CI 绿（那一行改），再上 Job Object。否则改动期间 CI 一直红，**看不出新改动是不是又引入了失败** —— 这正是「恒真断言」类问题的温床（见 §7）。

### 5.2 `isUnix()` 那一行的说明

`test/integration/stdio_limits_test.go:237`：

```go
func isUnix() bool {
    return true // 该测试文件仅在 unix 构建中跑（依赖 sleep / 进程组）
}
```

**这个注释是错的**：该文件**没有任何 build tag**，Windows 照样编译执行，`return true` 让 skip 永远不生效。
- 为什么不用 build tag：文件里另外两个测试（64KB 上限、超限退出）**平台无关**，加 tag 会一并排除
- 所以「函数内跳过」的思路是对的，**实现是错的**
- **这是第 8 类「注释声称的约束没人兑现」**（§7）

改法：`return runtime.GOOS != "windows"`，注释同时改成真话。

### 5.3 代码量估算

| 项 | 量 |
|---|---|
| `pal_windows.go`（Job Object） | ~90 行 |
| `pal_unix.go`（no-op 桩） | +3 行 |
| `execute_bash.go` | +4 行 |
| `process.go` | +1 行 |
| busybox embed + 解压校验 | ~80 行 |
| 新增依赖 | **0** |
| 二进制体积 | Linux/macOS **+0**；Windows **约 +1MB** |
| Linux/macOS 回归风险 | **零**（`pal_unix.go` 只加 no-op） |
| 工期 | P0 半天；P1 1~1.5 天；P2 1 天 |

### 5.4 ⚠️ GPLv2 法务风险（阻塞项）

busybox 是 **GPLv2**。把它嵌进 gtsp 二进制**分发**，触发 GPL 的传染性义务。**存在争议点**：
- 「同一可执行文件内嵌」通常视为**单一作品** → 整个 gtsp 需按 GPLv2 发布
- 「独立 exe 运行时解压」可能被视为 **aggregate**（聚合）而非衍生 → 争议大
- **这一条必须由法务/Operator 拍板，不由工程决定**

**若 GPL 不可接受**，替代路径：
1. **Git Bash 方案**（§2.2）—— 零体积、零许可问题，代价是**要求用户安装**
2. **纯 Go 解释器 + 命令翻译层**（`mvdan.cc/sh`，BSD-3）—— 但 `ls`/`sleep` 等仍需自己用 Go 实现（工作量远大于 1MB）
3. **busybox 作为可选外部依赖** —— gtsp 不打包，检测到就提示下载

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
