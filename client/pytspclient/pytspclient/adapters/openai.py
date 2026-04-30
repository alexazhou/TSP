"""OpenAI Adapter"""

import json
from typing import Any, Dict, List

from .base import LLMAdapter
from ..types import ToolCall, ToolResult


class TspOpenAIAdapter(LLMAdapter):
    """OpenAI Chat Completions API Adapter。"""

    @property
    def tools(self) -> List[Dict[str, Any]]:
        """将 TSP schema 转换为 OpenAI tools 格式。"""
        return [
            {
                "type": "function",
                "function": {
                    "name": t.name,
                    "description": t.description,
                    "parameters": t.input_schema,
                },
            }
            for t in self.tsp.tools
        ]

    def parse_tool_calls(self, response: "openai.types.chat.ChatCompletion") -> List[ToolCall]:
        """从 OpenAI ChatCompletion 响应中提取 tool_calls。"""
        calls = response.choices[0].message.tool_calls or []
        return [
            ToolCall(
                id=c.id,
                name=c.function.name,
                input=json.loads(c.function.arguments),
            )
            for c in calls
        ]

    def get_text(self, response: "openai.types.chat.ChatCompletion") -> str:
        """提取 message content。"""
        return response.choices[0].message.content or ""

    def to_tool_messages(self, results: List[ToolResult]) -> List[Dict[str, Any]]:
        """将工具执行结果转换为 OpenAI tool messages。"""
        return [
            {"role": "tool", "tool_call_id": r.call_id, "content": r.output}
            for r in results
        ]