#!/usr/bin/env python3
"""
Concurrency test to verify unique session IDs.
This test creates multiple sessions simultaneously to check for race conditions.
"""

import subprocess
import json
import os
import time
import glob

PROJECT_DIR = "/Volumes/PDATA/GitDB/GTAgentHands"
LOG_DIR = os.path.join(PROJECT_DIR, "logs")
BINARY = os.path.join(PROJECT_DIR, "gtsp")

def clear_session_logs():
    """Remove session log files but keep the global log."""
    if os.path.exists(LOG_DIR):
        for f in glob.glob(os.path.join(LOG_DIR, "gt-agent-*.log")):
            os.remove(f)

def test_concurrent_sessions():
    """Test that concurrent sessions get unique IDs and separate log files."""
    print("=" * 70)
    print("CONCURRENT SESSION ID UNIQUENESS TEST")
    print("=" * 70)
    
    clear_session_logs()
    
    # Create multiple sessions simultaneously
    procs = []
    num_sessions = 5
    
    print(f"\n[Creating {num_sessions} sessions simultaneously...]")
    
    # Start all sessions at almost the same time
    for i in range(num_sessions):
        proc = subprocess.Popen(
            [BINARY, "--mode", "stdio"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            cwd=PROJECT_DIR
        )
        procs.append(proc)
    
    # Send initialize to all at once
    for i, proc in enumerate(procs):
        init = json.dumps({"id": str(i), "method": "initialize", "input": {"protocolVersion": "0.3"}})
        proc.stdin.write((init + "\n").encode())
        proc.stdin.flush()
    
    time.sleep(0.5)
    
    # Check log files
    session_logs = glob.glob(os.path.join(LOG_DIR, "gt-agent-*.log"))
    
    print(f"\n[Result] Created {num_sessions} sessions")
    print(f"[Result] Found {len(session_logs)} log files:")
    
    for log_file in session_logs:
        basename = os.path.basename(log_file)
        size = os.path.getsize(log_file)
        # Extract session ID from filename
        session_id = basename.replace("gt-agent-", "").replace(".log", "")
        
        # Read first few lines to check content
        with open(log_file, 'r') as f:
            lines = f.readlines()
            first_line = lines[0] if lines else ""
        
        print(f"  - {basename} ({size} bytes)")
        print(f"    Session ID: {session_id}")
        print(f"    First line: {first_line[:80]}...")
    
    # Clean up
    for proc in procs:
        try:
            shutdown = json.dumps({"id": "99", "method": "shutdown", "input": {}})
            proc.stdin.write((shutdown + "\n").encode())
            proc.stdin.flush()
        except:
            pass
        finally:
            proc.terminate()
    
    # Verify uniqueness
    if len(session_logs) == num_sessions:
        print(f"\n✅ PASS: All {num_sessions} sessions have unique log files")
        return True
    else:
        print(f"\n❌ FAIL: Expected {num_sessions} log files, found {len(session_logs)}")
        print("This indicates a race condition where concurrent sessions share the same ID")
        return False

def test_session_id_format():
    """Test that session IDs have the correct timestamp format."""
    print("\n" + "=" * 70)
    print("SESSION ID FORMAT TEST")
    print("=" * 70)
    
    import re
    # Expected format: YYYYMMDD-HHMMSS.mmm
    pattern = re.compile(r'^\d{8}-\d{6}\.\d{3}$')
    
    session_logs = glob.glob(os.path.join(LOG_DIR, "gt-agent-*.log"))
    
    print(f"\n[Checking {len(session_logs)} session IDs]")
    
    all_valid = True
    for log_file in session_logs:
        basename = os.path.basename(log_file)
        session_id = basename.replace("gt-agent-", "").replace(".log", "")
        
        if pattern.match(session_id):
            print(f"  ✓ {session_id}")
        else:
            print(f"  ✗ {session_id} (invalid format)")
            all_valid = False
    
    if all_valid and len(session_logs) > 0:
        print("\n✅ PASS: All session IDs have correct format")
        return True
    elif len(session_logs) == 0:
        print("\n❌ FAIL: No session logs found")
        return False
    else:
        print("\n❌ FAIL: Some session IDs have invalid format")
        return False

def main():
    print("\n" + "=" * 70)
    print("SESSION ID UNIQUENESS AND FORMAT TEST SUITE")
    print("=" * 70)
    
    results = []
    
    results.append(("Concurrent sessions unique", test_concurrent_sessions()))
    results.append(("Session ID format", test_session_id_format()))
    
    print("\n" + "=" * 70)
    print("TEST SUMMARY")
    print("=" * 70)
    
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