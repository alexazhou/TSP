#!/usr/bin/env python3
"""
Test websocket mode session logging.
"""

import subprocess
import json
import os
import time
import glob
import websocket

PROJECT_DIR = "/Volumes/PDATA/GitDB/GTAgentHands"
LOG_DIR = os.path.join(PROJECT_DIR, "logs")
BINARY = os.path.join(PROJECT_DIR, "gtsp")

def clear_session_logs():
    """Remove session log files."""
    if os.path.exists(LOG_DIR):
        for f in glob.glob(os.path.join(LOG_DIR, "gt-agent-*.log")):
            os.remove(f)

def test_websocket_sessions():
    """Test that websocket connections create separate session logs."""
    print("=" * 70)
    print("WEBSOCKET SESSION LOGGING TEST")
    print("=" * 70)
    
    clear_session_logs()
    
    # Start websocket server
    print("\n[Starting WebSocket server on port 9876...]")
    server_proc = subprocess.Popen(
        [BINARY, "--mode", "websocket", "--port", "9876"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        cwd=PROJECT_DIR
    )
    
    time.sleep(1)  # Wait for server to start
    
    try:
        # Connect multiple clients
        clients = []
        num_clients = 3
        
        print(f"[Connecting {num_clients} WebSocket clients...]")
        
        for i in range(num_clients):
            ws = websocket.create_connection("ws://localhost:9876/tsp")
            init = json.dumps({"id": str(i), "method": "initialize", "input": {"protocolVersion": "0.3"}})
            ws.send(init)
            response = ws.recv()
            print(f"  Client {i} initialized")
            clients.append(ws)
            time.sleep(0.1)
        
        time.sleep(0.5)
        
        # Check log files
        session_logs = glob.glob(os.path.join(LOG_DIR, "gt-agent-*.log"))
        
        print(f"\n[Result] Found {len(session_logs)} log files:")
        for log_file in session_logs:
            basename = os.path.basename(log_file)
            size = os.path.getsize(log_file)
            print(f"  - {basename} ({size} bytes)")
        
        # Clean up clients
        for ws in clients:
            try:
                shutdown = json.dumps({"id": "99", "method": "shutdown", "input": {}})
                ws.send(shutdown)
                ws.recv()
            except:
                pass
            finally:
                ws.close()
        
        # Verify result
        if len(session_logs) >= num_clients:
            print(f"\n✅ PASS: At least {num_clients} session logs created for WebSocket connections")
            return True
        else:
            print(f"\n❌ FAIL: Expected at least {num_clients} log files, found {len(session_logs)}")
            return False
            
    finally:
        # Stop server
        server_proc.terminate()
        server_proc.wait()

def main():
    print("\n" + "=" * 70)
    print("WEBSOCKET SESSION LOGGING TEST")
    print("=" * 70)
    
    # Check if websocket-client is installed
    try:
        import websocket
    except ImportError:
        print("\n❌ SKIP: websocket-client not installed. Install with: pip install websocket-client")
        return 0
    
    result = test_websocket_sessions()
    
    print("\n" + "=" * 70)
    print("TEST SUMMARY")
    print("=" * 70)
    
    status = "✅ PASS" if result else "❌ FAIL"
    print(f"{status}: WebSocket session logging")
    
    return 0 if result else 1

if __name__ == "__main__":
    exit(main())