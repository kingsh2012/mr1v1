# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 仓库目录结构

```
mr1v1-rehlds/           ← CS 1.6 ReHLDS 游戏服务端（已部署完成的运行环境）
mr1v1-server/           ← Go 后端服务 module（backend/consumer/agent/miniprogram-backend）
mr1v1-manager-frontend/ ← React 管理控制台（打包后 embed 进 mr1v1-server/internal/backend）
mr1v1-miniprogram/      ← 微信小程序（独立 git 仓库，.gitignore 排除）
```

历史参考目录（仅作架构/逻辑参考，不再扩展）：
- `PROCS.PRO-REHLDS-LINUX-PROD`：5v5服务端+插件
- `PROCS.PRO-REHLDS-STARTUP`：已弃用的Go启动器
- `PROCS.PRO-REHLDS-COLLECTION-SYSTEM`：5v5数据采集系统

背景见 [PROCS.PRO_PROJECT_OVERVIEW.md](./PROCS.PRO_PROJECT_OVERVIEW.md)，阶段目标见 [ROADMAP.md](./ROADMAP.md)。

**历史目录仅作参考，1v1平台不照搬PROCS.PRO的设计**：赛制、插件结构、启动方式等以1v1当前实现和讨论结论为准。

---

## mr1v1-server Go module

### 编译资源限制

执行 `go build` / `go test` / `go vet` 等命令时，必须限制为单核，避免把宿主机 CPU 跑满：

```bash
GOMAXPROCS=1 go build -p=1 ./...
```

**gin 版本 v1.12.0（最新）**：该版本起 gin 核心包直接依赖了 `github.com/quic-go/quic-go`（含 http3，TLS握手/拥塞控制协议栈）和 `go.mongodb.org/mongo-driver/v2/bson`，项目本身不需要 HTTP/3 或 MongoDB，但只要 import gin 就会一起编译，单核冷编译耗时约3-4分钟、内存峰值约2GB（已在3.8GB内存的VPS上实测通过，全程无swap）。如果在更小内存的机器上编译此项目，要先确认资源是否足够，或考虑临时降级到 v1.9.1（依赖干净、编译只需十几秒）。升级 gin 前建议用 `go mod why github.com/quic-go/quic-go` 检查新版本是否又引入了类似的重依赖。

### cmd 目录

```
mr1v1-server/cmd/
  manager-backend/     ← 管理控制台后端（API /api/manager/...）
  miniprogram-backend/ ← 微信小程序后端（API /api/wx/...）
  consumer/            ← MQTT 消费者，写入 PostgreSQL
  agent/               ← 部署在游戏服务器，调度 Docker 容器 + 内嵌 gateway
```

**路由前缀：**
- `/api/manager/` → manager-backend（对应 mr1v1-manager-frontend）
- `/api/wx/` + `/ws/wx/` → miniprogram-backend（对应 mr1v1-miniprogram）

**数据库迁移职责：**
- `manager-backend` 启动时建表：`mr1v1_agent`、`mr1v1_match`、`mr1v1_rehlds_config`、`mr1v1_operation_log`
- `consumer` 启动时建表：`mr1v1_match_start`、`mr1v1_round_end`、`mr1v1_match_end`、`mr1v1_combat_event` 等遥测表
- `miniprogram-backend` 启动时建表：`wx_users`、`wx_sessions`、`rooms`、`legacy_players`

### 数据流

```
AMXModX 插件 → HTTP POST /record → agent 内嵌 gateway（内存队列）
    → MQTT（topic: mr1v1/{match_id}） → consumer → PostgreSQL
```

agent 内嵌了 `internal/gateway` 模块，游戏插件直接上报到 agent 的 HTTP 端口（`network_mode: host`），完全异步解耦，不阻塞单线程游戏引擎。独立的 `cmd/gateway` 已删除。

### 消息格式

所有上报消息统一信封：
```json
{ "timestamp": 1700000000, "match_id": "比赛唯一ID", "type": "消息类型", "version": "插件版本", "data": { /* 嵌套JSON对象 */ } }
```

上报分两个插件：
- `mr1v1_match.sma`：`mr1v1_match_start` / `mr1v1_round_end` / `mr1v1_match_end`
- `mr1v1_telemetry.sma`：`mr1v1_combat_batch`（命中/伤害事件批量上报）

字段详情见 [MR1V1_EVENTS.md](./MR1V1_EVENTS.md)。

---

## mr1v1-rehlds 游戏服务端

`/data/rehlds/mr1v1-rehlds/` 已部署完成的 ReHLDS 环境：
- ReHLDS 3.15.0.896（build 4419，2026-05-18）
- ReGameDLL_CS 5.30.0.814
- Metamod-R 1.3.0.149
- AMXModX 1.10.0.5476 + ReAPI 5.29.0.358

**启动：**
```bash
cd /data/rehlds/mr1v1-rehlds
./start.sh            # 含 -insecure +sv_lan 1
```

**配置维护：**
`server.cfg`、`amxx.cfg`、`metamod-plugins.ini` 等直接手动维护，不经过任何渲染工具。临时调试可用 `set_cvar.sh <cvar> <value>` 修改 `server.cfg` 中已有的 cvar（保留行尾注释）。

**关键 libsteam_api.so：**
SteamCMD 下载的 HLDS (app 90) 自带版本过旧，需使用 `PROCS.PRO-REHLDS-LINUX-PROD/libsteam_api.so`（新版 Steam SDK），已复制到 mr1v1-rehlds/ 目录。

**GameConfig CRC 警告可忽略：**
AMXModX 日志中的 "GameConfig CRC mismatch" 是已知警告，不影响正常运行。

---

## 1v1比赛插件 mr1v1_match.sma

位置：`mr1v1-rehlds/cstrike/addons/amxmodx/scripting/mr1v1_match.sma`

赛制规则：
- 最多51回合（10手枪 / 28步枪 / 13狙击），先达26胜提前结束，无加时
- 第1回合随机分边，之后每回合结束T/CT自动互换
- 每回合开局强制清空背包并按阶段+玩家偏好（`.1`/`.2`/`.3`/`.4`/`.guns`）重发武器/满弹/护甲
- 比赛开始/每回合结束/比赛结束通过 HTTP POST 上报到 gateway（配置见 `configs/mr1v1.ini`）

赛制参照"完美天梯单挑"模式，已用实战数据验证。

## 数据采集插件 mr1v1_telemetry.sma

位置：`mr1v1-rehlds/cstrike/addons/amxmodx/scripting/mr1v1_telemetry.sma`

与 `mr1v1_match.sma` 解耦，通过其 forward `mr1v1_match_start` / `mr1v1_match_end` 获知比赛上下文，期间收集命中/伤害事件，按周期（默认1秒）批量上报 `mr1v1_combat_batch`。

## AMXModX 插件编译

插件源码在 `mr1v1-rehlds/cstrike/addons/amxmodx/scripting/`，需要 AMXModX **1.10** 编译器。

```bash
# 在 scripting/ 目录内编译再 mv（-o 不能含 "../"，否则打包失败）
./amxxpc mr1v1_match.sma -omr1v1_match
mv -f mr1v1_match.amxx ../plugins/mr1v1_match.amxx
```

**`-o` 注意事项：** 必须与 `-o` 不留空格直接拼接，且不能含 `../`，否则 amxxpc 打包 `.amxx` 时输出 "Could not locate output file"。

## 插件实现技术路线

优先使用 **ReAPI**（5.29.0.358，include 已安装在 `mr1v1-rehlds/cstrike/addons/amxmodx/scripting/include/`），只有 ReAPI 完全无法覆盖的功能才退回 fakemeta/engine 等旧方式。

**必须使用 AMXModX 1.10**（非 1.9），数据上报依赖 1.10 的原生 JSON 模块。

## PROCS.PRO 参考价值

`PROCS.PRO-REHLDS-LINUX-PROD/cstrike/addons/amxmodx/scripting/procs_cmps.sma` 是5v5平台的核心比赛插件，可作为"比赛流程类插件怎么写"的逻辑参考，但**不直接复用**——5v5赛制与1v1完全不同。
