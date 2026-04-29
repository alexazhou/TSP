import asyncio
import json
import logging
import sys
import os

# Ensure the parent directory is in the path so we can import pytspclient
sys.path.insert(0, os.path.abspath(os.path.dirname(__file__)))

from pytspclient import TSPClient, TSPEvent

logging.basicConfig(level=logging.INFO)

async def main():
    # Provide the path to the gtsp binary we just built
    # Use unbuffered I/O and pipe mode if needed, but our gtsp defaults to stdio
    command = ["../gtsp", "--mode", "stdio", "--workdir-root", "..", "--allow-read", "..", "--allow-write", ".."]
    print(f"Starting TSPClient with command: {' '.join(command)}")
    
    client = TSPClient(command)
    
    def on_event(event: TSPEvent):
        print(f"[EVENT] {event.event}: {event.data}")
        
    client.add_event_handler(on_event)
    
    await client.connect()
    
    try:
        # 1. Initialize
        print("\n--- Sending Initialize ---")
        init_result = await client.initialize()
        print(f"Server Name: {init_result.get('serverInfo', {}).get('name')}")
        print(f"Workdir: {init_result.get('workdir')}")
        print(f"Available Tools: {len(init_result.get('capabilities', {}).get('tools', []))} tools")
        
        # 2. Test list_dir
        print("\n--- Testing list_dir ---")
        dir_result = await client.tool("list_dir", {"dir_path": ".", "depth": 1})
        items = dir_result.get("items", [])
        print(f"Found {len(items)} items in parent directory.")
        for item in items[:5]: # Show first 5
            print(f"  - {item['name']} ({'dir' if item['is_dir'] else 'file'})")
            
        # 3. Test execute_bash
        print("\n--- Testing execute_bash ---")
        bash_result = await client.tool("execute_bash", {"command": "echo 'Hello from TSPClient!' && date"})
        print(f"Bash Exit Code: {bash_result.get('exit_code')}")
        print(f"Bash Stdout:\n{bash_result.get('stdout')}")
        
    except Exception as e:
        print(f"Error during test: {e}")
    finally:
        print("\n--- Shutting Down ---")
        await client.shutdown()
        print("Done.")

if __name__ == "__main__":
    asyncio.run(main())
