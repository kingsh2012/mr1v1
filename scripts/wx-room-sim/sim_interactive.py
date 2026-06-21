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


async def wait_for_new_room(known_ids, timeout=3600):
    print(f"[bot] 等待新房间出现（已知房间: {known_ids or '无'}）...", flush=True)
    elapsed = 0
    while elapsed < timeout:
        rooms = list_rooms()
        for r in rooms:
            if r["id"] not in known_ids and r["status"] == "waiting":
                return r
        await asyncio.sleep(2)
        elapsed += 2
    return None


async def run_bot(room):
    room_id = room["id"]
    print(f"[bot] 发现新房间: {room['title']} (id={room_id[:8]}..., locked={room['locked']})", flush=True)

    if room["locked"]:
        print("[bot] 房间加了密码，我猜不到密码，跳过加入。如果不想加密码，重新建一个空密码的房间。", flush=True)
        return

    status, body = http("POST", f"/api/wx/rooms/{room_id}/join", token=BOT_TOKEN, body={"password": ""})
    if status != 200:
        print(f"[bot] 加入失败: {status} {body}", flush=True)
        return
    print("[bot] 已加入房间，开始连接WebSocket...", flush=True)

    ws_url = f"{BASE_WS}/ws/wx/room/{room_id}?token={BOT_TOKEN}"
    async with websockets.connect(ws_url) as ws:
        print("[bot] WebSocket已连接，进入交互模式（聊天原样回复，发'准备'会自动确认）", flush=True)
        async for raw in ws:
            try:
                msg = json.loads(raw)
            except json.JSONDecodeError:
                continue
            mtype = msg.get("type")

            if mtype == "chat" and msg.get("role") == "creator":
                content = msg.get("content", "")
                print(f"[creator说]: {content}", flush=True)
                # 原样回复
                await ws.send(json.dumps({"type": "chat", "content": content}))
                print(f"[bot回复]: {content}", flush=True)
                # 关键词触发确认/取消
                if content.strip() in ("准备", "开战", "确认"):
                    await ws.send(json.dumps({"type": "confirm"}))
                    print("[bot动作]: 发送confirm（准备）", flush=True)
                elif content.strip() in ("取消", "不打了"):
                    await ws.send(json.dumps({"type": "cancel_confirm"}))
                    print("[bot动作]: 发送cancel_confirm（取消准备）", flush=True)

            elif mtype == "confirmed" and msg.get("role") == "creator":
                print("[事件] 房主已确认/开战", flush=True)
            elif mtype == "cancelled" and msg.get("role") == "creator":
                print("[事件] 房主取消了确认", flush=True)
            elif mtype == "matched":
                print(f"[事件] 🎉 比赛已建服: {msg.get('server_addr')}", flush=True)
            elif mtype == "kicked":
                print(f"[事件] 被房主请出房间: {msg.get('message')}", flush=True)
                break
            elif mtype == "room_closed":
                print(f"[事件] 房间已关闭: {msg.get('message')}", flush=True)
                break
            elif mtype == "match_ended":
                print(f"[事件] 比赛已结束: {msg.get('message')}", flush=True)
                break
            elif mtype == "player_left":
                print(f"[事件] {msg.get('name')} 离开了房间", flush=True)
            elif mtype == "error":
                print(f"[事件] 错误: {msg.get('message')}", flush=True)


async def main():
    known_ids = {r["id"] for r in list_rooms()}
    while True:
        room = await wait_for_new_room(known_ids)
        if room is None:
            print("[bot] 等待超时，退出。", flush=True)
            return
        known_ids.add(room["id"])
        await run_bot(room)
        print("[bot] 本轮结束，继续等待下一个新房间...", flush=True)

asyncio.run(main())
