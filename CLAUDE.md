# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

本仓库是 **CS 1.6 1v1 对战平台服务端**，以 PROCS.PRO（作者 2020-2024 年开发的 5v5 竞技平台）为架构参考，重新立项开发。详细背景见 [PROCS.PRO_PROJECT_OVERVIEW.md](./PROCS.PRO_PROJECT_OVERVIEW.md)，阶段目标见 [ROADMAP.md](./ROADMAP.md)。

三个子项目：
- **服务端** `PROCS.PRO-REHLDS-LINUX-PROD`（分支 v2）：CS 1.6 游戏服务器，含 AMXModX 插件
- **启动器** `PROCS.PRO-REHLDS-STARTUP`（分支 v2）：Go，进程管理 + 配置渲染注入
- **消费端** `PROCS.PRO-REHLDS-COLLECTION-SYSTEM`（分支 v4-mqtt）：Go，数据采集 gateway → MQTT → consumer → PostgreSQL

## 构建与运行

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

### 启动器（PROCS.PRO-REHLDS-STARTUP）

```bash
cd PROCS.PRO-REHLDS-STARTUP

# 直接运行（配置文件默认 rehlds-startup.yml）
go run main.go -conf rehlds-startup.yml

# 容器模式启动
go run main.go -conf rehlds-startup.yml -docker

# 初始化服务端文件（首次部署时执行）
go run main.go -conf rehlds-startup.yml -init

# 调试模式
go run main.go -conf rehlds-startup.yml -debug

# 运行测试
go test ./...
go test ./hlds/...
go test ./config/...
```

### AMXModX 插件（服务端）

插件源码在 `PROCS.PRO-REHLDS-LINUX-PROD/cstrike/addons/amxmodx/scripting/`，扩展名 `.sma`。
编译需要 AMXModX **1.10** 编译器（`amxxpc`），输出 `.amxx` 放到 `../plugins/` 目录。

```bash
# Linux 编译示例
./amxxpc procs_cmps.sma -o ../plugins/procs_cmps.amxx
```

## 架构关键点

### 数据流
```
AMXModX 插件 → HTTP POST /record (JSON) → gateway (channel 10000)
    → MQTT (topic: {prefix}/{token}) → consumer → PostgreSQL
```
gateway 与游戏进程完全异步解耦，AMXModX 插件发完即走，不等响应，避免阻塞单线程游戏引擎。

### 消息格式（pkg/mes/mes.go）
所有上报消息使用统一信封：
```json
{ "timestamp": 1700000000, "token": "服务器Token", "type": "消息类型", "version": "插件版本", "data": "JSON字符串" }
```
`data` 字段为具体结构体序列化后的 JSON 字符串（`PlayerDamage` / `PlayerDeath` / `PlayerDetails` 等）。

### 启动器配置渲染
`rehlds-startup.yml` 是服务器的唯一配置入口。启动器读取后，按 `hlds_mode` 从 `templates/env/<mode>/` 加载模板，渲染写入服务器实际配置文件（`server.cfg`、`amxx.cfg`、`metamod-plugins.ini` 等），再启动 HLDS 进程。

### 游戏模式
`hlds_mode` 决定加载哪套插件和配置：`public` / `match` / `latestmatch` / `league` / `csdm`。

### AMXModX 插件版本
**必须使用 AMXModX 1.10**（非 1.9），因为数据上报使用 JSON 格式，依赖 1.10 的原生 JSON 模块。

### 服务端插件体系
- Metamod-R 作为插件加载器，`.so` 插件在 `cstrike/addons/metamod/plugins.pro.ini` 中注册
- AMXModX 插件在 `cstrike/addons/amxmodx/configs/plugins.ini` 中注册
- `procs_cmps.sma` 是核心比赛插件，对外暴露 native（`get_match_score` 等）和 forward（`match_begin` / `match_round_end` / `match_end`），其他插件可 hook 这些事件

### 1v1 新插件开发说明
原 `procs_cmps.sma` 按 5v5 设计，**不直接复用**，作为逻辑参考。新 1v1 插件需重新开发，关注：
- 2 人房间限制
- 自动 ready 流程
- 刀战换边
- 比赛结束事件上报（通过 HTTP POST 到 gateway）
