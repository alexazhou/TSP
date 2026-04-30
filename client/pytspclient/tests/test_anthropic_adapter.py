"""Anthropic Adapter 测试"""

import json
import pytest
from dataclasses import dataclass
from typing import Any, Dict, List

from pytspclient import ToolCall, ToolResult
from tests.test_base import TSPTestCase


# ─────────────────────────────────────────────────────────────────────────────
# Mock Anthropic Response
# ─────────────────────────────────────────────────────────────────────────────

@dataclass
class MockAnthropicToolUse:
    type: str = "tool_use"
    id: str = ""
    name: str = ""
    input: Dict[str, Any] = None


@dataclass
class MockAnthropicText:
    type: str = "text"
    text: str = ""


@dataclass
class MockAnthropicResponse:
    content: List[Any]


# ─────────────────────────────────────────────────────────────────────────────
# Tests
# ─────────────────────────────────────────────────────────────────────────────

@pytest.mark.asyncio(loop_scope="class")
class TestAnthropicAdapter(TSPTestCase):
    """Anthropic Adapter 测试"""

    async def test_tools_format(self):
        """测试 tools 格式"""
        adapter = self.client.for_anthropic()
        tools = adapter.tools
        assert len(tools) > 0
        for t in tools:
            assert "name" in t
            assert "description" in t
            assert "input_schema" in t

    async def test_parse_tool_calls(self):
        """测试解析 tool_use"""
        adapter = self.client.for_anthropic()
        mock_response = MockAnthropicResponse(
            content=[
                MockAnthropicText(type="text", text="Let me read that file."),
                MockAnthropicToolUse(
                    type="tool_use",
                    id="toolu-1",
                    name="read_file",
                    input={"file_path": "test.txt"},
                ),
            ],
        )
        calls = adapter.parse_tool_calls(mock_response)
        assert len(calls) == 1
        assert calls[0].id == "toolu-1"
        assert calls[0].name == "read_file"
        assert calls[0].input == {"file_path": "test.txt"}

    async def test_execute_tool_calls(self):
        """测试执行 tool_calls"""
        adapter = self.client.for_anthropic()

        # 先写入文件
        write_call = ToolCall(name="write_file", input={"file_path": "/tmp/test_anthropic.txt", "content": "Anthropic test"})
        await self.client.call_tool(write_call)

        mock_response = MockAnthropicResponse(
            content=[
                MockAnthropicToolUse(
                    type="tool_use",
                    id="toolu-1",
                    name="read_file",
                    input={"file_path": "/tmp/test_anthropic.txt"},
                ),
            ],
        )
        results = await adapter.execute_tool_calls(mock_response)
        assert len(results) == 1
        assert results[0].call_id == "toolu-1"
        assert results[0].name == "read_file"
        assert "Anthropic test" in results[0].output

        # 清理
        await self.cleanup_file("/tmp/test_anthropic.txt")

    async def test_to_tool_messages(self):
        """测试转换 tool messages"""
        adapter = self.client.for_anthropic()
        results = [
            ToolResult(call_id="toolu-1", name="read_file", output=json.dumps({"content": "hello"})),
        ]
        message = adapter.to_tool_messages(results)
        assert message["role"] == "user"
        assert len(message["content"]) == 1
        assert message["content"][0]["type"] == "tool_result"
        assert message["content"][0]["tool_use_id"] == "toolu-1"

    async def test_get_text(self):
        """测试提取文本"""
        adapter = self.client.for_anthropic()
        mock_response = MockAnthropicResponse(
            content=[
                MockAnthropicText(type="text", text="Hello from Claude"),
            ],
        )
        text = adapter.get_text(mock_response)
        assert text == "Hello from Claude"