"""TSP 客户端 — 纯 TSP 协议实现"""

import asyncio
import json
import logging
import uuid
from typing import Any, Callable, Dict, List, Optional

from .types import (
    TSPRequest, TSPResponse, TSPEvent, TSPException,
    TSPInitializeResult, TSPTool, TSPToolResponse,
    ToolCall, ToolResult,
)
from .adapters.anthropic import TspAnthropicAdapter
from .adapters.openai import TspOpenAIAdapter

logger = logging.getLogger(__name__)

_DEFAULT_PROTOCOL_VERSION = "0.3"


class TSPClient:
    """TSP 客户端，只懂 TSP 协议，不涉及 LLM 格式。

    使用工厂方法创建实例：
        TSPClient.from_stdio("gtsp")
        TSPClient.from_websocket("ws://localhost:8080/tsp")
    """

    def __init__(self):
        raise TypeError("请使用工厂方法创建实例：TSPClient.from_stdio('gtsp')")

    def _init(self, command: List[str], request_timeout_sec: int = 30):
        """内部初始化方法，请使用 from_stdio 或 from_websocket。"""
        self.command = command
        self.request_timeout_sec = request_timeout_sec
        self.process: Optional[asyncio.subprocess.Process] = None
        self.in_flight: Dict[str, asyncio.Future] = {}
        self.event_handlers: List[Callable[[TSPEvent], None]] = []
        self._read_task: Optional[asyncio.Task] = None
        self._stderr_task: Optional[asyncio.Task] = None
        self._tools: List[TSPTool] = []
        self._workdir: str = ""

    @classmethod
    def from_stdio(cls, command: str, request_timeout_sec: int = 30) -> "TSPClient":
        """创建 stdio 模式的 TSP 客户端。

        Args:
            command: gtsp 命令（如 "gtsp" 或 "/path/to/gtsp"）
        """
        instance = cls.__new__(cls)
        instance._init([command], request_timeout_sec)
        return instance

    @classmethod
    def from_websocket(cls, url: str, token: Optional[str] = None, request_timeout_sec: int = 30) -> "TSPClient":
        """创建 websocket 模式的 TSP 客户端（暂未实现）。"""
        raise NotImplementedError("WebSocket mode not yet implemented")

    @property
    def tools(self) -> List[TSPTool]:
        """TSP 工具定义列表。"""
        return self._tools

    @property
    def workdir(self) -> str:
        """TSP 工作目录。"""
        return self._workdir

    async def connect(self) -> "TSPClient":
        """启动 TSP 进程并建立连接。返回 self 支持链式调用。"""
        self.process = await asyncio.create_subprocess_exec(
            *self.command,
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        self._read_task = asyncio.create_task(self._read_loop())
        self._stderr_task = asyncio.create_task(self._read_stderr_loop())
        return self

    async def start(self) -> "TSPClient":
        """connect + initialize，返回 self。"""
        await self.connect()
        await self.initialize()
        return self

    async def disconnect(self):
        """断开连接并终止 TSP 进程。"""
        if self._read_task:
            self._read_task.cancel()
            await asyncio.gather(self._read_task, return_exceptions=True)
        if self._stderr_task:
            self._stderr_task.cancel()
            await asyncio.gather(self._stderr_task, return_exceptions=True)

        self._read_task = None
        self._stderr_task = None

        if self.process:
            try:
                self.process.terminate()
                await self.process.wait()
            except ProcessLookupError:
                pass
            except Exception:
                if self.process:
                    self.process.kill()
                    await self.process.wait()
        self.process = None
        self._fail_pending(RuntimeError("TSP connection closed"))

    def _fail_pending(self, exc: Exception):
        for future in self.in_flight.values():
            if not future.done():
                future.set_exception(exc)
        self.in_flight.clear()

    async def _read_loop(self):
        try:
            while self.process and self.process.stdout:
                line = await self.process.stdout.readline()
                if not line:
                    break

                try:
                    data = json.loads(line.decode().strip())
                    msg_type = data.get("type")

                    if msg_type == "event":
                        event = TSPEvent.from_dict(data)
                        for handler in self.event_handlers:
                            handler(event)
                    else:
                        resp = TSPResponse.from_dict(data)
                        if resp.id is None:
                            logger.error(f"Received response without ID: {data}")
                            continue

                        resp_id = str(resp.id)
                        if resp_id in self.in_flight:
                            future = self.in_flight.pop(resp_id)
                            if not future.done():
                                future.set_result(resp)
                        else:
                            logger.warning(f"Received response for unknown request ID: {resp_id}")

                except json.JSONDecodeError:
                    logger.error(f"Failed to decode message: {line}")
        except asyncio.CancelledError:
            pass
        finally:
            self._fail_pending(RuntimeError("TSP stdout closed"))

    async def _read_stderr_loop(self) -> None:
        try:
            while self.process is not None and self.process.stderr is not None:
                line = await self.process.stderr.readline()
                if not line:
                    break
                logger.debug("TSP stderr: %s", line.decode("utf-8", errors="ignore").rstrip("\n"))
        except asyncio.CancelledError:
            pass

    async def request(self, method: str, input_params: Any, tool: Optional[str] = None) -> Any:
        if not self.process or not self.process.stdin:
            raise RuntimeError("Client not connected")

        req_id = str(uuid.uuid4())
        req = TSPRequest(id=req_id, method=method, input=input_params, tool=tool)

        loop = asyncio.get_running_loop()
        future = loop.create_future()
        self.in_flight[req_id] = future

        try:
            msg = json.dumps(req.to_dict(), ensure_ascii=False) + "\n"
            self.process.stdin.write(msg.encode("utf-8"))
            await self.process.stdin.drain()
        except Exception as e:
            self.in_flight.pop(req_id, None)
            raise e

        try:
            resp: TSPResponse = await asyncio.wait_for(future, timeout=self.request_timeout_sec)
        except asyncio.TimeoutError:
            self.in_flight.pop(req_id, None)
            raise TimeoutError(f"TSP request timeout after {self.request_timeout_sec}s")

        if resp.type == "error":
            raise TSPException(str(resp.code or "tsp/error"), str(resp.error))

        return resp.result

    async def initialize(
        self,
        client_info: Optional[Dict[str, str]] = None,
        include: Optional[List[str]] = None,
        exclude: Optional[List[str]] = None,
    ) -> "TSPClient":
        """初始化 TSP，返回 self 支持链式调用。"""
        params = {
            "protocolVersion": _DEFAULT_PROTOCOL_VERSION,
            "clientInfo": client_info or {"name": "pytspclient"},
        }
        tools_capability = {}
        if include:
            tools_capability["include"] = include
        if exclude:
            tools_capability["exclude"] = exclude
        if tools_capability:
            params["capabilities"] = {"tools": tools_capability}

        result_dict = await self.request("initialize", params)
        result = TSPInitializeResult.from_dict(result_dict)
        self._tools = result.capabilities.tools
        self._workdir = result.workdir
        return self

    async def sandbox(self, config: Dict[str, Any]) -> Dict[str, Any]:
        return await self.request("sandbox", config)

    async def call_tool(self, call: ToolCall) -> ToolResult:
        """执行工具调用。"""
        tsp_resp = await self.request("tool", call.input, tool=call.name)
        resp = TSPToolResponse.from_any(tsp_resp)
        output = json.dumps(resp.result, ensure_ascii=False) if isinstance(resp.result, dict) else str(resp.result)
        return ToolResult(call_id=call.id, name=call.name, output=output)

    async def shutdown(self):
        try:
            await self.request("shutdown", {})
        except Exception as e:
            logger.warning(f"TSP shutdown failed: {e}")
        finally:
            await self.disconnect()

    def add_event_handler(self, handler: Callable[[TSPEvent], None]):
        self.event_handlers.append(handler)

    # ─────────────────────────────────────────────────────────────────────────
    # Adapter 工厂方法
    # ─────────────────────────────────────────────────────────────────────────

    def for_anthropic(self) -> TspAnthropicAdapter:
        """创建 Anthropic Adapter。"""
        return TspAnthropicAdapter(self)

    def for_openai(self) -> TspOpenAIAdapter:
        """创建 OpenAI Adapter。"""
        return TspOpenAIAdapter(self)