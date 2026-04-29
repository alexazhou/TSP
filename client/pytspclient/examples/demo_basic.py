#!/usr/bin/env python3
"""
gtsp 极简 Demo：用 10 行代码让 agent 获得完整的工具能力。

运行方式：
    python examples/demo_basic.py

前置条件：
    pip install pytspclient
    gtsp 二进制文件在 PATH 中，或设置 GTSP_PATH 环境变量
"""

import asyncio
import os
from pytspclient import TSPClient

GTSP_PATH = os.environ.get("GTSP_PATH", "gtsp")


async def main():
    client = await TSPClient.from_stdio(GTSP_PATH).start()

    print(f"可用工具: {[t['name'] for t in client.tools]}")

    # 调用工具
    await client.call_tool("write_file", {"file_path": "hello.txt", "content": "Hello from gtsp!"})
    print("✓ 写入 hello.txt")

    resp = await client.call_tool("read_file", {"file_path": "hello.txt"})
    print(f"✓ 读取: {resp.output[:50]}...")

    resp = await client.call_tool("execute_bash", {"command": "ls -la hello.txt"})
    print(f"✓ 执行: {resp.output.strip()}")

    await client.shutdown()


if __name__ == "__main__":
    asyncio.run(main())