"""Anthropic Adapter"""

from typing import Any, Dict, List

from .base import LLMAdapter
from ..types import ToolCall, ToolResult


class TspAnthropicAdapter(LLMAdapter):
    """Anthropic Claude API Adapter。

    TSP schema 就是 Anthropic 格式，零转换。
    """

    @property
    def tools(self) -> List[Dict[str, Any]]:
        """直接返回 TSP 原始 schema（Anthropic 格式）。"""
        return [t.to_dict() for t in self.tsp.tools]

    def parse_tool_calls(self, response: "anthropic.types.Message") -> List[ToolCall]:
        """从 Anthropic Messages API 响应中提取 tool_use blocks。"""
        return [
            ToolCall(id=b.id, name=b.name, input=b.input)
            for b in response.content
            if hasattr(b, "type") and b.type == "tool_use"
        ]

    def get_text(self, response: "anthropic.types.Message") -> str:
        """提取 text block 内容。"""
        for b in response.content:
            if hasattr(b, "type") and b.type == "text":
                return b.text
        return ""

    def to_tool_messages(self, results: List[ToolResult]) -> Dict[str, Any]:
        """将工具执行结果转换为 Anthropic user message（包含 tool_result blocks）。"""
        return {
            "role": "user",
            "content": [
                {"type": "tool_result", "tool_use_id": r.call_id, "content": r.output}
                for r in results
            ],
        }