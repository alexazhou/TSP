# gtsp 端到端（E2E）测试设计

> **状态**：设计（待实施）
> **更新**：2026-11-14 补充 §6 临时目录与文件的清理逻辑（仅文档约定，不改代码）
> **更新**：2026-11-14 吸收 review 意见：S2 标注待实现并移出本期、S1 改用启动参数/sandbox RPC 下发、M1 断言对齐现有 KillSession 语义、L9 保留在 integration、TestMain 单次编译 + 每用例 --log-path、osSurvivors 改 PID 探测、P9 去 python 依赖、P5 显式超时、§5 第二批前置依赖已落地（commit 9c208d0）
> **更新**：2026-11-14 review 二次修正：§7/§2 osSurvivors 表述对齐（去 pgrep/tasklist 残留）、Windows OpenProcess 判活须加 WaitForSingleObject 二次确认、§5 第三批编号 L3–L8
> **撰写日期**：2026-11-14
> **关联文档**：[child-process-reclamation.md](./child-process-reclamation.md)（子进程回收机制）、[../process_group_cleanup.md](../process_group_cleanup.md)（孤儿进程清理方案）、[windows-busybox-integration.md](./windows-busybox-integration.md)
> **动机**：现有 CI（`test-gtsp.yml`）几乎全是进程内测试（import 源码直接调 Handler/函数），只有 `test/e2e/read_image_test.go` 一例真实进程黑盒测试，且仅覆盖单工具单场景；`script/test_script/` 下的 Python 脚本未接 CI、路径硬编码已失效。进程内测试验证不了：stdio 行协议、stdin EOF 停机、信号处理、真实进程树中的进程组行为、多会话隔离——这些恰恰是 gtsp 作为**子进程管理器**的核心职责。

---

## 0. 一句话结论

以现有 `read_image_test.go` 的模式为基础（真实 gtsp 二进制 + stdio 行协议对话），抽取通用 harness，把 **全部工具命令 + 生命周期 + 沙箱 + 多会话** 的黑盒场景做成 `test/e2e/` 下的 Go 测试套件，随现有 `go test ./test/...` 自动进三平台 CI 矩阵，不新增 workflow。

## 1. 测试形态与原则

| 维度 | 决策 | 理由 |
|---|---|---|
| 语言/载体 | Go，`test/e2e/` 包内 `*_test.go` | 与 CI 现有收集方式一致（`go test ./test/...`），三平台矩阵自动覆盖平台分支（bash vs busybox ash、进程组 vs Job Object） |
| 被测对象 | **真实编译的二进制**（`go build ./src` → 临时目录），非 `go run` | 启动开销固定且可控；`go run` 在 Windows 上有临时产物回收问题 |
| 二进制复用 | **`TestMain` 编译一次**到包级临时目录（结束时删除），各用例共用；`GTSP_BIN` 环境变量沿用（指向已构建二进制时跳过编译） | 避免每个用例各 `go build` 一次拖垮 <60s 预算（Windows 尤甚） |
| 日志隔离 | 各用例启动 gtsp 时**显式传 `--log-path <t.TempDir()>/logs`**（默认日志目录是 `<executable_dir>/logs`，二进制为包级共享时必须覆盖，否则多用例日志混写同一目录） | 日志按用例隔离，随 `t.TempDir()` 自动清理 |
| 对话方式 | stdio 行协议（initialize → tool 调用循环），另设 websocket 模式专项场景 | 两种运行模式都要覆盖 |
| 断言通道 | **双通道**：in-band（gtsp 返回值）+ out-of-band（OS 进程表 / 文件系统 / 日志文件） | in-band 验证协议正确性，out-of-band 验证真实效果——后者是 e2e 的核心价值 |
| 隔离 | 每个测试独立 `t.TempDir()` 作为工作目录，gtsp 以该目录为 cwd 启动 | 互不污染，失败残留可定位 |
| 超时纪律 | 每个 tool 调用带显式超时（默认 10s）；整个套件预算 <60s（CI 当前 `-timeout 120s` 含进程内测试） | 防 CI 卡死 |

## 2. 测试骨架（harness）

新增 `test/e2e/harness_test.go`，从 `read_image_test.go` 抽取并扩展：

```go
type TSPSession struct {
    cmd    *exec.Cmd
    stdin  io.WriteCloser
    reader *bufio.Reader
    logDir string // 便于断言会话日志
}

func startGTSP(t *testing.T, opts ...) *TSPSession
    // 使用 TestMain 预构建的二进制（GTSP_BIN 复用）→ 启动（显式 --log-path <t.TempDir()>/logs）→
    // 发送 initialize → 校验 serverInfo / 工具清单
    // opts: 模式（stdio / websocket）、工作目录、capabilities include/exclude、沙箱参数（--sandbox 等）

func (s *TSPSession) callTool(name string, params any, timeout ...) map[string]any
    // 发送 {"method":"tool", "tool":..., "input":...} → 读一行 JSON 响应 → 解析
    // 超时或连接死亡即 Fatal（区分错误信息）

func (s *TSPSession) callRaw(raw []byte, timeout) (string, error)
    // 发送非规范请求（malformed JSON / 未知 method），用于协议健壮性场景

func (s *TSPSession) closeStdin()               // 触发 EOF 停机路径
func (s *TSPSession) waitExit(timeout)          // 断言 gtsp 进程退出
func (s *TSPSession) killSignal(sig)            // 发 SIGTERM/SIGINT（Windows 上降级为 kill）

func osSurvivors(t *testing.T, pids []int) []int
    // OS 级进程断言，按 **PID 探测**而非按命令行/映像名过滤：
    // PID 来源：优先取 gtsp 返回的 leftover_processes 树中的 pid 集合；
    // 场景脚本自身把子进程 pid 写入工作目录临时文件（kill -9 之外最可靠的跨平台凭证）。
    // 探测方式：Unix kill(pid, 0)；Windows OpenProcess + WaitForSingleObject(h,0)==WAIT_TIMEOUT
    // （OpenProcess 成功≠存活：进程已退出但句柄未关时内核对象仍在，需二次确认再 CloseHandle）
    // 精确无跨用例/系统进程误报
    // （pgrep -f 在 Unix 可作辅助，tasklist /FI "IMAGENAME eq busybox64u.exe" 在 Windows
    // 下所有 busybox 子命令同名，不可用于区分，不采用）

func wsConnect(t, addr) *WSSession              // websocket 模式专用（复用同一套 callTool 抽象）
```

## 3. 场景矩阵（覆盖全部工具命令）

### 3.1 生命周期与协议

| # | 场景 | 步骤 | 断言 |
|---|------|------|------|
| L1 | initialize 握手 | 启动 → initialize（不设 include） | 返回 serverInfo（gTSP / 版本）+ 全量工具清单，与 schema 输出一致 |
| L2 | capabilities 过滤 | initialize 设 `include: ["read_file"]` | 工具清单仅含 read_file；调用未注册工具报错 |
| L3 | schema 子命令 | `gtsp schema` / `schema -o file`（沿用现有 CLI 测试） | 合法 JSON 工具定义数组 |
| L4 | 未知工具 | callTool("nonexistent") | 返回结构化错误，进程不退出 |
| L5 | 非法输入 | 畸形 JSON / 缺 required 字段 / 非法参数类型 | 结构化错误响应，会话继续可用 |
| L6 | shutdown RPC | 发 shutdown | gtsp 优雅退出；已注册后台进程被清理（结合 P 场景） |
| L7 | stdin EOF | closeStdin → waitExit | gtsp 退出；后台进程被 KillAll（out-of-band 验证） |
| L8 | 信号终止 | killSignal(SIGTERM) | gtsp 退出；后台进程被 KillAll |
| L9 | panic 恢复 | **不在 E2E 范围**：panic 注入需进程内注册 mock tool（`test/integration/panic_test.go` 现有做法），黑盒二进制无触发入口；保留在 test/integration/ | — |

### 3.2 文件工具（工作目录内往复）

| # | 场景 | 步骤 | 断言 |
|---|------|------|------|
| F1 | write→read 往复 | write_file 写多行文本 → read_file 读回 | 内容逐字节一致；行号/偏移语义正确 |
| F2 | edit | 写入 → edit（old→new 替换）→ read_file | 仅目标片段变更；old 不存在时结构化报错 |
| F3 | read_file 边界 | 读不存在文件 / 行区间越界 | 结构化错误，进程存活 |
| F4 | list_dir | 预置嵌套目录 + 垃圾文件（.git、__pycache__）| 默认忽略规则生效；recursive/limit/ignore_patterns 正确 |
| F5 | glob | 多模式匹配 | 命中集合与排序符合工具描述 |
| F6 | grep_search | 正则 / 固定串 / 上下文行 | 命中与上下文正确；二进制文件跳过 |
| F7 | read_image | （迁移现有 `read_image_test.go`）写 PNG → read_image | 尺寸/格式解析正确 |

### 3.3 execute_bash 与进程生命周期（与 `process_group_cleanup.md` 验收对齐）

| # | 场景 | 步骤 | 断言 |
|---|------|------|------|
| P1 | 同步命令 | `echo hello`；`exit 3`；stderr 输出 | stdout/stderr/exit_code 正确；success 与 exit_code 一致 |
| P2 | 输出截断 | 输出 >50KB / >1000 行 | `truncated: true`，截断标记文案正确 |
| P3 | 超时 kill | `sleep 999` + `task_timeout:1, timeout_action:"kill"` | 超时杀整组；返回部分输出；`osSurvivors`=0 |
| P4 | 超时转后台 | 同上 `timeout_action:"background"` | 返回 process_id；进程继续跑；后续 `process_stop` 清理 |
| P5 | run_in_background | 启动 `sleep 2` 后台 → `process_output`（block）轮询（callTool 显式 timeout 10s，慢 runner 防抖） | 立即返回 process_id；阻塞至完成；exit_code 正确 |
| P6 | process_list | 并存 1 个后台进程 | 列表含该条目与运行时长 |
| P7 | **事故复现：遗留进程移交** | 写 `load230.sh`（4 个忙等子进程，POSIX sh）→ `bash load230.sh 2>&1 \| head -1` | 返回 `process_id` + `leftover_processes` 树（4 存活、孤儿孙挂根下）；**out-of-band：`osSurvivors`=4** |
| P8 | 移交处置 | 接 P7 → `process_stop(process_id)` → `process_list` | stop 成功；`osSurvivors`=0；列表条目消失 |
| P9 | 有意后台服务 | 同步模式 `sleep 30 >/dev/null 2>&1 &`（无 run_in_background；不用 python http.server——runner 可能无 python 且固定端口会撞） | 不被杀；`process_output` 可读；最后 `process_stop` 清理（验证「不误杀」语义） |
| P10 | process_stop 幂等 | 对已 stop 的 id 再 stop | 成功返回，不报错 |

### 3.4 沙箱与权限

| # | 场景 | 步骤 | 断言 |
|---|------|------|------|
| S1 | 路径白名单 | 以 `--sandbox --allow-write=<工作目录>` 启动 gtsp → 工作目录内 write_file 正常；越界路径 write_file / sandbox RPC 调整后复测 | 结构化拒绝；目录内正常（注意：路径规则由启动参数或 sandbox RPC 下发，initialize 的 capabilities 只含 tools include/exclude） |
| S2 | 网络限制 | **待实现能力，本期移除**：permissions.go 的 `CheckNetwork()` 已定义但全仓无调用方（execute_bash 未接入），且无 OS 级网络隔离；待 execute_bash 接入网络闸门后补用例 | — |
| S3 | 权限降级 | （对应 api/permissions.go 能力）调用受限工具 | 结构化拒绝 |

### 3.5 多会话与日志

| # | 场景 | 步骤 | 断言 |
|---|------|------|------|
| M1 | 双会话隔离 | websocket 模式下两个连接并行对话（stdio 模式单进程仅一个会话） | 工具调用互不串扰；**断连只 KillSession 杀本会话进程，另一连接的进程不受影响**（与现有 KillSession 行为一致；注意：process_list/output/stop 当前不按 SessionID 过滤，A 可见/可停 B 的进程，这是现状语义，不做反向断言） |
| M2 | 会话日志 | 两次会话各产生日志 | `gt-agent-*.log` 按 session 命名、内容不串（吸收 `test_session_logging.py` 目标） |
| M3 | websocket 模式 | wsConnect 走一轮 F1 + P1 | 与 stdio 行为一致 |

## 4. 平台差异处理

| 差异点 | 处理 |
|---|---|
| shell：bash vs busybox ash | 场景脚本统一 POSIX sh 子集（`while :; do :; done`、`seq`、`&`），单份脚本三平台通用；shell 相关断言经由 `pal.ShellDescription()` 生成的描述做二次校验（防止描述与实现脱节，沿用 desc_check 思路） |
| 进程组 vs Job Object | P3/P7/P8 的 out-of-band 断言按平台选择探测命令；语义（「组内无存活」）保持一致 |
| 路径分隔符 | fixture 路径统一 `filepath.Join`；P 场景的 `| head` 管道语法 POSIX 通用 |
| busybox 可用性 | Windows 上 P 场景依赖 busybox 分发（v0.4.19+），缺失时 skip 并输出明确原因 |

## 5. 实施拆分

1. **第一批**：harness + L1/L2/L7 + F1/F2 + P1/P5（骨架跑通，覆盖最高频路径）
2. **第二批**：P3–P10（进程生命周期全量，含事故复现——前置依赖已完成：`process_group_cleanup.md` 第 2 批改造已落地 dev 分支 commit 9c208d0，可直接实施）
3. **第三批**：L3–L8、F3–F7、S、M 全量补齐
4. **收尾**：`read_image_test.go` 迁入 harness（删重复骨架）；`script/test_script/` Python 脚本标记废弃（其目标已被 M2 覆盖）

## 6. 临时目录与文件的清理逻辑

### 6.1 测试期间产生的临时产物

| 产物 | 位置 | 生命周期 |
|---|---|---|
| 工作目录（fixture、脚本、工具读写） | `t.TempDir()` 子目录（macOS `/var/folders/.../T/TestXxx…/001/`，Linux `/tmp/TestXxx…/001/`） | 测试函数结束即删 |
| gtsp 二进制 | **包级临时目录**（`TestMain` 创建，整包结束删除）——各用例共用一份，避免重复编译 | 整包结束即删 |
| gtsp 会话日志 | 每用例 `--log-path <t.TempDir()>/logs` 显式指定（默认 `<executable_dir>/logs` 指向包级目录，必须覆盖） | 随各用例目录一起删 |

仓库本身与用户目录（`~`）全程只读或不涉及；CI 上临时目录由 runner 的 `TMPDIR` 决定。

### 6.2 清理时序（设计约定）

每个用例按以下顺序收尾（由 harness 的 Cleanup 统一编排，先于 Go 框架对 `t.TempDir()` 的删除）：

1. `closeStdin`（或对已退出进程跳过）→ `waitExit`：让 gtsp 走正常 EOF 停机路径，顺带触发其内部 KillAll；
2. 超时未退出则 harness 强杀 gtsp 进程组——harness 持有的进程，允许测试基础设施杀；
3. 断言 `osSurvivors` 为空：若不为空，属于**被测行为缺陷**（正常失败路径），不阻塞目录删除；
4. gtsp 进程确认退出后，`t.TempDir()` 的自动删除才执行，避免 Windows 上因活进程占用目录导致 `RemoveAll` 失败。

### 6.3 残留场景与影响

| 场景 | 是否残留 | 说明 |
|---|---|---|
| 用例通过 / `t.Fatal` / 测试内 panic | 否 | testing 框架在任何退出路径上都执行 Cleanup |
| 测试进程被硬杀（CI 超时强杀、runner OOM） | 是 | Cleanup 无机会运行；目录留在 `/tmp` 或 `/var/folders`，macOS 由系统 3 天周期清理兜底，Linux/CI 量级小无实际影响 |
| Windows 上子进程仍占着目录 | 是 | Windows 无法删除被占用的文件/目录，testing 框架报 warning 后保留——**残留本身即是「进程未清理干净」的失败证据**；harness 的 6.2 时序（先杀 gtsp 再删目录）用于把这种情况压到只在真实缺陷时出现 |

### 6.4 本文档不改代码

以上清理时序是对 harness 实现的约定（当前 `read_image_test.go` 未含全部兜底步骤），落地时按 §5 实施批次随 harness 一并实现。

## 7. 局限

- 硬杀 gtsp（kill -9）无法在测试进程内安全演练（收尸逻辑在测试进程里），保持人工验证 + 方案文档 §3.6 防线覆盖。
- e2e 断言 out-of-band 依赖平台探测原语（Unix `kill(pid,0)`、Windows OpenProcess），不依赖外部命令输出格式。Windows 实现注意：`OpenProcess` 只要进程内核对象尚未释放就会成功（进程已退出但仍有句柄未关时亦然），不能仅凭其成功判活——须再查 `WaitForSingleObject(h, 0) == WAIT_TIMEOUT`（或 `GetExitCodeProcess == STILL_ACTIVE`），最后 `CloseHandle`。此外 **pid 不是唯一身份**：刚死的后代 pid 可能被无关新进程复用，探测须核对进程实例身份（Linux `/proc/<pid>/stat` 的 starttime、Windows `GetProcessTimes` 的 CreationTime，均在首次探测时记录、后续对比；快照表随每个测试的 gtsp 会话启动重置），实例变化一律视为原进程已死。CI 实测另确认：终止信号是异步生效的，杀后立即单次探测会撞上数十毫秒的死亡窗口，断言处统一用 `waitForNoSurvivors`（≤2s 轮询）吸收。
- 时间类断言（超时、轮询）保留余量（如 P3 的 task_timeout=1 实际允许 1–3s 完成），防慢 runner 抖动。
