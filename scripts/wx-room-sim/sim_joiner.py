import asyncio
import json
import urllib.request
import urllib.error

import websockets

BASE_HTTP = "https://mr1v1.smarteamlab.com"
BASE_WS = "wss://mr1v1.smarteamlab.com"
BOT_TOKEN = "test-sim-token-p1"


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


def list_rooms():
    status, body = http("GET", "/api/wx/rooms")
    if status != 200:
        return []
    return body.get("data", [])


async def wait_for_new_room(known_ids, timeout=60):
    print(f"[joiner] 等待新房间出现...", flush=True)
    elapsed = 0
    while elapsed < timeout:
        rooms = list_rooms()
        for r in rooms:
            if r["id"] not in known_ids and r["status"] == "waiting" and not r["locked"]:
                return r
        await asyncio.sleep(1)
        elapsed += 1
    return None


async def main():
    known_ids = {r["id"] for r in list_rooms()}
    room = await wait_for_new_room(known_ids)
    if room is None:
        print("[joiner] 等待超时，退出。", flush=True)
        return
    room_id = room["id"]
    print(f"[joiner] 发现新房间: {room['title']} (id={room_id[:8]}...)", flush=True)

    status, body = http("POST", f"/api/wx/rooms/{room_id}/join", token=BOT_TOKEN, body={"password": ""})
    if status != 200:
        print(f"[joiner] 加入失败: {status} {body}", flush=True)
        return
    print("[joiner] 已加入房间，开始连接WebSocket...", flush=True)

    ws_url = f"{BASE_WS}/ws/wx/room/{room_id}?token={BOT_TOKEN}"
    async with websockets.connect(ws_url) as ws:
        print("[joiner] WebSocket已连接，立即发送confirm", flush=True)
        await asyncio.sleep(1)
        await ws.send(json.dumps({"type": "confirm"}))
        print("[joiner动作]: 发送confirm（准备）", flush=True)
        async for raw in ws:
            try:
                msg = json.loads(raw)
            except json.JSONDecodeError:
                continue
            print(f"[joiner收到]: {msg}", flush=True)
            mtype = msg.get("type")
            if mtype == "matched":
                print(f"[事件] 🎉 比赛已建服: {msg.get('server_addr')} match_id={msg.get('match_id')}", flush=True)
                return
            elif mtype in ("room_closed", "match_ended", "kicked"):
                print(f"[事件] {mtype}: {msg.get('message')}", flush=True)
                return
            elif mtype == "error":
                print(f"[事件] 错误: {msg.get('message')}", flush=True)


asyncio.run(main())
