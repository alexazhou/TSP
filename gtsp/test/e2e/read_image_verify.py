"""TSP read_image 端到端验证脚本（自包含）：顺序调用 initialize → tool。

不依赖任何仓库外的资源：
- gtsp 二进制从本仓库源码构建（或通过 GTSP_BIN 环境变量指定预构建路径）
- 工作区为临时目录
- 测试图片在脚本内用标准库生成

用法:
    python3 gtsp/test/e2e/read_image_verify.py
"""
import base64
import json
import os
import shutil
import struct
import subprocess
import sys
import tempfile
import zlib

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))


def build_gtsp():
    """构建 gtsp 二进制并返回路径。优先使用 GTSP_BIN 环境变量。"""
    env_bin = os.environ.get("GTSP_BIN")
    if env_bin:
        if not os.path.exists(env_bin):
            raise SystemExit(f"GTSP_BIN 指定的二进制不存在: {env_bin}")
        return env_bin
    gtsp_dir = os.path.join(REPO_ROOT, "gtsp")
    out = os.path.join(tempfile.gettempdir(), f"gtsp-e2e-{os.getpid()}")
    print(f"building gtsp -> {out}")
    subprocess.run(["go", "build", "-o", out, "./src"], cwd=gtsp_dir, check=True)
    return out


def make_png(width, height):
    """用标准库生成一张纯色 PNG（8-bit RGB）。"""
    def chunk(tag, data):
        body = tag + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)

    ihdr = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)  # depth 8, color type 2 (RGB)
    row = b"\x00" + b"\x00\x00\xff" * width  # 行首 filter byte + 蓝色像素
    png = b"\x89PNG\r\n\x1a\n"
    png += chunk(b"IHDR", ihdr)
    png += chunk(b"IDAT", zlib.compress(row * height))
    png += chunk(b"IEND", b"")
    return png


def main():
    gtsp = build_gtsp()
    workdir = tempfile.mkdtemp(prefix="gtsp-e2e-")
    try:
        png_bytes = make_png(16, 12)
        with open(os.path.join(workdir, "test_image.png"), "wb") as f:
            f.write(png_bytes)

        proc = subprocess.Popen(
            [gtsp],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            cwd=workdir,
        )

        # 1. initialize
        init = {
            "id": "1",
            "method": "initialize",
            "input": {"protocolVersion": "0.3", "capabilities": {"tools": {"include": ["read_image"]}}},
        }
        proc.stdin.write(json.dumps(init) + "\n")
        proc.stdin.flush()
        data = json.loads(proc.stdout.readline())
        tools = data.get("result", {}).get("capabilities", {}).get("tools", [])
        names = [t.get("name") for t in tools]
        print("initialize tools:", names)
        if "read_image" not in names:
            print("ERROR: read_image not in tool list")
            return 1

        # 2. read_image 调用
        call = {"id": "2", "method": "tool", "tool": "read_image", "input": {"file_path": "test_image.png"}}
        proc.stdin.write(json.dumps(call) + "\n")
        proc.stdin.flush()
        data = json.loads(proc.stdout.readline())

        proc.terminate()
        proc.wait(timeout=5)

        if data.get("type") == "error":
            print("read_image ERROR:", json.dumps(data, ensure_ascii=False))
            return 1

        result = data.get("result", {})
        ok = True
        checks = []

        def check(name, cond):
            nonlocal ok
            ok = ok and bool(cond)
            checks.append(f"{'PASS' if cond else 'FAIL'}  {name}")

        check("mime_type == image/png", result.get("mime_type") == "image/png")
        check("format == png", result.get("format") == "png")
        check("width == 16", result.get("width") == 16)
        check("height == 12", result.get("height") == 12)
        check("size_bytes == fixture", result.get("size_bytes") == len(png_bytes))
        check("base64 roundtrip", base64.b64decode(result.get("base64", "")) == png_bytes)
        check("file_path absolute", result.get("file_path", "").startswith("/"))

        meta = {k: result.get(k) for k in ["file_path", "mime_type", "format", "width", "height", "size_bytes"]}
        print("read_image result:", json.dumps(meta, ensure_ascii=False))
        print("base64 length:", len(result.get("base64", "")))
        for c in checks:
            print(c)
        print("VERIFY:", "PASS" if ok else "FAIL")
        return 0 if ok else 1
    finally:
        shutil.rmtree(workdir, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
