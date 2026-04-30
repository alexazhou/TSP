"""pytspclient 自动化测试

覆盖 Normal API 和 Adapter API 两类使用场景。
"""

import asyncio
import json
import os
import pytest
from dataclasses import dataclass
from typing import Any, Dict, List, Optional

from pytspclient import TSPClient, ToolCall, ToolResult, TSPTool
from pytspclient.adapters import TspOpenAIAdapter, TspAnthropicAdapter


# ─────────────────────────────────────────────────────────────────────────────
# 配置
# ─────────────────────────────────────────────────────────────────────────────

import platform

# 根据架构选择 gtsp 二进制
_arch = platform.machine()
if _arch == "arm64":
    GTSP_PATH = os.environ.get("GTSP_PATH", "../../gtsp/dist/gtsp-darwin-arm64")
else:
    GTSP_PATH = os.environ.get("GTSP_PATH", "../../gtsp/dist/gtsp-darwin-amd64")


# ─────────────────────────────────────────────────────────────────────────────
# Mock LLM Response
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
# Normal API Tests
# ─────────────────────────────────────────────────────────────────────────────

@pytest.mark.asyncio
async def test_normal_api_start_and_shutdown():
    """测试启动和关闭"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()
    assert client.tools is not None
    assert len(client.tools) > 0
    assert client.workdir is not None
    await client.shutdown()


@pytest.mark.asyncio
async def test_normal_api_tools_are_tsp_tool_objects():
    """测试 tools 属性返回 TSPTool 对象"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()
    for tool in client.tools:
        assert isinstance(tool, TSPTool)
        assert tool.name
        assert tool.description
        assert isinstance(tool.input_schema, dict)
    await client.shutdown()


@pytest.mark.asyncio
async def test_normal_api_call_tool():
    """测试 call_tool 执行"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()

    # 写入文件
    call = ToolCall(name="write_file", input={"file_path": "test_hello.txt", "content": "Hello TSP!"})
    result = await client.call_tool(call)
    assert result.name == "write_file"
    assert result.call_id == ""  # Normal API 不传 id

    # 读取文件
    call = ToolCall(name="read_file", input={"file_path": "test_hello.txt"})
    result = await client.call_tool(call)
    assert result.name == "read_file"
    assert "Hello TSP!" in result.output

    # 清理
    call = ToolCall(name="execute_bash", input={"command": "rm -f test_hello.txt"})
    await client.call_tool(call)

    await client.shutdown()


@pytest.mark.asyncio
async def test_normal_api_call_tool_with_id():
    """测试 call_tool 传入自定义 id"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()

    call = ToolCall(id="custom-123", name="write_file", input={"file_path": "test_id.txt", "content": "test"})
    result = await client.call_tool(call)
    assert result.call_id == "custom-123"

    # 清理
    call = ToolCall(name="execute_bash", input={"command": "rm -f test_id.txt"})
    await client.call_tool(call)

    await client.shutdown()


# ─────────────────────────────────────────────────────────────────────────────
# Adapter API Tests - OpenAI
# ─────────────────────────────────────────────────────────────────────────────

@pytest.mark.asyncio
async def test_openai_adapter_tools_format():
    """测试 OpenAI adapter tools 格式转换"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()
    adapter = client.for_openai()

    tools = adapter.tools
    assert len(tools) > 0
    for t in tools:
        assert t["type"] == "function"
        assert "function" in t
        assert t["function"]["name"]
        assert "parameters" in t["function"]

    await client.shutdown()


@pytest.mark.asyncio
async def test_openai_adapter_parse_tool_calls():
    """测试 OpenAI adapter 解析 tool_calls"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()
    adapter = client.for_openai()

    # 构造 mock response
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

    await client.shutdown()


@pytest.mark.asyncio
async def test_openai_adapter_execute_tool_calls():
    """测试 OpenAI adapter 执行 tool_calls"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()
    adapter = client.for_openai()

    # 先写入文件
    write_call = ToolCall(name="write_file", input={"file_path": "test_adapter.txt", "content": "Adapter test"})
    await client.call_tool(write_call)

    # 构造 mock response
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
                                arguments=json.dumps({"file_path": "test_adapter.txt"}),
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
    call = ToolCall(name="execute_bash", input={"command": "rm -f test_adapter.txt"})
    await client.call_tool(call)

    await client.shutdown()


@pytest.mark.asyncio
async def test_openai_adapter_to_tool_messages():
    """测试 OpenAI adapter 转换 tool messages"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()
    adapter = client.for_openai()

    results = [
        ToolResult(call_id="call-1", name="read_file", output=json.dumps({"content": "hello"})),
        ToolResult(call_id="call-2", name="write_file", output=json.dumps({"success": True})),
    ]

    messages = adapter.to_tool_messages(results)
    assert len(messages) == 2
    assert messages[0]["role"] == "tool"
    assert messages[0]["tool_call_id"] == "call-1"
    assert messages[1]["tool_call_id"] == "call-2"

    await client.shutdown()


@pytest.mark.asyncio
async def test_openai_adapter_get_text():
    """测试 OpenAI adapter 提取文本"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()
    adapter = client.for_openai()

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

    await client.shutdown()


# ─────────────────────────────────────────────────────────────────────────────
# Adapter API Tests - Anthropic
# ─────────────────────────────────────────────────────────────────────────────

@pytest.mark.asyncio
async def test_anthropic_adapter_tools_format():
    """测试 Anthropic adapter tools 格式"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()
    adapter = client.for_anthropic()

    tools = adapter.tools
    assert len(tools) > 0
    for t in tools:
        assert "name" in t
        assert "description" in t
        assert "input_schema" in t

    await client.shutdown()


@pytest.mark.asyncio
async def test_anthropic_adapter_parse_tool_calls():
    """测试 Anthropic adapter 解析 tool_use"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()
    adapter = client.for_anthropic()

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

    await client.shutdown()


@pytest.mark.asyncio
async def test_anthropic_adapter_execute_tool_calls():
    """测试 Anthropic adapter 执行 tool_calls"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()
    adapter = client.for_anthropic()

    # 先写入文件
    write_call = ToolCall(name="write_file", input={"file_path": "test_anthropic.txt", "content": "Anthropic test"})
    await client.call_tool(write_call)

    mock_response = MockAnthropicResponse(
        content=[
            MockAnthropicToolUse(
                type="tool_use",
                id="toolu-1",
                name="read_file",
                input={"file_path": "test_anthropic.txt"},
            ),
        ],
    )

    results = await adapter.execute_tool_calls(mock_response)
    assert len(results) == 1
    assert results[0].call_id == "toolu-1"
    assert results[0].name == "read_file"
    assert "Anthropic test" in results[0].output

    # 清理
    call = ToolCall(name="execute_bash", input={"command": "rm -f test_anthropic.txt"})
    await client.call_tool(call)

    await client.shutdown()


@pytest.mark.asyncio
async def test_anthropic_adapter_to_tool_messages():
    """测试 Anthropic adapter 转换 tool messages"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()
    adapter = client.for_anthropic()

    results = [
        ToolResult(call_id="toolu-1", name="read_file", output=json.dumps({"content": "hello"})),
    ]

    message = adapter.to_tool_messages(results)
    assert message["role"] == "user"
    assert len(message["content"]) == 1
    assert message["content"][0]["type"] == "tool_result"
    assert message["content"][0]["tool_use_id"] == "toolu-1"

    await client.shutdown()


@pytest.mark.asyncio
async def test_anthropic_adapter_get_text():
    """测试 Anthropic adapter 提取文本"""
    client = await TSPClient.from_stdio(GTSP_PATH).start()
    adapter = client.for_anthropic()

    mock_response = MockAnthropicResponse(
        content=[
            MockAnthropicText(type="text", text="Hello from Claude"),
        ],
    )

    text = adapter.get_text(mock_response)
    assert text == "Hello from Claude"

    await client.shutdown()