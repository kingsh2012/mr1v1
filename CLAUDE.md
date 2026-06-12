# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

本仓库是 **CS 1.6 1v1 对战平台服务端**，当前开发与部署都在 `server/` 目录下（已部署完成的 ReHLDS 环境）。

仓库里另外三个历史目录——`PROCS.PRO-REHLDS-LINUX-PROD`（5v5服务端+插件）、`PROCS.PRO-REHLDS-STARTUP`（已弃用的Go启动器）、`PROCS.PRO-REHLDS-COLLECTION-SYSTEM`（数据采集系统）——来自作者2020-2024年开发的 PROCS.PRO 5v5竞技平台，背景见 [PROCS.PRO_PROJECT_OVERVIEW.md](./PROCS.PRO_PROJECT_OVERVIEW.md)，阶段目标见 [ROADMAP.md](./ROADMAP.md)。

**这些历史目录仅作架构/逻辑参考，1v1平台不需要照搬PROCS.PRO的设计**：赛制、插件结构、启动方式等1v1有自己的规则和实现（如下文 `mr1v1_match.sma`），遇到与PROCS.PRO参考资料不一致的地方，以1v1当前实现和讨论结论为准。其中 `PROCS.PRO-REHLDS-COLLECTION-SYSTEM`（gateway/consumer/api）仍在实际使用，1v1插件通过它上报比赛数据。

## 当前核心：1v1比赛插件 mr1v1_match.sma

位置：`server/cstrike/addons/amxmodx/scripting/mr1v1_match.sma`（编译产物 `mr1v1_match.amxx`，已在 `configs/plugins.ini` 中注册）。

赛制规则（详见文件头注释）：
- 最多51回合（10手枪 / 28步枪 / 13狙击），先达26胜（51的过半数）提前结束，无加时
- 第1回合随机分边，之后每回合结束无论胜负T/CT自动互换
- 每回合开局强制清空背包并按当前阶段+玩家偏好（`.1`/`.2`/`.3`/`.4`/`.guns`）重发武器/满弹/护甲
- 比赛开始/每回合结束/比赛结束 通过 HTTP POST 上报到 gateway（配置见 `configs/mr1v1.ini`）

赛制设计参照"完美天梯单挑"模式（非PROCS.PRO的5v5赛制），已用实战数据验证过回合/阶段分布。

## 数据采集插件 mr1v1_telemetry.sma

位置：`server/cstrike/addons/amxmodx/scripting/mr1v1_telemetry.sma`（编译产物 `mr1v1_telemetry.amxx`，已在 `configs/plugins.ini` 中注册）。

与 `mr1v1_match.sma` 解耦：通过其广播的自定义forward `mr1v1_match_start(match_id, id0, id1)` / `mr1v1_match_end(...)` 获知比赛上下文，期间收集命中/伤害事件，按固定周期（默认1秒）批量HTTP上报（`mr1v1_combat_batch`，地址复用 `configs/mr1v1.ini` 的 `mr1v1_gateway_http`）。后续计划加入开枪、玩家移动轨迹等事件，复用同一套批量上报机制。

## 构建与运行

### AMXModX 插件编译

插件源码在 `server/cstrike/addons/amxmodx/scripting/`，扩展名 `.sma`。
编译需要 AMXModX **1.10** 编译器（`amxxpc`），输出 `.amxx` 放到 `../plugins/` 目录。

```bash
# Linux 编译示例：-o 后接的输出路径不能含 ".."（amxxpc 1.10.0.5476 在打包.amxx时
# 路径里有".."会算错输出文件名导致"Could not locate output file"，详见下方注意事项），
# 所以先在 scripting/ 目录内编译，再移动到 ../plugins/
./amxxpc mr1v1_match.sma -omr1v1_match
mv -f mr1v1_match.amxx ../plugins/mr1v1_match.amxx
```

**`-o` 参数注意事项：**`-o<name>` 是"set base name of output file"，必须与 `-o` **不留空格**直接拼接，且 `<name>` 不能含路径分隔符跳转（如 `../`），否则amxxpc打包`.amxx`的最后一步会输出"Could not locate output file"且不生成`.amxx`（pawncc本身可能已编译成功但不会被打包）。

### 消费端（PROCS.PRO-REHLDS-COLLECTION-SYSTEM）

```bash
cd PROCS.PRO-REHLDS-COLLECTION-SYSTEM

# 编译全部（api / gateway / consumer）
make build

# 单独编译
make gateway
make consumer
make api

# 格式化
make fmt

# 运行（各服务独立进程，配置文件在 cmd/<服务>/<服务>.yml）
./bin/gateway  -conf cmd/gateway/gateway.yml
./bin/consumer -conf cmd/consumer/consumer.yml
./bin/api      -conf cmd/api/api.yml

# 构建 Docker 镜像
make images
```

## 服务端安装与运行（server/ 目录）

`/data/rehlds/server/` 已部署完成的 ReHLDS 环境：
- ReHLDS 3.15.0.896（build 4419，2026-05-18）
- ReGameDLL_CS 5.30.0.814
- Metamod-R 1.3.0.149
- AMXModX 1.10.0.5476 + ReAPI 5.29.0.358

**启动方式：**
```bash
cd /data/rehlds/server
./start.sh            # 基本启动，含 -insecure +sv_lan 1
```

**配置维护：**
`server.cfg`、`amxx.cfg`、`metamod-plugins.ini` 等配置文件直接在 `server/` 目录下手动维护，不经过任何配置渲染/注入工具。临时调试可用 `server/set_cvar.sh <cvar> <value>` 修改 `server.cfg` 中已存在的cvar值（保留行尾注释）。

**关键 libsteam_api.so 说明：**
SteamCMD 下载的 HLDS (app 90) 自带的 `libsteam_api.so` 版本过旧（缺少 `SteamGameServer_Init`、`SteamGameServer`、`SteamApps` 等符号），无法与 ReHLDS 3.x 配合使用。
需使用 `PROCS.PRO-REHLDS-LINUX-PROD/libsteam_api.so`（新版 Steam SDK），已复制到 server/ 目录。

**GameConfig CRC 警告可忽略：**
AMXModX 日志中的 "GameConfig CRC mismatch" 是因为 AMXModX 不认识最新版 ReHLDS/ReGameDLL 的 CRC，属于已知警告，不影响基本运行（服务器正常启动和监听 27015）。

## 架构关键点

### 数据流
```
AMXModX 插件 → HTTP POST /record (JSON) → gateway (channel 10000)
    → MQTT (topic: {prefix}/{match_id}) → consumer → PostgreSQL
```
gateway 与游戏进程完全异步解耦，AMXModX 插件发完即走，不等响应，避免阻塞单线程游戏引擎。

### 消息格式（AMXX侧已实现，Go侧 pkg/mes/mes.go 待适配）
所有上报消息使用统一信封：
```json
{ "timestamp": 1700000000, "match_id": "比赛唯一ID", "type": "消息类型", "version": "插件版本", "data": { /* 嵌套JSON对象 */ } }
```
`match_id`取代了原来的服务器级`token`——1v1服务器打完即销毁、一台对应一场比赛，`match_id`本身已是唯一标识，gateway可据此将同一场比赛的事件分流到MQTT topic `{prefix}/{match_id}`。`data`为嵌套JSON对象（非转义字符串）。

上报分两个插件：
- `mr1v1_match.sma`：`mr1v1_match_start` / `mr1v1_round_end` / `mr1v1_match_end`
- `mr1v1_telemetry.sma`：`mr1v1_combat_batch`（命中/伤害事件，批量上报；后续会加开枪/移动轨迹等批量事件）

字段详情见 [MR1V1_EVENTS.md](./MR1V1_EVENTS.md)。Go侧（`pkg/mes`、consumer）尚未适配新信封和新事件类型，目前这些消息到达gateway后仍按旧逻辑处理/丢弃。

### AMXModX 插件版本
**必须使用 AMXModX 1.10**（非 1.9），因为数据上报使用 JSON 格式，依赖 1.10 的原生 JSON 模块。

### 插件实现技术路线
插件优先使用 **ReAPI**（5.29.0.358，include 已安装在 `server/cstrike/addons/amxmodx/scripting/include/`）实现游戏事件 hook、读写实体/玩家数据、调用游戏内部函数等。只有 ReAPI 完全无法覆盖的功能才退回 fakemeta/engine 等旧方式。

### PROCS.PRO参考价值
`PROCS.PRO-REHLDS-LINUX-PROD/cstrike/addons/amxmodx/scripting/procs_cmps.sma` 是PROCS.PRO 5v5平台的核心比赛插件（暴露 `get_match_score` 等native和 `match_begin`/`match_round_end`/`match_end` 等forward），可作为"比赛流程类插件怎么写"的逻辑参考，但**不直接复用**——其5v5赛制、房间逻辑与1v1完全不同。
