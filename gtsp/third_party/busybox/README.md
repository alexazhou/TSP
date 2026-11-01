# Third-party: busybox-w32

`busybox64u.exe` 是 [busybox-w32](https://frippery.org/busybox/)（WIN32 原生移植版
BusyBox）的 64 位 + Unicode 构建，由 gTSP 随 **Windows 版**发布一起分发。

## 为什么需要它

Windows 没有原生 `sh`/`bash`。在引入本文件之前，`execute_bash` 硬编码
`exec.Command("bash", ...)`，Windows 上会出现两类问题：

1. **无 bash** → `exec: "bash": executable file not found`，工具直接不可用；
2. **命中 WSL 的 `C:\Windows\System32\bash.exe`** → 更糟：命令被交给 Linux
   虚拟机执行，进程不在 Windows 内核里、文件系统与 Windows 分离，
   于是 `execute_bash` 与 `read_file`/`write_file` 看到两个不同的根目录
   （静默跑错地方），且 `taskkill` 完全无法清理其中残留的进程。

busybox-w32 是**原生 Windows PE 程序**，命令与 gTSP 在同一内核、同一文件系统
（同一个 `C:\`），并自带一百多个 Unix applet，一次解决「没有 shell」和
「没有命令集」两件事。

## 许可与分发方式（重要）

busybox 是 **GPLv2**，gTSP 是 **MIT**。为避免「把 GPL 代码编译进单一可执行文件
= 衍生作品」的争议，本项目采用 **mere aggregation（聚合分发）**：

- `busybox64u.exe` **不嵌入** `gtsp.exe`，是**独立的可执行文件**
- 二者通过 `fork + exec + 管道` 通信（FSF GPL FAQ 认可这是聚合而非合并）
- Release 中同时提供 `busybox64u.exe` 与其许可证全文 `COPYING.busybox`
- 用户解压后把 `busybox64u.exe` 放在 `gtsp-windows-amd64.exe` **同一目录**

因此 **gTSP 自身仍然是 MIT**，GPLv2 只约束 `busybox64u.exe` 本身。

## 文件指纹

```
busybox64u.exe
  size    683,520 bytes
  sha256  36b266923fc5765b42f5f981319f39568498fba60b53ccf26e8fc1e575e2a9a9
  来源    https://frippery.org/files/busybox/busybox64u.exe
  构建日  2026-10-27
```

指纹由 `test/integration/busybox_windows_test.go` 的 `TestBusyboxBinaryUsable`
在 Windows CI 上断言。**替换该文件必须同步更新测试里的常量**，否则「CI 绿」
可能对应一个被悄悄换过的二进制。

## 变体选择

| 文件 | 体积 | 适用 |
| :--- | ---: | :--- |
| `busybox64u.exe` | 683,520 | **本项目选用**：64 位 + Unicode，Win10 1903+ / Win11 |
| `busybox64.exe` | 707,072 | 64 位，无 Unicode 支持 |
| `busybox64a.exe` | 655,872 | **ARM64**（Win on ARM），x64 上靠模拟运行会慢且不完整 |
| `busybox.exe` | — | 32 位，可在 64 位系统运行 |

`src/pal/shell_windows.go` 按 `busybox64u.exe` → `busybox64.exe` → `busybox.exe`
顺序查找，覆盖 x64 与 32 位回退；ARM64 版 Windows 需替换为 `busybox64a.exe`。

## 更新步骤

1. 从 <https://frippery.org/files/busybox/> 下载新版本，用同一目录下的
   `SHA256SUM` 校验
2. 替换本目录的 `busybox64u.exe`
3. 更新本 README 与 `test/integration/busybox_windows_test.go` 中的 sha256 / size
4. 跑 Windows CI，确认 `TestBusyboxBinaryUsable` 与
   `TestWindowsEOFKillsBackgroundTree` 均通过
