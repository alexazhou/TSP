"""测试基类"""

import os
import platform
import asyncio
import pytest_asyncio
from pytspclient import TSPClient, ToolCall

_arch = platform.machine()
if _arch == "arm64":
    GTSP_PATH = os.environ.get("GTSP_PATH", "../../gtsp/dist/gtsp-darwin-arm64")
else:
    GTSP_PATH = os.environ.get("GTSP_PATH", "../../gtsp/dist/gtsp-darwin-amd64")


class TSPTestCase:
    """TSP 测试基类：setup_class 方式构建 client

    由于 TSPClient 有后台 _read_task 需要持续运行，
    使用 pytest_asyncio.fixture(scope="class") 来管理生命周期，
    配合 loop_scope="class" 让 fixture 和测试方法共享同一个 event loop。
    """

    client: TSPClient = None

    @pytest_asyncio.fixture(scope="class", autouse=True)
    async def _tsp_setup_teardown(self):
        """类级别 fixture：模拟 setup_class/teardown_class"""
        TSPTestCase.client = await TSPClient.from_stdio(GTSP_PATH).start()
        yield
        await TSPTestCase.client.shutdown()

    async def cleanup_file(self, filename: str):
        """清理测试文件"""
        await self.client.call_tool(ToolCall(name="execute_bash", input={"command": f"rm -f {filename}"}))