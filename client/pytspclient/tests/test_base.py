"""测试基类：管理 TSPClient 生命周期"""

import os
import platform
import pytest
import pytest_asyncio
from pytspclient import TSPClient, ToolCall


_arch = platform.machine()
if _arch == "arm64":
    GTSP_PATH = os.environ.get("GTSP_PATH", "../../gtsp/dist/gtsp-darwin-arm64")
else:
    GTSP_PATH = os.environ.get("GTSP_PATH", "../../gtsp/dist/gtsp-darwin-amd64")


class TSPTestCase:
    """TSP 测试基类：通过 autouse fixture 管理 TSPClient 生命周期

    子类直接通过 self.client 访问，测试方法不需要显式传 fixture 参数。
    """

    _client: TSPClient = None

    @pytest_asyncio.fixture(autouse=True)
    async def _setup_tsp_client(self):
        """自动为每个测试方法创建/销毁 TSPClient"""
        self._client = await TSPClient.from_stdio(GTSP_PATH).start()
        yield
        if self._client:
            await self._client.shutdown()
            self._client = None

    @property
    def client(self) -> TSPClient:
        """子类通过 self.client 访问"""
        return self._client

    async def cleanup_file(self, filename: str):
        """清理测试文件"""
        await self.client.call_tool(ToolCall(name="execute_bash", input={"command": f"rm -f {filename}"}))