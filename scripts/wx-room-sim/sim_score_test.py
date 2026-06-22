import asyncio
import json
import urllib.request
import urllib.error

import websockets

BASE_HTTP = "https://mr1v1.smarteamlab.com"
BASE_WS = "wss://mr1v1.smarteamlab.com"
P0_TOKEN = "test-sim-token-p0"
P1_TOKEN = "test-sim-token-p1"
INTERNAL_API_KEY = "kingsh2012"


def http(method, path, token=None, body=None, headers=None):
    url = BASE_HTTP + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", token)
    if headers:
        for k, v in headers.items():
            req.add_header(k, v)
    try:
        with urllib.request.urlopen(req) as resp:
            return resp.status, json.loads(resp.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read())


async def main():
    status, resp = http("POST", "/api/wx/rooms", token=P0_TOKEN,
                         body={"title": "比分推送测试房", "password": "", "category": "pistol"})
    print(f"[creator] 创建房间: {status}", flush=True)
    room_id = resp["data"]["id"]
    print(f"[creator] room_id={room_id}", flush=True)

    creator_ws = await websockets.connect(f"{BASE_WS}/ws/wx/room/{room_id}?token={P0_TOKEN}")
    print("[creator] WS已连接", flush=True)

    status, resp = http("POST", f"/api/wx/rooms/{room_id}/join", token=P1_TOKEN, body={"password": ""})
    print(f"[joiner] 加入房间: {status}", flush=True)
    joiner_ws = await websockets.connect(f"{BASE_WS}/ws/wx/room/{room_id}?token={P1_TOKEN}")
    print("[joiner] WS已连接", flush=True)

    match_id = None
    server_addr = None
    # drain until matched on both sides
    async def wait_matched(ws, label):
        nonlocal match_id, server_addr
        async for raw in ws:
            msg = json.loads(raw)
            print(f"[{label}收到]: {msg}", flush=True)
            if msg.get("type") == "matched":
                match_id = msg.get("match_id")
                server_addr = msg.get("server_addr")
                return

    await creator_ws.send(json.dumps({"type": "confirm"}))
    await joiner_ws.send(json.dumps({"type": "confirm"}))

    await asyncio.gather(wait_matched(creator_ws, "creator"), wait_matched(joiner_ws, "joiner"))
    print(f"[事件] matched! match_id={match_id} server_addr={server_addr}", flush=True)

    # 现在双方WS还开着（建服后不关闭），直接调内部round-update接口模拟一次回合结束，
    # 验证score_update是否真的推送到了这两个还连着的WS
    status, resp = http("POST", "/api/wx/internal/round-update",
                         headers={"X-API-Key": INTERNAL_API_KEY},
                         body={"match_id": match_id, "score_creator": 3, "score_joiner": 1})
    print(f"[round-update] 触发: {status} {resp}", flush=True)

    async def expect_score_update(ws, label):
        try:
            raw = await asyncio.wait_for(ws.recv(), timeout=5)
            msg = json.loads(raw)
            print(f"[{label}收到score推送]: {msg}", flush=True)
            assert msg.get("type") == "score_update", f"期望score_update，收到{msg.get('type')}"
            assert msg.get("score_creator") == 3 and msg.get("score_joiner") == 1, "比分不对"
            print(f"[{label}] ✅ 比分推送验证通过", flush=True)
        except asyncio.TimeoutError:
            print(f"[{label}] ❌ 5秒内没收到score_update事件", flush=True)

    await asyncio.gather(expect_score_update(creator_ws, "creator"), expect_score_update(joiner_ws, "joiner"))

    await creator_ws.close()
    await joiner_ws.close()


asyncio.run(main())
