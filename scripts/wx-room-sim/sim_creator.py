import asyncio
import json
import sys
import urllib.request
import urllib.error

import websockets

BASE_HTTP = "https://mr1v1.smarteamlab.com"
BASE_WS = "wss://mr1v1.smarteamlab.com"
BOT_TOKEN = "test-sim-token-p0"


def http(method, path, token=None, body=None):
    url = BASE_HTTP + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", token)
    try:
        with urllib.request.urlopen(req) as resp:
            return resp.status, json.loads(resp.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read())


async def main():
    category = sys.argv[1] if len(sys.argv) > 1 else "pistol"
    map_name = sys.argv[2] if len(sys.argv) > 2 else ""

    body = {"title": "全流程测试房间", "password": "", "category": category}
    if map_name:
        body["map_name"] = map_name
    status, resp = http("POST", "/api/wx/rooms", token=BOT_TOKEN, body=body)
    print(f"[creator] 创建房间: {status} {resp}", flush=True)
    if status != 200:
        return
    room_id = resp["data"]["id"]
    print(f"[creator] room_id={room_id}", flush=True)

    ws_url = f"{BASE_WS}/ws/wx/room/{room_id}?token={BOT_TOKEN}"
    async with websockets.connect(ws_url) as ws:
        print("[creator] WebSocket已连接", flush=True)
        confirmed_sent = False
        async for raw in ws:
            try:
                msg = json.loads(raw)
            except json.JSONDecodeError:
                continue
            mtype = msg.get("type")
            print(f"[creator收到]: {msg}", flush=True)

            if mtype == "player_joined":
                if not confirmed_sent:
                    confirmed_sent = True
                    await asyncio.sleep(1)
                    await ws.send(json.dumps({"type": "confirm"}))
                    print("[creator动作]: 发送confirm（准备）", flush=True)
            elif mtype == "matched":
                print(f"[事件] 🎉 比赛已建服: {msg.get('server_addr')} match_id={msg.get('match_id')}", flush=True)
                print(json.dumps({"room_id": room_id, "match_id": msg.get("match_id"), "server_addr": msg.get("server_addr")}))
                return
            elif mtype in ("room_closed", "match_ended"):
                print(f"[事件] {mtype}: {msg.get('message')}", flush=True)
                return
            elif mtype == "error":
                print(f"[事件] 错误: {msg.get('message')}", flush=True)


asyncio.run(main())
