"""LLM Adapter 抽象基类"""

from abc import ABC, abstractmethod
from typing import Any, Dict, List

from ..types import ToolCall, ToolResult
from ..client import TSPClient


class LLMAdapter(ABC):
    """LLM Adapter 基类，屏蔽不同 LLM API 格式的差异。"""

    def __init__(self, tsp: TSPClient):
        self.tsp = tsp

    @property
    @abstractmethod
    def tools(self) -> List[Dict[str, Any]]:
        """返回该 LLM 格式的 tool schema 列表。"""
        ...

    @abstractmethod
    def parse_tool_calls(self, response: Any) -> List[ToolCall]:
        """从 LLM 响应中提取工具调用。"""
        ...

    @abstractmethod
    def get_text(self, response: Any) -> str:
        """提取最终文本回复。"""
        ...

    @abstractmethod
    def to_tool_messages(self, results: List[ToolResult]) -> Any:
        """将工具执行结果转换为该 LLM 格式的 messages。"""
        ...

    # 具体方法：parse + 执行，子类无需重写
    async def execute_tool_calls(self, response: Any) -> List[ToolResult]:
        """解析工具调用并执行，返回结果列表。"""
        calls = self.parse_tool_calls(response)
        results = []
        for c in calls:
            r = await self.tsp.call_tool(c)
            results.append(r)
        return results