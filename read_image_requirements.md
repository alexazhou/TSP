# 需求：TSP 支持读取图片文件

## 1. 背景与目标

### 1.1 背景

TeamAgent（host）正在推进 **Agent 多模态请求** 能力（V25）：让 Agent 的工具结果图片可以回传给大模型识别，使 Agent 具备"看图"能力。

当前 TSP 内置工具**不支持读取图片**：

- `read_file` 有 `isBinary` 保护，图片直接被拒绝（`file appears to be binary and cannot be read as text`）。
- `grep_search` / `glob` 明确跳过 png/jpg 等媒体文件。
- TSP 协议的工具结果只支持 JSON（`ToolResult.output: Dict | List | str`），没有二进制通道。

> 注：`gtsp/doc/tool_spec/read_file.md` 的功能目标中已预留"支持读取文本、Markdown、JSON 以及**图片/PDF**（如果底层模型支持多模态）"的方向，本需求即该方向的落地。

### 1.2 目标

让 TSP 提供**读取图片文件**的能力：Agent 调用工具后，能拿到图片的内容（base64）与元数据，host 侧将其作为多模态消息发送给支持视觉的模型。

---

## 2. 需求描述

### 2.1 功能需求

- 提供一个新的图片读取能力，输入图片文件路径，输出：
  - **图片内容**：base64 编码（可直接用于多模态请求的 `data:image/...;base64,` 形式）。
  - **元数据**：`file_path` / `mime_type` / `format` / `width` / `height` / `size_bytes`，供模型与 host 了解图片概况。

### 2.2 使用场景

- Agent 执行命令 / 打开页面产生截图后，读取该截图让模型识别、判断结果。
- Agent 需要阅读工作区内已有的图片文件（如读取到的图片素材）并基于其内容继续行动。

### 2.3 与 V25（host 侧）的衔接

host 侧已确定的多模态发送约定：

- 工具结果图片 → **TOOL 纯文本结果** + **独立 USER 图片消息**（OpenAI 规范：`image_url` 仅允许在 `user` 角色）。
- `MessageAttachment` 支持 `data`（base64 内联）形态；host 在**创建消息时内联** base64，发送时直接拼 data URL。

因此 TSP 侧只需返回 base64 + 元数据，host 负责拆分与组装，无需额外协议扩展。

---

## 3. 现状分析

| 层面 | 现状 |
|------|------|
| **TSP 协议** | 工具结果仅支持 JSON，无二进制通道 |
| **`read_file`** | `isBinary` 拒绝二进制/图片；语义是文本读取 |
| **`grep_search` / `glob`** | 跳过媒体文件 |
| **格式识别** | 现有代码无图片 magic bytes / MIME 检测 |
| **host ↔ TSP** | 共享工作区文件系统；但按本方案 host **不读文件**，由 TSP 直接返回 base64 |

---

## 4. 方案设计

### 4.1 工具形态：新增 `read_image`（推荐）vs 扩展 `read_file`

| 方案 | 说明 | 优缺点 |
|------|------|--------|
| **新增 `read_image`** | 独立工具，入参 `file_path`，返回图片 base64 + 元数据 | 语义清晰、返回结构独立；`read_file` 的文本契约不被污染；实现与测试解耦 |
| **扩展 `read_file`** | `read_file` 检测到图片时返回 base64 + 元数据 | 与现有规格文档"图片/PDF"方向一致；但文本/图片两种返回结构混在一个工具里，契约变复杂，且文本调用方需处理图片返回 |

**推荐：新增 `read_image` 独立工具。** `read_file` 保持纯文本语义。

### 4.2 `read_image` 工具规格（草案）

```json
// 入参
{ "file_path": "screenshots/foo.png" }

// 成功返回
{
  "file_path": "/abs/path/screenshots/foo.png",
  "mime_type": "image/png",
  "format": "png",
  "width": 1920,
  "height": 1080,
  "size_bytes": 234567,
  "base64": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB..."
}
```

实现要点：

- **沙箱校验**：沿用 `api.ValidatePath` + `session.CheckRead`（与 `read_file` 一致）。`file_path` 返回 ValidatePath 解析后的**绝对路径**（注意：与 `read_file` 回显输入路径不同，spec 文档需注明）。
- **格式识别**：magic bytes（PNG `\x89PNG`、JPEG `\xff\xd8\xff`、GIF、WebP `RIFF..WEBP`、BMP `BM`），**不信任扩展名**；全不匹配时报错 `unsupported image format: <path> (supported: png, jpeg, gif, webp, bmp)`。
- **宽高**：Go 标准库 `image.DecodeConfig`（支持 png/jpeg/gif）。**bmp/webp 的 `width`/`height` 返回 0**（v1 不引入 `golang.org/x/image`，后续可补充）。`format` 统一为 mime subtype（png/jpeg/gif/webp/bmp，不出现 `jpg`）；动画 gif/webp 取逻辑屏幕/首帧尺寸。
- **Max-size 保护**：base64 走 JSON，超大图会拖垮协议与 host 内存。阈值按**原始文件大小**计算，**先 `stat` 后读取**，超过即拒绝并提示压缩后再读。
- **注册**：`handlers.go` 的 `RegisterAll` + `main.go` `handleSchemaCommand` 的 `order` 列表，两处。

> **关于 `format` 字段的命名**：`format` 取 MIME subtype 而非文件后缀。原因：`image/jpeg` 的 subtype 只有 `jpeg` 一个规范值，而文件后缀存在 `.jpg` / `.jpeg` 两种写法。`read_image` 按 magic bytes 识别格式（不信任扩展名），若 `format` 以后缀为准，同一个 JPEG 文件返回值会不稳定。统一返回 subtype（png/jpeg/gif/webp/bmp）后，`format` 与 `mime_type`（`image/<subtype>`）一一对应、自洽。

### 4.3 host 侧接入约定

`_run_tool_to_item` 收到 `read_image` 结果后：

> TSP 侧**不额外添加** `kind` 之类的标记字段，识别完全依赖结果形状，host 侧按 §2.3 约定实现。

1. **按结果形状识别图片**：结果为 dict 且含 `mime_type` + `base64` 即视为图片。
2. **TOOL 消息剥离 base64**：`content` 只保留元数据（不含 `base64`），避免 base64 进 LLM 上下文。
3. **USER 图片消息**：`AgentMessage(role=USER, content=文字描述, attachments=[MessageAttachment(kind="image", mime_type=..., data=<base64>)])`，创建时内联。
4. 发送时 `to_openai_message()` 把 `data` 拼成 `data:image/...;base64,` data URL，随 USER 消息发给模型。

---

## 5. 边界与约束

- **图片大小上限**：`--max-image-size` flag（默认 5MB，`0` 表示不限），阈值按原始文件大小计算，超过即拒绝（保护协议负载与内存）。先 `stat` 再读取，拒绝时不读文件。
- **格式范围**：首发支持 png / jpeg / gif / webp / bmp；不支持的格式明确报错。**svg 不在 v1 范围**（纯文本，`read_file` 即可读取）。
- **宽高限制**：bmp / webp 的 `width`/`height` 返回 0；动画 gif/webp 取逻辑屏幕/首帧尺寸。
- **日志脱敏**：dispatcher 会把完整响应 JSON 写入日志（`dispatcher.go` 的 `← %s`），base64 会撑爆日志文件——需在 §6 改动范围中加入对超长日志行的截断。
- **只读**：`read_image` 只读不写，不引入图片处理/生成能力。
- **多模态响应**：本需求只解决"读取图片供模型识别"，不涉及模型回传图片的存储与展示。

---

## 6. 涉及改动范围（预估）

### TSP 仓库

- `gtsp/src/tools/read_image.go`（新增，实现 + schema + magic bytes 检测）
- `gtsp/src/tools/handlers.go`（`RegisterAll` 注册）
- `gtsp/src/main.go`（`handleSchemaCommand` 的 `order` 列表补 `read_image`）
- `gtsp/src/api/dispatcher.go`（响应日志对超长行截断，避免 base64 写满日志）
- `spec/tools/read_image.md`（协议工具定义文档）
- `spec/tools/README.md`（工具索引登记）
- `gtsp/doc/tool_spec/read_image.md`（工具规格文档）
- 相关单测（含 png/jpeg/gif/webp/bmp 各格式 fixture 小图）

### TeamAgent 仓库（host 侧）

- `src/service/agentService/agentTurnRunner.py`：`_run_tool_to_item` 识别图片结果，拆分 TOOL 文本 + USER 图片消息
- V25 设计文档（step2 §6.1 工具图片小节 / step3 任务表）同步

---

## 7. 决策记录（2026-08-16）

- ✅ 采用**新增 `read_image`** 独立工具，`read_file` 保持纯文本语义。
- ✅ max-size 默认 **5MB**，做成 `--max-image-size` flag，阈值按原始文件大小计算。
- ✅ **v1 不引入 `golang.org/x/image`**，bmp/webp 宽高返回 0，后续可补充。
- ✅ **不附带缩略图 / 降采样**（维持本文"暂不纳入"）。
- ✅ **不额外添加结果标记字段**，host 侧按 `mime_type` + `base64` 形状识别（维持 §2.3 约定）。

> 以上为默认决策，按 §4.2 / §5 落地；如需调整（如首发就要 bmp/webp 宽高，或改默认阈值），直接改对应小节即可。
