#!/usr/bin/env python3
"""交互式 agent demo。

pip install pytspclient openai
export OPENAI_API_KEY
python examples/demo_agent.py
"""

import asyncio
from pytspclient import TSPClient
from openai import OpenAI

GTSP_PATH = "./gtsp"  # 替换为实际路径

async def main():
    tsp = await TSPClient.from_stdio(GTSP_PATH).start()
    adapter = tsp.for_openai()
    llm = OpenAI()
    messages = [{"role": "system", "content": "我会给你任务，请用中文回复。"}]

    # 交互循环
    while True:
        content = input("You: ").strip()
        if not content: continue
        messages.append({"role": "user", "content": content})

        # 执行直到 agent 不再调用工具
        while True:
            resp = llm.chat.completions.create(model="gpt-4o-mini", messages=messages, tools=adapter.tools)
            messages.append(resp.choices[0].message)

            calls = adapter.parse_tool_calls(resp)
            if calls:
                results = await adapter.execute_tool_calls(resp)
                messages.extend(adapter.to_tool_messages(results))
            else:
                print(f"Agent: {resp.choices[0].message.content}")
                break

asyncio.run(main())