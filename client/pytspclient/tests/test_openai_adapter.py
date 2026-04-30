"""OpenAI Adapter 测试"""

import json
import pytest
from dataclasses import dataclass
from typing import Any, List, Optional

from pytspclient import ToolCall, ToolResult
from tests.test_base import TSPTestCase


# ─────────────────────────────────────────────────────────────────────────────
# Mock OpenAI Response
# ─────────────────────────────────────────────────────────────────────────────

@dataclass
class MockOpenAIToolCall:
    id: str
    type: str = "function"
    function: Any = None


@dataclass
class MockOpenAIFunction:
    name: str
    arguments: str


@dataclass
class MockOpenAIMessage:
    content: Optional[str]
    tool_calls: List[MockOpenAIToolCall]


@dataclass
class MockOpenAIChoice:
    message: MockOpenAIMessage


@dataclass
class MockOpenAIResponse:
    choices: List[MockOpenAIChoice]


# ─────────────────────────────────────────────────────────────────────────────
# Tests
# ─────────────────────────────────────────────────────────────────────────────

@pytest.mark.asyncio(loop_scope="class")
class TestOpenAIAdapter(TSPTestCase):
    """OpenAI Adapter 测试"""

    async def test_tools_format(self):
        """测试 tools 格式转换"""
        adapter = self.client.for_openai()
        tools = adapter.tools
        assert len(tools) > 0
        for t in tools:
            assert t["type"] == "function"
            assert "function" in t
            assert t["function"]["name"]
            assert "parameters" in t["function"]

    async def test_parse_tool_calls(self):
        """测试解析 tool_calls"""
        adapter = self.client.for_openai()
        mock_response = MockOpenAIResponse(
            choices=[
                MockOpenAIChoice(
                    message=MockOpenAIMessage(
                        content=None,
                        tool_calls=[
                            MockOpenAIToolCall(
                                id="call-1",
                                function=MockOpenAIFunction(
                                    name="read_file",
                                    arguments=json.dumps({"file_path": "test.txt"}),
                                ),
                            ),
                        ],
                    ),
                ),
            ],
        )
        calls = adapter.parse_tool_calls(mock_response)
        assert len(calls) == 1
        assert calls[0].id == "call-1"
        assert calls[0].name == "read_file"
        assert calls[0].input == {"file_path": "test.txt"}

    async def test_execute_tool_calls(self):
        """测试执行 tool_calls"""
        adapter = self.client.for_openai()

        # 先写入文件
        write_call = ToolCall(name="write_file", input={"file_path": "/tmp/test_adapter.txt", "content": "Adapter test"})
        await self.client.call_tool(write_call)

        mock_response = MockOpenAIResponse(
            choices=[
                MockOpenAIChoice(
                    message=MockOpenAIMessage(
                        content=None,
                        tool_calls=[
                            MockOpenAIToolCall(
                                id="call-1",
                                function=MockOpenAIFunction(
                                    name="read_file",
                                    arguments=json.dumps({"file_path": "/tmp/test_adapter.txt"}),
                                ),
                            ),
                        ],
                    ),
                ),
            ],
        )
        results = await adapter.execute_tool_calls(mock_response)
        assert len(results) == 1
        assert results[0].call_id == "call-1"
        assert results[0].name == "read_file"
        assert "Adapter test" in results[0].output

        # 清理
        await self.cleanup_file("/tmp/test_adapter.txt")

    async def test_to_tool_messages(self):
        """测试转换 tool messages"""
        adapter = self.client.for_openai()
        results = [
            ToolResult(call_id="call-1", name="read_file", output=json.dumps({"content": "hello"})),
            ToolResult(call_id="call-2", name="write_file", output=json.dumps({"success": True})),
        ]
        messages = adapter.to_tool_messages(results)
        assert len(messages) == 2
        assert messages[0]["role"] == "tool"
        assert messages[0]["tool_call_id"] == "call-1"
        assert messages[1]["tool_call_id"] == "call-2"

    async def test_get_text(self):
        """测试提取文本"""
        adapter = self.client.for_openai()
        mock_response = MockOpenAIResponse(
            choices=[
                MockOpenAIChoice(
                    message=MockOpenAIMessage(
                        content="Hello from LLM",
                        tool_calls=[],
                    ),
                ),
            ],
        )
        text = adapter.get_text(mock_response)
        assert text == "Hello from LLM"