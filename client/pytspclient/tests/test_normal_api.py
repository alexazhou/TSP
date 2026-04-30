"""Normal API 测试：直接调用工具"""

import pytest
from pytspclient import ToolCall, TSPTool
from tests.test_base import TSPTestCase


@pytest.mark.asyncio(loop_scope="class")
class TestNormalAPI(TSPTestCase):
    """Normal API 测试：直接调用工具"""

    async def test_tools_are_tsp_tool_objects(self):
        """测试 tools 属性返回 TSPTool 对象"""
        for tool in self.client.tools:
            assert isinstance(tool, TSPTool)
            assert tool.name
            assert tool.description
            assert isinstance(tool.input_schema, dict)

    async def test_call_tool(self):
        """测试 call_tool 执行"""
        # 写入文件
        call = ToolCall(name="write_file", input={"file_path": "/tmp/test_hello.txt", "content": "Hello TSP!"})
        result = await self.client.call_tool(call)
        assert result.name == "write_file"
        assert result.call_id == ""  # Normal API 不传 id

        # 读取文件
        call = ToolCall(name="read_file", input={"file_path": "/tmp/test_hello.txt"})
        result = await self.client.call_tool(call)
        assert result.name == "read_file"
        assert "Hello TSP!" in result.output

        # 清理
        await self.cleanup_file("/tmp/test_hello.txt")

    async def test_call_tool_with_id(self):
        """测试 call_tool 传入自定义 id"""
        call = ToolCall(id="custom-123", name="write_file", input={"file_path": "/tmp/test_id.txt", "content": "test content"})
        result = await self.client.call_tool(call)
        assert result.call_id == "custom-123"

        # 读取验证写入成功
        call = ToolCall(name="read_file", input={"file_path": "/tmp/test_id.txt"})
        result = await self.client.call_tool(call)
        assert "test content" in result.output

        # 清理
        await self.cleanup_file("/tmp/test_id.txt")

    async def test_execute_bash(self):
        """测试 execute_bash 执行命令"""
        call = ToolCall(name="execute_bash", input={"command": "date"})
        result = await self.client.call_tool(call)
        assert result.name == "execute_bash"
        # output 应包含时间信息
        assert len(result.output) > 0