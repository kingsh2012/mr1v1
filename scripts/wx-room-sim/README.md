# wx-room-sim

模拟一个"对手"测试小程序约战房间的完整交互（创建/加入/聊天/确认/踢人/建服），
不需要准备第二个真实微信账号——脚本用 `test-sim-token-p1` 这个测试token假扮对手。

## 用法

```bash
pip install websockets   # 如果还没装
python3 sim_interactive.py
```

启动后脚本会轮询 `GET /api/wx/rooms`，等真人在小程序里创建一个**不加密码**的新房间，
检测到后自动加入、连上房间WebSocket，进入交互模式：

- 真人在房间里发的聊天消息，脚本会原样回复一遍（验证聊天链路通不通）
- 真人发"准备"/"开战"/"确认"，脚本会自动发送 `confirm`（模拟对手也点了准备，
  触发双方确认 -> 建服)
- 真人发"取消"/"不打了"，脚本会发送 `cancel_confirm`
- 房主把"对手"踢出（`kick_joiner`）、关闭房间、比赛结束等事件都会在终端打印出来

单轮结束后（被踢/房间关闭/比赛结束）脚本会自动回到等待状态，继续监听下一个新房间，
可以反复创建房间连续测试，不需要重启脚本。

## 依赖的测试账号

`test-sim-token-p1` 对应的测试用户需要已存在于目标环境的 `wx_sessions`/`wx_users`表
（建库时用的是 `mr1v1-miniprogram-backend` 连的那个 PostgreSQL）。如果环境换了或者
token失效，去插一条新的测试session/user，或者把脚本里的 `BOT_TOKEN` 换成别的有效token。

## 配置

脚本顶部的 `BASE_HTTP`/`BASE_WS`/`BOT_TOKEN` 按需改成对应环境的域名和测试token。
默认指向生产域名 `mr1v1.smarteamlab.com`。
