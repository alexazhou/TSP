"""TSP 协议类型 + LLM Adapter 中间类型"""

from dataclasses import dataclass, field, asdict
from typing import Any, Dict, List, Optional, Union
import json


# ─────────────────────────────────────────────────────────────────────────────
# TSP 协议类型
# ─────────────────────────────────────────────────────────────────────────────

@dataclass
class TSPRequest:
    id: str
    method: str
    input: Any
    tool: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        input_data = self.input
        if hasattr(input_data, "__dataclass_fields__"):
            input_data = asdict(input_data)
        d = {"id": self.id, "method": self.method, "input": input_data}
        if self.tool:
            d["tool"] = self.tool
        return d


@dataclass
class TSPResponse:
    id: Optional[str]
    type: str  # "result" | "error"
    result: Any = None
    code: Optional[str] = None
    error: Optional[Union[str, Dict[str, Any]]] = None

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "TSPResponse":
        return cls(
            id=d.get("id"),
            type=d.get("type", "result"),
            result=d.get("result"),
            code=d.get("code"),
            error=d.get("error"),
        )


@dataclass
class TSPTool:
    """TSP 工具定义"""
    name: str
    description: str = ""
    input_schema: Dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "TSPTool":
        return cls(
            name=d.get("name", ""),
            description=d.get("description", ""),
            input_schema=d.get("input_schema") or d.get("inputSchema") or {},
        )

    def to_dict(self) -> Dict[str, Any]:
        return {
            "name": self.name,
            "description": self.description,
            "input_schema": self.input_schema,
        }


@dataclass
class TSPCapabilities:
    tools: List[TSPTool] = field(default_factory=list)

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "TSPCapabilities":
        tools_data = d.get("tools", [])
        return cls(tools=[TSPTool.from_dict(t) for t in tools_data])


@dataclass
class TSPInitializeResult:
    protocol_version: str
    capabilities: TSPCapabilities = field(default_factory=TSPCapabilities)
    server_info: Dict[str, Any] = field(default_factory=dict)
    workdir: str = ""

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "TSPInitializeResult":
        return cls(
            protocol_version=d.get("protocolVersion") or d.get("protocol_version", ""),
            capabilities=TSPCapabilities.from_dict(d.get("capabilities", {})),
            server_info=d.get("serverInfo") or d.get("server_info", {}),
            workdir=d.get("workdir", ""),
        )


@dataclass
class TSPToolResponse:
    success: bool
    result: Any
    message: Optional[str] = None
    code: Optional[str] = None

    @classmethod
    def from_any(cls, result: Any) -> "TSPToolResponse":
        if isinstance(result, dict):
            return cls(
                success=result.get("success", True),
                result=result,
                message=result.get("message"),
                code=result.get("code"),
            )
        return cls(success=True, result=result)


@dataclass
class TSPEvent:
    type: str = "event"
    event: str = ""
    data: Dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "TSPEvent":
        return cls(
            type=d.get("type", "event"),
            event=d.get("event", ""),
            data=d.get("data", {}),
        )


class TSPException(Exception):
    def __init__(self, code: str, message: str, data: Any = None):
        super().__init__(f"[{code}] {message}")
        self.code = code
        self.message = message
        self.data = data


# ─────────────────────────────────────────────────────────────────────────────
# LLM Adapter 中间类型
# ─────────────────────────────────────────────────────────────────────────────

@dataclass
class ToolCall:
    """工具调用请求"""
    name: str                 # 工具名
    input: Dict[str, Any]     # 工具参数
    id: str = ""              # 调用 ID（可选，用于关联结果）

    def to_dict(self) -> Dict[str, Any]:
        """转换为 OpenAI tool_calls 格式。"""
        import json
        return {
            "id": self.id,
            "type": "function",
            "function": {
                "name": self.name,
                "arguments": json.dumps(self.input, ensure_ascii=False),
            },
        }


@dataclass
class ToolResult:
    """工具执行结果（统一中间格式）"""
    call_id: str  # 对应 ToolCall.id
    name: str     # 工具名（Anthropic 不需要，但保留备用）
    output: str   # JSON 字符串，result 或 error