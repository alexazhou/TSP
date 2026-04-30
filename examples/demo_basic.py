#!/usr/bin/env python3
"""
gtsp 极简 Demo：用 10 行代码让 agent 获得完整的工具能力。

运行方式：
    python examples/demo_basic.py

前置条件：
    pip install pytspclient
    gtsp 二进制文件放在当前目录，或修改 GTSP_PATH 变量指向实际路径
"""

import asyncio
from pytspclient import TSPClient, ToolCall

GTSP_PATH = "./gtsp"  # 替换为实际路径，如 "/path/to/gtsp"


async def main():
    client = await TSPClient.from_stdio(GTSP_PATH).start()

    print(f"可用工具: {[t.name for t in client.tools]}")

    # 调用工具
    await client.call_tool(ToolCall(name="write_file", input={"file_path": "hello.txt", "content": "Hello from gtsp!"}))
    print("✓ 写入 hello.txt")

    resp = await client.call_tool(ToolCall(name="read_file", input={"file_path": "hello.txt"}))
    print(f"✓ 读取: {resp.output[:50]}...")

    resp = await client.call_tool(ToolCall(name="execute_bash", input={"command": "ls -la hello.txt"}))
    print(f"✓ 执行: {resp.output.strip()}")

    await client.shutdown()


if __name__ == "__main__":
    asyncio.run(main())