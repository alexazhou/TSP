"""测试 TSP 错误码场景：stdout-closed / connection-closed / _connected 标志位"""

import asyncio
import pytest
from unittest.mock import AsyncMock, MagicMock, patch

from pytspclient import TSPClient, TSPException
from pytspclient.types import TSP_ERROR_STDOUT_CLOSED, TSP_ERROR_CONNECTION_CLOSED


# ── helpers ──────────────────────────────────────────────────────────────────

def _make_eof_process():
    """mock 进程：stdout 立即返回空 → 模拟 EOF / stdout 关闭"""
    proc = MagicMock()
    proc.stdin = AsyncMock()
    proc.stdout = MagicMock()
    proc.stdout.readline = AsyncMock(return_value=b"")
    proc.stderr = MagicMock()
    proc.stderr.readline = AsyncMock(return_value=b"")
    proc.wait = AsyncMock()
    return proc


def _make_hanging_process():
    """mock 进程：stdout.readline 永远不返回 → read_loop 保持存活"""
    proc = MagicMock()
    proc.stdin = AsyncMock()
    proc.stdout = MagicMock()
    proc.stdout.readline = AsyncMock(side_effect=lambda: asyncio.get_running_loop().create_future())
    proc.stderr = MagicMock()
    proc.stderr.readline = AsyncMock(side_effect=lambda: asyncio.get_running_loop().create_future())
    proc.wait = AsyncMock()
    return proc


# ── _connected 标志位 ────────────────────────────────────────────────────────

class TestConnectedFlag:
    """_connected 标志位状态转换"""

    @pytest.fixture
    def client(self):
        return TSPClient.from_stdio("gtsp", request_timeout_sec=1)

    @pytest.mark.asyncio
    async def test_connected_true_after_connect(self, client):
        """connect() 后 _connected 应为 True"""
        proc = _make_eof_process()
        with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=proc)):
            await client.connect()
        assert client._connected is True

    @pytest.mark.asyncio
    async def test_connected_false_after_disconnect(self, client):
        """disconnect() 后 _connected 应为 False"""
        proc = _make_eof_process()
        with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=proc)):
            await client.connect()
        await client.disconnect()
        assert client._connected is False

    @pytest.mark.asyncio
    async def test_connected_false_after_stdout_closed(self, client):
        """stdout 断开（read_loop 退出）后 _connected 应为 False"""
        proc = _make_eof_process()
        with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=proc)):
            await client.connect()
        # read_loop 因 EOF 立即退出，等事件循环处理完
        await asyncio.sleep(0.05)
        assert client._connected is False


# ── stdout-closed ────────────────────────────────────────────────────────────

class TestStdoutClosed:
    """stdout 管道意外关闭场景"""

    @pytest.fixture
    def client(self):
        return TSPClient.from_stdio("gtsp", request_timeout_sec=1)

    @pytest.mark.asyncio
    async def test_inflight_fails_with_stdout_closed_code(self, client):
        """stdout 断开后，存量 in-flight 请求收到 tsp/stdout-closed"""
        proc = _make_eof_process()
        with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=proc)):
            await client.connect()

        future = asyncio.get_running_loop().create_future()
        client.in_flight["test-001"] = future

        # read_loop 因 EOF 退出，finally → _fail_pending(stdout-closed)
        await asyncio.sleep(0.05)

        with pytest.raises(TSPException) as exc_info:
            await future
        assert exc_info.value.code == TSP_ERROR_STDOUT_CLOSED
        assert "TSP stdout closed" in str(exc_info.value)

    @pytest.mark.asyncio
    async def test_new_request_fails_immediately(self, client):
        """stdout 断开后新请求立即失败（不等超时）"""
        proc = _make_eof_process()
        with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=proc)):
            await client.connect()
        await asyncio.sleep(0.05)  # read_loop 已退出

        with pytest.raises(TSPException) as exc_info:
            await client.request("test", {})
        assert exc_info.value.code == TSP_ERROR_STDOUT_CLOSED


# ── connection-closed ────────────────────────────────────────────────────────

class TestConnectionClosed:
    """主动 disconnect 场景"""

    @pytest.fixture
    def client(self):
        return TSPClient.from_stdio("gtsp", request_timeout_sec=1)

    @pytest.mark.asyncio
    async def test_disconnect_fails_inflight_with_connection_closed(self, client):
        """主动 disconnect 后，存量 in-flight 请求收到 tsp/connection-closed"""
        proc = _make_hanging_process()
        with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=proc)):
            await client.connect()

        future = asyncio.get_running_loop().create_future()
        client.in_flight["test-001"] = future

        await client.disconnect()

        with pytest.raises(TSPException) as exc_info:
            await future
        assert exc_info.value.code == TSP_ERROR_CONNECTION_CLOSED
        assert "TSP connection closed" in str(exc_info.value)

    @pytest.mark.asyncio
    async def test_inflight_cleared_after_disconnect(self, client):
        """disconnect 后 in_flight 应被清空"""
        proc = _make_hanging_process()
        with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=proc)):
            await client.connect()

        f1 = asyncio.get_running_loop().create_future()
        f2 = asyncio.get_running_loop().create_future()
        client.in_flight["a"] = f1
        client.in_flight["b"] = f2

        await client.disconnect()

        assert len(client.in_flight) == 0
        # 验证两个 future 都收到了异常
        for f in (f1, f2):
            with pytest.raises(TSPException) as exc_info:
                await f
            assert exc_info.value.code == TSP_ERROR_CONNECTION_CLOSED


# ── 综合 ─────────────────────────────────────────────────────────────────────

class TestCombined:
    """stdout-closed 后再 disconnect 的场景"""

    @pytest.fixture
    def client(self):
        return TSPClient.from_stdio("gtsp", request_timeout_sec=1)

    @pytest.mark.asyncio
    async def test_disconnect_after_stdout_closed_is_noop_for_pending(self, client):
        """stdout 已关闭后 disconnect：_fail_pending 为空操作，不报错"""
        proc = _make_eof_process()
        with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=proc)):
            await client.connect()
        await asyncio.sleep(0.05)  # stdout 已关闭

        # disconnect 应正常完成，不抛异常
        await client.disconnect()
        assert len(client.in_flight) == 0
