# 工具规格：read_image (图片读取)

## 1. 功能目标
让 Agent 具备**读取图片**的能力：输入图片文件路径，输出图片内容（base64）与元数据（`mime_type` / `format` / `width` / `height` / `size_bytes`），host 侧可将其作为多模态消息发送给支持视觉的模型。这是 V25（Agent 多模态请求）的基础能力之一。

## 2. 核心功能效果
*   **图片内容返回**：将图片原始字节以 base64 编码返回，可直接拼装为 `data:image/<mime>;base64,<data>` data URL。
*   **Magic Bytes 识别**：通过文件头识别 png / jpeg / gif / webp / bmp，**不信任扩展名**。JPEG 文件即使后缀是 `.jpg`，`format` 也统一返回 `jpeg`。
*   **元数据**：`mime_type`（`image/<subtype>`）与 `format` 一一对应；`width`/`height` 对 png/jpeg/gif 用标准库 `image.DecodeConfig` 解码获取，webp/bmp 在 v1 返回 0。
*   **Max-size 保护**：`--max-image-size` flag（默认 5MB），阈值按**原始文件大小**计算，**先 `stat` 后读取**，超限拒绝并提示压缩后再读。

## 3. 参数定义 (JSON)
*   **`file_path`** (string): 图片文件路径。经过沙箱 `ValidatePath` + `CheckRead` 校验。

## 4. 返回结果 (JSON)
*   **`file_path`** (string): 沙箱解析后的**绝对路径**（与 `read_file` 回显输入路径不同）。
*   **`mime_type`** (string): 如 `image/png`。
*   **`format`** (string): MIME subtype（`png`/`jpeg`/`gif`/`webp`/`bmp`，不出现 `jpg`）。
*   **`width`** / **`height`** (integer): 像素宽高；webp/bmp 为 0。
*   **`size_bytes`** (integer): 原始文件字节数。
*   **`base64`** (string): 图片原始内容 base64 编码。

## 5. 边界与约束
*   仅支持 png / jpeg / gif / webp / bmp；svg 不在 v1 范围（纯文本可用 `read_file`）。
*   v1 不引入 `golang.org/x/image`，bmp/webp 宽高返回 0。
*   不附带缩略图 / 降采样；不额外添加结果标记字段（host 按 `mime_type` + `base64` 形状识别）。
*   只读不写，不引入图片处理/生成能力。
*   超长日志截断：dispatcher 对请求/响应日志行超过 4096 字符进行截断，防止 base64 撑爆日志文件。

## 6. 错误处理
| 条件 | 错误信息 |
|---|---|
| 文件不存在 | `file not found: <path>` |
| 路径是目录 | `path is a directory: <path>` |
| 不支持格式 | `unsupported image format: <path> (supported: png, jpeg, gif, webp, bmp)` |
| 文件超限 | `image file is too large (<N> bytes, max <M> bytes). Please compress the image and try again` |
| 越界访问 | `security error: path "..." is outside of workspace root "..."` |

## 7. host 侧接入约定（V25）
TSP 侧只返回 base64 + 元数据，host 侧负责：
1. 按结果形状识别图片：结果为 dict 且含 `mime_type` + `base64` 即视为图片。
2. TOOL 消息剥离 base64，只保留元数据，避免 base64 进 LLM 上下文。
3. USER 图片消息：`attachments=[MessageAttachment(kind="image", mime_type=..., data=<base64>)]`，创建时内联。
4. 发送时拼成 `data:image/...;base64,` data URL 随 USER 消息发送。
