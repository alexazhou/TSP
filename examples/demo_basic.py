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
import os
import tempfile
from pytspclient import TSPClient, ToolCall

GTSP_PATH = "./gtsp"  # 替换为实际路径，如 "/path/to/gtsp"


async def main():
    client = await TSPClient.from_stdio(GTSP_PATH).start()

    print(f"可用工具: {[t.name for t in client.tools]}")

    # 使用临时目录，避免污染当前目录
    tmp_dir = tempfile.gettempdir()
    test_file = os.path.join(tmp_dir, "hello.txt")

    # 调用工具
    await client.call_tool(ToolCall(name="write_file", input={"file_path": test_file, "content": "Hello from gtsp!"}))
    print(f"✓ 写入 {test_file}")

    resp = await client.call_tool(ToolCall(name="read_file", input={"file_path": test_file}))
    content = resp.output.get("content", "")
    print(f"✓ 读取: {content[:50]}...")

    resp = await client.call_tool(ToolCall(name="execute_bash", input={"command": f"ls -la {test_file}"}))
    stdout = resp.output.get("stdout", "")
    print(f"✓ 执行: {stdout.strip()}")

    # 清理测试文件
    await client.call_tool(ToolCall(name="execute_bash", input={"command": f"rm -f {test_file}"}))
    print(f"✓ 清理 {test_file}")

    await client.shutdown()


if __name__ == "__main__":
    asyncio.run(main())