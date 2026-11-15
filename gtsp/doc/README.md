# gtsp 文档

两份：`design/` 是**设计与规格**（系统该怎么工作），`need_fix/` 是**待修问题**（现状哪里不对、要怎么改）。

## design/ — 设计与规格

| 文档 | 内容 |
|---|---|
| [`process-output-optimization.md`](./design/process-output-optimization.md) | 工具输出截断与 `process_output` 增量读取方案（Phase 1 已实现：512 KB ring + Head+Tail + 增量游标） |
| [`child-process-reclamation.md`](./design/child-process-reclamation.md) | 子进程回收机制（进程组、handover、残留上报、reaper） |
| [`windows-busybox-integration.md`](./design/windows-busybox-integration.md) | Windows 平台集成 busybox 与子进程自动退出（v0.4.19 已发布；Job Object 未实施、作为后备方案保留） |
| [`e2e-testing-design.md`](./design/e2e-testing-design.md) | 端到端测试设计（含临时目录/文件清理约定） |
| [`tool_spec/`](./design/tool_spec/) | 各工具规格：`execute_bash` / `read_file` / `write_file` / `edit` / `glob` / `grep_search` / `list_dir` / `read_image` |

> **注意**：对外契约的权威来源是 [`spec/`](../../spec/) 下的协议与工具规范；`design/tool_spec/` 是实现侧的规格说明（更详细、含内部设计取舍）。

## need_fix/ — 待修问题

| 文档 | 内容 |
|---|---|
| [`tool-description-review.md`](./need_fix/tool-description-review.md) | 以 Agent 视角评审运行时注入的 tool description：哪些行为「规格里有、注入给 Agent 的描述里没有」，属对外契约披露缺失 |

## 历史记录（未归类）

以下两份为早期测试报告，均为历史快照（日期 2024-01 / 2026-04），不含待办项，暂留在 `doc/` 根：

- `test_report_session_logging.md`
- `test_results_session_logging.md`

## 相关

- 协议与工具规范：[`spec/`](../../spec/)
- 团队侧的分析报告（不进仓库）：`shared/gtsp-output-opt/`
