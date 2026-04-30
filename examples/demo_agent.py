#!/usr/bin/env python3
"""交互式 agent demo。

pip install pytspclient openai
export OPENAI_API_KEY
python examples/demo_agent.py
"""

import asyncio
from pytspclient import TSPClient
from openai import OpenAI

GTSP_PATH = "./gtsp-darwin-arm64"  # 替换为实际路径

async def main():
    tsp = await TSPClient.from_stdio(GTSP_PATH).start()
    adapter = tsp.for_openai()
    llm = OpenAI()
    messages = [{"role": "system", "content": "You are a helpful assistant."}]

    # 循环获取用户任务
    while True:
        content = input("You: ").strip() # 获取用户新输入的任务
        if not content: continue
        messages.append({"role": "user", "content": content})

        # ----- 以下10代码，即可实现 agent 自主连续行动，直到完成用户任务 -----
        while True:
            resp = llm.chat.completions.create(model="azure_openai/gpt-5.4", messages=messages, tools=adapter.tools)
            messages.append(resp.choices[0].message)

            calls = adapter.parse_tool_calls(resp)
            if calls:
                results = await adapter.execute_tool_calls(resp)
                messages.extend(adapter.to_tool_messages(results))
            else:
                print(f"Agent: {resp.choices[0].message.content}\n")
                break

asyncio.run(main())