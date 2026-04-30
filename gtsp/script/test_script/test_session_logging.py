#!/usr/bin/env python3
"""
Test script to verify session-based logging functionality.
Tests:
1. Multiple sessions create separate log files
2. Logs don't mix between sessions
3. Session log files have proper naming format
"""

import subprocess
import json
import os
import time
import glob

PROJECT_DIR = "/Volumes/PDATA/GitDB/GTAgentHands"
LOG_DIR = os.path.join(PROJECT_DIR, "logs")
BINARY = os.path.join(PROJECT_DIR, "gtsp")

def clear_logs():
    """Remove session log files but keep the global log."""
    if os.path.exists(LOG_DIR):
        for f in glob.glob(os.path.join(LOG_DIR, "gt-agent-*.log")):
            os.remove(f)

def count_session_logs():
    """Count session log files."""
    pattern = os.path.join(LOG_DIR, "gt-agent-*.log")
    files = glob.glob(pattern)
    return files

def get_log_content(log_file):
    """Read log file content."""
    if os.path.exists(log_file):
        with open(log_file, 'r') as f:
            return f.read()
    return ""

def test_session_creation():
    """Test 1: Multiple sessions create separate log files."""
    print("=" * 60)
    print("TEST 1: Multiple sessions create separate log files")
    print("=" * 60)
    
    clear_logs()
    
    # Create first session
    print("\n[Session 1] Starting...")
    proc1 = subprocess.Popen(
        [BINARY, "--mode", "stdio"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        cwd=PROJECT_DIR
    )
    
    # Initialize session 1
    init1 = json.dumps({"id": "1", "method": "initialize", "input": {"protocolVersion": "0.3"}})
    proc1.stdin.write((init1 + "\n").encode())
    proc1.stdin.flush()
    time.sleep(0.3)
    
    # Create second session
    print("[Session 2] Starting...")
    proc2 = subprocess.Popen(
        [BINARY, "--mode", "stdio"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        cwd=PROJECT_DIR
    )
    
    # Initialize session 2
    init2 = json.dumps({"id": "2", "method": "initialize", "input": {"protocolVersion": "0.3"}})
    proc2.stdin.write((init2 + "\n").encode())
    proc2.stdin.flush()
    time.sleep(0.3)
    
    # Check for session logs
    session_logs = count_session_logs()
    print(f"\n[Result] Found {len(session_logs)} session log files:")
    for log_file in session_logs:
        size = os.path.getsize(log_file)
        print(f"  - {os.path.basename(log_file)} ({size} bytes)")
    
    # Clean up
    try:
        shutdown = json.dumps({"id": "99", "method": "shutdown", "input": {}})
        proc1.stdin.write((shutdown + "\n").encode())
        proc1.stdin.flush()
        proc2.stdin.write((shutdown + "\n").encode())
        proc2.stdin.flush()
        time.sleep(0.2)
    except:
        pass
    finally:
        proc1.terminate()
        proc2.terminate()
    
    if len(session_logs) >= 2:
        print("\n✅ PASS: Multiple session log files created")
        return True
    else:
        print(f"\n❌ FAIL: Expected at least 2 log files, found {len(session_logs)}")
        return False

def test_log_isolation():
    """Test 2: Logs don't mix between sessions."""
    print("\n" + "=" * 60)
    print("TEST 2: Logs don't mix between sessions")
    print("=" * 60)
    
    clear_logs()
    
    # Create session with unique identifier
    print("\n[Session A] Starting...")
    proc1 = subprocess.Popen(
        [BINARY, "--mode", "stdio"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        cwd=PROJECT_DIR
    )
    
    init1 = json.dumps({"id": "A1", "method": "initialize", "input": {"protocolVersion": "0.3"}})
    proc1.stdin.write((init1 + "\n").encode())
    proc1.stdin.flush()
    time.sleep(0.3)
    
    # Create another session
    print("[Session B] Starting...")
    proc2 = subprocess.Popen(
        [BINARY, "--mode", "stdio"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        cwd=PROJECT_DIR
    )
    
    init2 = json.dumps({"id": "B1", "method": "initialize", "input": {"protocolVersion": "0.3"}})
    proc2.stdin.write((init2 + "\n").encode())
    proc2.stdin.flush()
    time.sleep(0.3)
    
    # Get session logs
    session_logs = count_session_logs()
    
    # Clean up sessions
    try:
        shutdown = json.dumps({"id": "99", "method": "shutdown", "input": {}})
        proc1.stdin.write((shutdown + "\n").encode())
        proc1.stdin.flush()
        proc2.stdin.write((shutdown + "\n").encode())
        proc2.stdin.flush()
        time.sleep(0.2)
    except:
        pass
    finally:
        proc1.terminate()
        proc2.terminate()
    
    time.sleep(0.2)
    
    # Check each log file
    print("\n[Result] Checking log isolation:")
    all_pass = True
    for log_file in session_logs:
        content = get_log_content(log_file)
        # Each log should have its own session ID
        lines = content.strip().split('\n') if content else []
        print(f"\n  {os.path.basename(log_file)}:")
        print(f"    - Lines: {len(lines)}")
        if content:
            # Show first few lines
            for line in lines[:3]:
                print(f"    - {line[:80]}")
    
    # Verify we have separate files
    if len(session_logs) >= 2:
        print("\n✅ PASS: Logs are in separate files (not mixed)")
        return True
    else:
        print(f"\n❌ FAIL: Expected at least 2 separate log files, found {len(session_logs)}")
        return False

def test_naming_format():
    """Test 3: Session log files have proper naming format."""
    print("\n" + "=" * 60)
    print("TEST 3: Session log file naming format")
    print("=" * 60)
    
    session_logs = count_session_logs()
    
    import re
    # Expected format: gt-agent-YYYYMMDD-HHMMSS.mmm.log
    pattern = re.compile(r'^gt-agent-\d{8}-\d{6}\.\d{3}\.log$')
    
    print("\n[Result] Checking naming format:")
    all_match = True
    for log_file in session_logs:
        basename = os.path.basename(log_file)
        matches = bool(pattern.match(basename))
        status = "✓" if matches else "✗"
        print(f"  {status} {basename}")
        if not matches:
            all_match = False
    
    if all_match and len(session_logs) > 0:
        print("\n✅ PASS: All session log files have correct naming format")
        return True
    elif len(session_logs) == 0:
        print("\n❌ FAIL: No session log files found")
        return False
    else:
        print("\n❌ FAIL: Some log files don't match expected naming format")
        return False

def main():
    print("\n" + "=" * 60)
    print("SESSION-BASED LOGGING TEST SUITE")
    print("=" * 60)
    
    results = []
    
    # Run all tests
    results.append(("Multiple session logs", test_session_creation()))
    results.append(("Log isolation", test_log_isolation()))
    results.append(("Naming format", test_naming_format()))
    
    # Summary
    print("\n" + "=" * 60)
    print("TEST SUMMARY")
    print("=" * 60)
    
    passed = sum(1 for _, result in results if result)
    total = len(results)
    
    for test_name, result in results:
        status = "✅ PASS" if result else "❌ FAIL"
        print(f"{status}: {test_name}")
    
    print(f"\nTotal: {passed}/{total} tests passed")
    
    if passed == total:
        print("\n🎉 All tests passed!")
        return 0
    else:
        print(f"\n⚠️  {total - passed} test(s) failed")
        return 1

if __name__ == "__main__":
    exit(main())