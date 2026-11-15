# 命令输出不保证是 UTF-8（待决策）

> **状态**：问题记录 + 待决策项（未实施）
> **来源**：Operator 2026-11-15 提问引出；缺陷部分已修（见 §3）
> **撰写日期**：2026-11-15

---

## 1. 问题

`execute_bash` / `process_output` 的 `stdout` / `stderr` 是**字节流**，**不保证是 UTF-8**；而 TSP 的结果字段是 JSON 字符串，**只能承载文本**。两者之间必然有损，且损失发生在协议层（JSON 序列化），不在 buffer 层。

---

## 2. 事实

### 2.1 stdout 没有编码属性（实测，macOS）

stdout 是 fd 1，内核只搬字节。编码由**程序 + locale 环境**决定，与 bash 无关（shell 不转码）。

同一台机器、同一个程序（`python3 -c 'print("中")'`），只改 locale：

| `LC_ALL` | `sys.stdout.encoding` | 实际输出字节 |
|---|---|---|
| `en_US.UTF-8` | `utf-8` | `e4 b8 ad`（UTF-8 的「中」） |
| `LC_ALL=C` | `utf-8`（Python 3.7+ PEP 538 强转 C.UTF-8） | `e4 b8 ad` |
| `zh_CN.GBK` | `gbk` | **`d6 d0`（GBK 的「中」，不是 UTF-8）** |
| `en_US.ISO8859-1` | `iso8859-1` | 对应单字节编码 |

`LC_ALL=C` 下并非所有工具都像 Python 那样强转 UTF-8——Perl / 部分 GNU 工具会退化成 ASCII，非 ASCII 变 `?` 或转义序列。

第三类更彻底：`cat <二进制文件>`、`gzip`、`base64 -d` 的输出**在任何 locale 下都不是文本**。

**平台差异（⚠️ Windows 部分未实测）**：
- Linux/macOS：locale（`LANG` / `LC_ALL` / `LC_CTYPE`）
- Windows：**代码页**。简体中文默认 OEM CP936（GBK）；输出重定向到管道时很多原生程序按 ANSI/OEM 代码页吐字节。`chcp 65001` 只对尊重代码页的程序生效；PowerShell 5.1 的 `>` 默认 UTF-16LE，PS 7 默认 UTF-8。现代运行时（Go / Rust / Node / .NET Core）多数默认 UTF-8。

**我们环境的实测事实**：gtsp 拉起的 shell 里 `LANG` / `LC_*` **全部未设置**（等价 C locale）——locale 敏感工具的行为与用户在终端里手动跑可能不一致。

### 2.2 JSON 层把非 UTF-8 字节变成 U+FFFD（实测）

`encoding/json` 序列化时把**每个**非法 UTF-8 字节替换成 U+FFFD（`EF BF BD`）：**1 B → 3 B**，长度膨胀且不可逆。

```
命令 printf 'a\270\255b\n'    → JSON "stdout":"a\xef\xbf\xbd\xef\xbf\xbdb\n"  （2 个坏字节 → 2 个 U+FFFD）
命令 printf 'a\344\270\255b\n' → JSON "stdout":"a\xe4\xb8\xadb\n"            （合法 UTF-8 原样通过）
```

⇒ **二进制输出的「字节精确回传」在协议层就不可能**，与 buffer 实现无关。

---

## 3. 相关缺陷：流起点曾被当成「切在字符中间」（已修 `22231ff`）

`ProcBuffer.alignCeilLocked` 原本从 `offset 0` 起就跳过 UTF-8 continuation byte（0x80–0xBF），把流开头当成「切在字符中间」——但流起点根本没有「前一个字符」可归属，首字节可以是任意字节。

`b4b4b49` 实测（`printf '\200\200…abcdef\n'`，前导 N 个 0x80）：

| 前导长度 | 旧输出 | `stdout_overflow` | 新（`22231ff`） |
|---|---|---|---|
| 2 | `... (2 bytes earlier output evicted by buffer) ...` | 未置位 | 无 marker，字节全保留 |
| 10 | `... (10 bytes …) ...` | 未置位 | 无 marker，字节全保留 |
| 100 | `... (100 bytes …) ...` | 未置位 | 无 marker，字节全保留 |

三个症状：① 字节**永久丢失**（起点跳过没有「上一页」能补回，与终点 trim 不同）；② 被误标成「buffer evicted」；③ `truncated = clipped || overflow` 不覆盖对齐跳过 ⇒ 聚合标志与 marker 文案矛盾。

**修复**：`alignCeilLocked` 在 `offset <= 0` 直接返回——流起点天然是字符边界。终点策略（`utf8Ceil` + `trimPartialRuneSuffix`）不动，因为终点裁剪**可恢复**（下一页能补读）。

**一个曾被写错的上界**：早期文档称「最多丢 1–3 B」，理由「UTF-8 一个字符最多 4 字节」。该推理只对「有 lead byte 的序列」成立；**流开头的 continuation run 没有 lead byte，长度任意**（实测 100 B 也能全丢）。

### 3.1 残留（pre-existing，未修）

1. **前导非法 run 超过一页时仍会丢**：prefix 第一页读完（约 50 KB）后 `next` 落在 run 中间，下一页 `alignCeil(offset > 0)` 把 run 剩余整段跳过。实测（`abc` + 10×`\270` + `defg`，小窗口）会丢 4–7 B；生产中页预算 50944 B，故需要 **> 50 KB 的连续 continuation 字节**才可达。**新旧代码读数逐条相同**（skip 7/6/7/6/4）⇒ 不是流起点修复引入的。
2. **非法字节在流中间时**：跨页跳过的是「该 run 在页边界之后的剩余部分」，实际通常 1–3 B（页 50944 B）。与「合法 UTF-8 分页必须 rune 对齐」互斥，只能文档化。
3. **非 UTF-8 文本（如 GBK）仍然是乱码**：buffer 不再丢字节，但 JSON 会把 GBK 双字节变成两个 U+FFFD，用户看到满屏 `�`。
4. **二进制输出语义上不适合走 stdout**：正确做法是写文件 + `read_file`，或 base64。

---

## 4. 待决策项

### D1｜是否引入编码自动识别 / 转码？

**检测本身可行但不可靠**：字节流无编码标识（除 BOM），只能靠启发式（编码合法性 + 字节频率统计，Mozilla universalchardet 系）：
- 短输出（几百字节、纯 ASCII 前缀）几乎测不准；GBK / GB18030 / Big5 / Shift-JIS 互有重叠；
- **误判代价不对称**：二进制误判成 GBK 再转码，会产出「看起来像文本」的垃圾，**比 U+FFFD 更难发现**；
- Go 标准库只有转码（`golang.org/x/text/encoding`），**没有检测**；检测需第三方库（如 `saintfish/chardet`、`gogs/chardet`）。⚠️ 准确率/性能/体积影响**未评估**。

**真正的坎是分页契约**：`stdout_offset` / `EvictedBytes` / 512 KB ring 容量全以**原始字节**为单位，转码会改变字节数（GBK「中」2 B → UTF-8 3 B，+50%）：

| 方案 | 后果 |
|---|---|
| buffer 存原始字节，输出时转码 | 每页转码后长度不定，**跨页可能切断字符**；marker 里 `N bytes remaining` 指原始还是转码后？两套数并存 ⇒ 调用方必然算错游标 |
| buffer 存转码后 UTF-8 | 写者/读者字节数不一致；ring 的 512 KB 覆盖量随内容膨胀，`EvictedBytes` 失去物理含义 |

叠加流式特性：溢出后流的前半已丢，检测只能看残留部分；一条流前后编码不同（少见但存在）无法处理。

**可选分层**：

| 层 | 内容 | 契约风险 | 建议 |
|---|---|---|---|
| **L0** | 只识别、只报告、不转码：结果加诊断字段（如 `stdout_encoding_hint: "utf-8" \| "likely-non-utf8" \| "binary"`），`utf8.Valid` + 可打印比例即可 | **零**（offset 语义不动） | 推荐先做 |
| **L1** | 仅当 `!utf8.Valid` 时跑检测把结论放进 hint；另给**显式**参数 `encoding: "gbk"` 由调用方主动要求转码（游标仍指原始流） | 中（需写清两套语义） | 有真实场景再做 |
| **L2** | 默认自动转码成 UTF-8 | **高**（offset/容量/marker 三处契约 + 误判不可逆） | 不建议 |

**更划算的落点**：编码检测放 `read_file` 比放输出路径安全得多——它没有分页游标语义。

### D2｜Windows 行为实测
代码页、`chcp 65001`、PowerShell 重定向、busybox `ash` 在 Windows 的编码行为**全部未实测**，需有 Windows 环境的人验证。

### D3｜`read_file` 侧是否已有编码处理
未查证。若要做 L0/L1 必须先查这一处（D1 的「更划算落点」依赖它）。

### D4｜spec 措辞限定
[`spec/tools/execute_bash.md`](../../spec/tools/execute_bash.md) 有句「the stream head is never treated as mid-character … all leading bytes are returned」，严格说需要限定「前导非法 run 不超过一页」（见 §3.1 第 1 条）。纯措辞微调。

---

## 5. 使用告诫（已写入 spec）

- 需要文本稳定时，**在命令里自己钉编码**：Unix `LC_ALL=C.UTF-8`（+ Python `PYTHONIOENCODING=utf-8`）；Windows `chcp 65001 >nul &` 前缀，或选支持 `--encoding utf-8` 的工具。
- **二进制不要走 stdout**：写文件 + `read_file`，或 base64。
- 看到 `U+FFFD`（`�`）= 「这里有非 UTF-8 字节」，不是「输出被截断」。

---

## 6. 复现命令

```bash
# locale 影响同一程序的输出字节（macOS 实测）
LC_ALL=zh_CN.GBK   python3 -c 'print("中")' | od -An -tx1    # d6 d0
LC_ALL=en_US.UTF-8 python3 -c 'print("中")' | od -An -tx1    # e4 b8 ad

# JSON 层 U+FFFD（经 gtsp stdio JSON-RPC）
printf 'a\270\255b\n'    # → "stdout":"a\xef\xbf\xbd\xef\xbf\bdb\n"

# 前导 continuation run（旧版 b4b4b49 会丢并误报 evicted；22231ff 起保留）
printf '\200\200…\200abcdef\n'
```
