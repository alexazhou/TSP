# 工具规格：read_file (文件读取)

## 1. 功能目标
安全、高效地读取本地文件内容。通过引入“切片读取”机制，解决代理无法处理超过上下文窗口限制的大文件的问题。

返回的 `content` 每行带**行号前缀**（`%4d│` 格式，如 `"   12│code"`），便于模型引用具体行号进行推理或构造 `edit` 调用。

## 2. 核心功能效果
*   **切片读取 (Line-based Slicing)**：支持通过 `start_line` 和 `end_line` 参数只读取文件的特定部分。这是阅读大型源码库或日志文件的唯一安全方式。
*   **自动保护机制**：
    *   **默认大小限制**：未指定范围（全量读取）时，文件超过 **25KB** 仅返回开头约 **1KB**，并置 `truncated: true` 附加提示；分页需用 `start_line`/`end_line` 或 `grep_search`。
    *   **二进制保护**：自动识别二进制文件，防止乱码数据充斥上下文。
*   **格式支持**：支持读取文本、Markdown、JSON 以及图片/PDF（如果底层模型支持多模态）。

## 3. 参数定义 (JSON)
*   **`file_path`** (string): 文件的绝对路径。
*   **`start_line`** (integer, 可选): 读取的起始行号（从 1 开始）。
*   **`end_line`** (integer, 可选): 读取的结束行号（闭区间）。
*   **`encoding`** (string, 可选): 文件编码格式，默认 `utf-8`。

## 4. 返回值 (JSON)

```json
{
  "file_path": "src/main.go",
  "content": "   1│package main\n   2│\n   3│import (\n   4│    \"fmt\"\n",
  "total_lines": 171,
  "start_line": 1,
  "end_line": 50
}
```

*   **`content`**：读取范围内每行都以 `%4d│` 前缀标注**文件实际行号**（如 `"   12│code"`），方便模型引用具体行。
*   **`total_lines`**：文件总行数。
*   **`start_line` / `end_line`**：本次实际返回的行区间（1-based）。
*   **`truncated`**：全量读取超过 25KB 时置 `true`（仅返回开头约 1KB，末尾附截断提示）；否则省略。

> **edit 提示**：行号前缀仅用于引用定位。调用 `edit` 时，`old_string` 使用**不带前缀**的纯净文本。
>
> **截断提示**：全量读取大文件后，`content` 末尾会追加一行说明（如 `... (file truncated: first 1024 of 51200 bytes shown; ...)`），模型应据此改用 `start_line`/`end_line` 或 `grep_search` 分页。
