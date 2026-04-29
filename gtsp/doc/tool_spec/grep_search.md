# 工具规格：grep_search (内容检索)

## 1. 功能目标
提供高性能的代码检索能力。通过 `grep_search` 快速在代码库中定位特定内容，减少代理通过 `read_file` 盲目读取文件内容的行为。支持“上下文预览”以快速建立对代码逻辑的理解，同时严格控制输出大小以保护上下文窗口。

## 2. 核心功能
*   **高性能驱动**：后端集成高效检索算法，确保在大型项目中实现毫秒级响应。
*   **搜索模式切换**：支持字面量字符串（Fixed Strings）和正则表达式（Regex）切换。
*   **智能上下文**：不仅返回匹配行，还支持返回前后 N 行（`context`/`before`/`after`），帮助代理在不打开文件的情况下理解逻辑。
*   **输出保护机制**：
    *   **全局最大匹配数**：防止搜索词过于泛滥（如搜索 `err`）导致返回数万行。
    *   **单文件最大匹配数**：防止在单个巨大的日志或生成的代码文件中产生过多干扰。

## 3. 参数定义 (JSON)
*   **`pattern`** (string, 必填): 搜索关键词或正则表达式。
*   **`dir_path`** (string, 可选): 指定搜索的子目录，默认为项目根目录。
*   **`include_pattern`** (string, 可选): Glob 过滤，仅搜索匹配的文件（如 `*.go`）。
*   **`exclude_pattern`** (string, 可选): 排除匹配的文件或路径。
*   **`fixed_strings`** (boolean, 可选): 为 `true` 时，将 `pattern` 视为字面量字符串；为 `false` 时视为正则。默认 `false`。
*   **`case_sensitive`** (boolean, 可选): 是否区分大小写，默认 `false`。
*   **`context`** / **`before`** / **`after`** (integer, 可选): 返回匹配行周围的上下文行数。
*   **`total_max_matches`** (integer, 可选): 全局返回的最大匹配数量（默认 100）。
*   **`max_matches_per_file`** (integer, 可选): 单个文件内返回的最大匹配数量（默认 10）。

## 4. 返回值格式
*   **`matches`** (array): 匹配项列表。
    *   **`file_path`** (string): 文件路径。
    *   **`line_number`** (integer): 行号。
    *   **`content`** (string): 匹配行内容。
    *   **`context`** (array of string, 可选): 上下文行内容。
*   **`truncated`** (boolean): 如果因为达到 `total_max_matches` 而导致结果被截断，则为 `true`。
