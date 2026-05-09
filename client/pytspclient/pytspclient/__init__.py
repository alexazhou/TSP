"""pytspclient — TSP 客户端 + LLM Adapter"""

from .client import TSPClient
from .types import (
    TSPRequest, TSPResponse, TSPEvent, TSPException,
    TSPInitializeResult, TSPToolResponse, TSPTool, TSPCapabilities,
    ToolCall, ToolResult,
    TSP_ERROR_STDOUT_CLOSED, TSP_ERROR_CONNECTION_CLOSED,
)
from .adapters import LLMAdapter, TspAnthropicAdapter, TspOpenAIAdapter

__version__ = "0.2.6"
__all__ = [
    "TSPClient",
    "TSPRequest", "TSPResponse", "TSPEvent", "TSPException",
    "TSPInitializeResult", "TSPToolResponse", "TSPTool", "TSPCapabilities",
    "ToolCall", "ToolResult",
    "TSP_ERROR_STDOUT_CLOSED", "TSP_ERROR_CONNECTION_CLOSED",
    "LLMAdapter", "TspAnthropicAdapter", "TspOpenAIAdapter",
]