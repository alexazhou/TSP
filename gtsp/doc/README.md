# gtsp 文档

三部分：`design/` 是**系统设计**（系统该怎么工作），`tool_spec/` 是**工具规格**（每个工具对外的输入/行为/输出约定），`need_fix/` 是**待修问题**（现状哪里不对、要怎么改）。

## design/ — 系统设计

| 文档 | 内容 |
|---|---|
| [`process-output-optimization.md`](./design/process-output-optimization.md) | 工具输出截断与 `process_output` 增量读取方案（Phase 1 已实现：512 KB ring + Head+Tail + 增量游标） |
| [`child-process-reclamation.md`](./design/child-process-reclamation.md) | 子进程回收机制（进程组、handover、残留上报、reaper） |
| [`windows-busybox-integration.md`](./design/windows-busybox-integration.md) | Windows 平台集成 busybox 与子进程自动退出（v0.4.19 已发布；Job Object 未实施、作为后备方案保留） |
| [`e2e-testing-design.md`](./design/e2e-testing-design.md) | 端到端测试设计（含临时目录/文件清理约定） |

## tool_spec/ — 工具规格

`execute_bash` / `read_file` / `write_file` / `edit` / `glob` / `grep_search` / `list_dir` / `read_image`，共 8 份。

> **权威来源**：对外契约以 [`spec/`](../../spec/) 下的协议与工具规范为准；`tool_spec/` 是实现侧的规格说明，更详细、并记录内部设计取舍。

## need_fix/ — 待修问题

| 文档 | 作者 / 日期 | 内容 |
|---|---|---|
| [`tool-description-review.md`](./need_fix/tool-description-review.md) | 李哥（测试工程师）／2026-11-15 | 以 Agent 视角评审**运行时实际注入给 Agent 的 tool description 字符串**（`execute_bash` / `process_list` / `process_output` / `process_stop` 四个工具）：哪些行为「规格里有、注入给 Agent 的描述里没有」，属对外契约披露缺失。方法只以「描述文本 + 实机返回体」为证据，不参考实现代码 |
| [`encoding-and-binary-output.md`](./need_fix/encoding-and-binary-output.md) | gtsp dev／2026-11-15（源自 Operator 提问） | 命令输出**不保证是 UTF-8**（locale / Windows 代码页 / 二进制），而 JSON 只能承载文本 ⇒ 非 UTF-8 字节变 U+FFFD。含实测、已修缺陷（流起点对齐，`22231ff`）与残留、编码自动识别/转码的取舍（L0/L1/L2）与 4 项待决策 |

> **状态（2026-11-15 复核）**：`tool-description-review.md` 原先「未写所评审版本」的问题**已补**——李哥加了「版本锚点与生效边界」节（基线 gtsp v0.5.0 / `0a4e39d8`）；gtsp 开发随后加了「逐条复核」节（基线 `dad259f`），结论是 6 条里 **2 条已修**（#3 部分、#5 完全）、**4 条仍成立**（#1/#2/#4/#6）。

## 历史记录（未归类）

以下两份为早期测试报告，均为历史快照（日期 2024-01 / 2026-04），不含待办项，暂留在 `doc/` 根：

- `test_report_session_logging.md`
- `test_results_session_logging.md`

## 相关

- 协议与工具规范：[`spec/`](../../spec/)
- 团队侧的分析报告（不进仓库）：`shared/gtsp-output-opt/`
