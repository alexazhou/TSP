"""pytspclient — TSP 客户端 + LLM Adapter"""

from .client import TSPClient
from .types import (
    TSPRequest, TSPResponse, TSPEvent, TSPException,
    TSPInitializeResult, TSPToolResponse, TSPTool, TSPCapabilities,
    ToolCall, ToolResult,
)
from .adapters import LLMAdapter, TspAnthropicAdapter, TspOpenAIAdapter

__version__ = "0.2.5"
__all__ = [
    "TSPClient",
    "TSPRequest", "TSPResponse", "TSPEvent", "TSPException",
    "TSPInitializeResult", "TSPToolResponse", "TSPTool", "TSPCapabilities",
    "ToolCall", "ToolResult",
    "LLMAdapter", "TspAnthropicAdapter", "TspOpenAIAdapter",
]