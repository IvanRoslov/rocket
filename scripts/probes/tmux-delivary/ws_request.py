#!/usr/bin/env python3
import asyncio
import json
import sys

import websockets


async def main():
    socket_path, method, raw_params = sys.argv[1:4]
    params = json.loads(raw_params)
    async with websockets.unix_connect(
        path=socket_path, max_size=64 * 1024 * 1024
    ) as ws:

        async def request(request_id, request_method, request_params):
            await ws.send(
                json.dumps(
                    {
                        "method": request_method,
                        "id": request_id,
                        "params": request_params,
                    }
                )
            )
            while True:
                message = json.loads(await ws.recv())
                if message.get("id") == request_id:
                    if "error" in message:
                        raise RuntimeError(message["error"])
                    return message.get("result")

        await request(
            0,
            "initialize",
            {
                "clientInfo": {
                    "name": "rocket_recon_reader",
                    "title": "Rocket isolated recon reader",
                    "version": "1.0",
                },
                "capabilities": {"experimentalApi": True},
            },
        )
        await ws.send(json.dumps({"method": "initialized", "params": {}}))
        result = await request(1, method, params)
        print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    asyncio.run(main())
