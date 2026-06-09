# PROCS.PRO 项目介绍

## 背景

这套系统由作者本人从 2020 年到 2024 年陆续独立开发，是一套完整的 CS 1.6 竞技服务端平台，设计目标为 **5v5 对战**。

---

## 三个核心项目

### 1. 服务端 — PROCS.PRO-REHLDS-LINUX-PROD（当前分支：v2）

**定位**：CS 1.6 游戏服务端本体

**引擎栈：**
- ReHLDS（替代原版 HLDS 引擎，含安全补丁）
- ReGameDLL_CS（替代原版 cs.so，提供更多 hook 接口），支持两个版本：
  - `366`：死亡后 TAB 面板显示 C4 位置（含官方 bug）
  - `546`：无该 bug（默认使用）
- Metamod-R（插件管理器）
- AMXModX **1.10**（插件脚本框架，**非 1.9**，原因见下）
- ReAPI（暴露 ReHLDS/ReGameDLL 底层接口给插件）

**为什么用 AMXModX 1.10 而非 1.9：**
数据上报格式为 JSON，需要 AMXModX 1.10 的原生 JSON 模块支持，1.9 没有该模块。

**已装防作弊/功能插件（Metamod 级）：**
- ReChecker — 客户端文件完整性校验
- Reunion — 支持非 Steam 客户端连接
- ReSemiclip — 玩家穿透控制
- WHBlocker — 透视封锁
- SNAC (SafeNameAndChat) — 名称与聊天安全过滤
- HitboxFix — 命中框修正
- ReAuthCheck — 重认证检查

**自研 AMXModX 插件（procs 开头，作者本人编写）：**
- `procs_cmps.sma`（v3.5.6.29，5574行）— 核心比赛插件，功能：
  - 比赛生命周期：刀战（g_MatchType=5）→ 上半场（1）→ 下半场（2）→ 加时赛（3/4）
  - R3 热身流程（3次重启倒计时）
  - 换边投票系统
  - 比分 HUD + TAB 计分板显示
  - 赛点提示
  - 暂停功能（强制暂停 + 投票暂停）
  - DEMO 自动/手动录制
  - 后坐力控制
  - 手雷数量限制
  - 向外暴露 native 接口：`get_match_score`、`get_match_state`、`get_match_type`、`get_match_round`、`get_unique_id`、`get_match_id`
  - 向外发布 forward 事件：`match_begin`、`match_round_end`、`match_end`
- `procs_collection.sma` — 比赛数据采集插件，依赖 `procs_cmps` 提供的比赛状态，采集比分、击中、死亡、玩家进出事件，通过 `easy_http` 异步上报到 gateway
- `procs_pdetails.sma` — 玩家信息展示 HUD、SteamID 白名单验证（禁止未授权玩家进入）、回合击中统计、自动改名、Rating 显示、GeoIP 集成
- `procs_extension.sma` — 回合最佳、屏蔽服务器参数（防 HLSW 查看）、后坐力控制（ReAPI 实现）、ex_interp 锁定、烟雾增强
- `procs_csdm_extension.sma` — 死斗模式击杀加血加甲（爆头 HP+25/AP+10，普通击杀 HP+15/AP+5）
- `procs_pmenu.sma` — 玩家菜单插件，注册 `say menu` / `say .m` 命令，调起语音菜单、暂停菜单、屏蔽菜单
- `procs_ppause.sma` — 投票暂停插件，基于第三方 TACTICAL PAUSE 改造重写
- `procs_pvoice.sma` — 搞笑语音菜单插件
- `procs_recmps.sma` — 重构中的 cmps 插件，尚未完成
- `rechecker_logging.sma` — ReChecker 日志记录
- `reapi_test.sma` — ReAPI 接口测试

**死亡模式核心插件（CSDM，第三方）：**
- `csdm_core.sma` — CSDM 核心
- `csdm_equip_manager.sma` — 装备管理
- `csdm_map_cleaner.sma` — 地图清理
- `csdm_misc.sma` — 杂项功能
- `csdm_protection.sma` — 出生保护
- `csdm_spawn_manager.sma` — 出生点管理

**参考插件（开发期研究用，功能已合并进 procs 主插件，生产环境不再单独加载）：**
- `aim_botz.sma` — 射击练习机器人
- `aim_reflex.sma` — 反应力/快速瞄准训练
- `AIPunctureTraining.sma` — AI 穿点训练
- `armor.sma` — 出生自动补甲
- `BotFeatures.sma` — Bot 功能修复（改名、接管 Bot 等）
- `cvar_checker.sma` — 检测客户端 cvar 非法值并强制修正
- `Game_pause.sma` — 管理员暂停游戏
- `ham_register_cz_bots.sma` — 为 CZ Bot 注册 Ham 钩子的兼容补丁
- `recoil_manager.sma` — 后坐力控制
- `retakes.sma` — 回防模式
- `say_pause.sma` — 玩家通过 `say !pause` / `say /pause` 触发暂停
- `smokeex.sma` — 烟雾增强（一颗烟产生多个烟雾效果）
- `Unreal_Demo_Plugin.sma` — Demo 录制增强
- `warmup.sma` — 热身模式（选武器热身）
- `zbot_menu.sma` — ZBot 控制菜单

**支持双架构（Linux + Windows）：**
同一份仓库同时包含 Linux `.so` 和 Windows `.dll`，可在两个平台运行。

**游戏模式（通过 hlds_mode 切换）：**
| 模式 | 数据采集 | 玩家验证 | 自动改名 |
|------|---------|---------|---------|
| public | off | off | off |
| match | on | on | on（稳定版插件） |
| latestmatch | on | on | on（最新版插件） |
| league | off | on | off |
| csdm | off | on | on |

**部署方式：**
Docker 容器，使用私有镜像 `rehlds-linux-basic:v2.0.2`，通过挂载 `rehlds-startup.yml` 注入配置。

---

### 2. 启动器 — PROCS.PRO-REHLDS-STARTUP（当前分支：v2）

**定位**：CS 服务器进程的启动管理器，运行在容器内，随服务器一起启动

**语言**：Go

**核心功能：**
- 读取 `rehlds-startup.yml` 配置文件，按 `hlds_mode` 渲染写入服务器所需的各类配置文件
- 从 FL 平台下载玩家数据，生成 `data.json`（SteamID 白名单）和 `users.ini`（OP 权限用户）注入服务器
- 启动/停止 HLDS 进程（含 PID 文件管理、优雅关闭、强制 Kill -9）
- CPU 亲和性绑定（可手动/自动指定核心）
- iptables 端口控制

**已弃用：**
- WebSocket 客户端（代码已注释，不再使用）

**配置文件模板系统：**
`templates/env/` 下按模式预置配置模板，启动时按 `hlds_mode` 自动渲染写入：
- `server.cfg` — 服务器基础配置
- `amxx.cfg` — AMXModX 主配置
- `game.cfg` — 游戏规则配置
- `amxmodx-plugins.ini` — 启用的 AMXModX 插件列表
- `metamod-plugins.ini` — 启用的 Metamod 插件列表
- `mystats.ini` — 数据采集相关配置

**关键配置项（rehlds-startup.yml）：**
```yaml
hlds_mode: public          # 游戏模式
hlds_port: 27015           # 服务端口
hlds_rconpwd: xxx          # RCON 密码
hlds_regamedll: 546        # ReGameDLL 版本
hlds_pingboost: 2/3        # FPS 加速模式
mystats_gateway_token: xxx # 数据网关 Token
mystats_gateway_http: http://127.0.0.1:7778  # 数据上报地址
mystats_show_round_damage: true   # 显示回合伤害统计
mystats_show_round_mvp: true      # 显示回合 MVP
mystats_recoil_mode: 0/1/2        # 后坐力控制模式
```

**与上层平台交互：**
WebSocket 客户端连接到平台后端，定期上报宿主机状态（`HostInfo` 结构体）。注：当前 v2 分支中 WebSocket 功能已在 `main.go` 中注释掉，未启用。

---

### 3. 消费端 — PROCS.PRO-REHLDS-COLLECTION-SYSTEM（当前分支：v4-mqtt）

**定位**：游戏数据采集系统，包含三个独立进程：gateway 负责接收游戏服务器上报的数据并转发到 MQTT，consumer 消费 MQTT 消息写入数据库，api 对外提供数据查询接口。

**语言**：Go

**三个独立进程：**

```
gateway   ← 接收 CS 服务器插件的 HTTP POST，写入内存 channel，转发到 MQTT
consumer  ← 订阅 MQTT，消费消息，写入数据库（PostgreSQL，GORM）
api       ← 提供数据查询 HTTP 接口
```

**数据流：**
```
CS 插件 (AMXModX)
    → HTTP POST /record (JSON)
        → gateway (内存 channel, 容量 10000)
            → MQTT (topic: {prefix}/{token})
                → consumer
                    → PostgreSQL
```

**为什么不直接 HTTP 同步上报（解决卡进程问题）：**
CS 1.6 是单线程引擎，AMXModX 插件运行在游戏主线程。若直接同步 HTTP 请求，网络超时会冻结整个服务器。
解决方案：插件只做 HTTP POST（发完即走），gateway 用 Go channel 异步解耦，完全不阻塞游戏进程。
gateway 还有重试队列，MQTT 发送失败自动重试，保障数据不丢失。

**消息格式（通用信封）：**
```json
{
  "timestamp": 1700000000,
  "token": "服务器唯一Token",
  "type": "消息类型",
  "version": "插件版本",
  "data": "JSON字符串（具体数据）"
}
```

**已定义的数据类型（mes 包）：**
| 类型 | 字段 |
|------|------|
| `ClientPutInServerMes` | 玩家进入：token、unique_id、map_name |
| `ClientDisconnectedMes` | 玩家离开：token、unique_id、map_name |
| `PlayerDamage` | 伤害事件：攻击者/受伤者 idx+authid+team、damage、weapon_id、hit_id（命中部位）、TA（友伤标记） |
| `PlayerDeath` | 死亡事件：杀手/死亡者 idx+authid+team、weapon_id、hit_id、TK（队友击杀标记） |
| `PlayerDetails` | 玩家详情：name、authid、ip、ping、loss、team、deaths、frags、uptime |

**监控：**
gateway 内置 Prometheus 指标：
- `gateway_record_requests_total` — HTTP 请求计数
- `gateway_mqtt_publish_total` — MQTT 发布计数
- `gateway_channel_length` — 当前队列长度
- `GET /metrics` 暴露指标，`GET /health` 健康检查

**API 的具体用途：**

API 服务对接一个名为 **FL** 的外部平台，FL 负责数据分析和比分计算。数据流向为双向：

- **上行**：consumer 将比赛数据写入 PostgreSQL，FL 通过 API 查询消费
- **下行**：FL 平台提供玩家数据，由 API 生成两个文件注入服务器：
  - `data.json` — AMXModX 插件加载，记录允许进入服务器的玩家 SteamID 白名单，未在列表中的玩家一律禁止进入
  - `users.ini` — AMXModX 权限配置，生成 OP 用户（服务器管理员），拥有换图、刷新、比赛开始等控制权限

**附属库：**
- `pkg/go-a2s` — Steam A2S 协议实现，可直接查询服务器在线状态（人数、地图、ping）

---

## 整体架构图

```
平台后端
    ├── WebSocket ← STARTUP 每5秒上报宿主机状态
    ├── RCON     → 比赛指令控制（换图、执行cfg、重启）
    └── API      ← COLLECTION-SYSTEM 提供数据查询
         │
         ├── STARTUP（进程管理器）
         │       ↓ 启动/写配置
         │   LINUX-PROD（CS 1.6 服务端）
         │       ↓ HTTP POST /record (JSON)
         └── COLLECTION-SYSTEM
                 gateway → MQTT → consumer → PostgreSQL
```

