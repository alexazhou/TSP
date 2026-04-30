"""Normal API 测试：直接调用工具"""

import pytest
from pytspclient import ToolCall, TSPTool
from tests.test_base import TSPTestCase


class TestNormalAPI(TSPTestCase):
    """Normal API 测试：直接调用工具"""

    @pytest.mark.asyncio
    async def test_tools_are_tsp_tool_objects(self):
        """测试 tools 属性返回 TSPTool 对象"""
        for tool in self.client.tools:
            assert isinstance(tool, TSPTool)
            assert tool.name
            assert tool.description
            assert isinstance(tool.input_schema, dict)

    @pytest.mark.asyncio
    async def test_call_tool(self):
        """测试 call_tool 执行"""
        # 写入文件
        call = ToolCall(name="write_file", input={"file_path": "test_hello.txt", "content": "Hello TSP!"})
        result = await self.client.call_tool(call)
        assert result.name == "write_file"
        assert result.call_id == ""  # Normal API 不传 id

        # 读取文件
        call = ToolCall(name="read_file", input={"file_path": "test_hello.txt"})
        result = await self.client.call_tool(call)
        assert result.name == "read_file"
        assert "Hello TSP!" in result.output

        # 清理
        await self.cleanup_file("test_hello.txt")

    @pytest.mark.asyncio
    async def test_call_tool_with_id(self):
        """测试 call_tool 传入自定义 id"""
        call = ToolCall(id="custom-123", name="write_file", input={"file_path": "test_id.txt", "content": "test"})
        result = await self.client.call_tool(call)
        assert result.call_id == "custom-123"

        # 清理
        await self.cleanup_file("test_id.txt")