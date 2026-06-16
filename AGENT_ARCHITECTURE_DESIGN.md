# Agent 架构设计

> 状态：**设计稿**，对应 [ROADMAP.md](./ROADMAP.md) 阶段三/四/五的重新设计。具体实现按本文档分阶段在 `mr1v1-server/` 中落地。

## 1. 背景与目标

原计划中"遥测网关（gateway）"和"调度/管理器服务"是两个独立组件，但二者都需要：
- 常驻每台rehlds主机
- 与平台侧建立MQTT连接

因此合并为单个per-host **agent** 进程，减少部署面。同时，原本"容器常驻+每日定时硬重启"的模型，调整为"**按局ephemeral创建/销毁**"——一台rehlds容器对应一场比赛，打完即销毁，与`match_id`即唯一标识的设计天然契合。

## 2. 部署拓扑（集中化）

```
                    ┌─────────────────────────────────────┐
                    │           平台侧（集中部署一套）        │
                    │                                       │
                    │  mosquitto  ←─────┐                  │
                    │      ↑            │                  │
                    │  consumer ←───────┤                  │
                    │      ↓            │                  │
                    │  postgres     platform-backend       │
                    │                   │  (撮合/match_id/  │
                    │                   │   agent注册表/     │
                    │                   │   建房指令下发)     │
                    └───────────────────┼───────────────────┘
                                         │ MQTT (公网/内网均可达)
              ┌──────────────────────────┼──────────────────────────┐
              │                          │                          │
        ┌─────┴─────┐              ┌─────┴─────┐              ┌─────┴─────┐
        │  host A   │              │  host B   │              │  host C   │
        │  agent    │              │  agent    │              │  agent    │
        │ (gateway+ │              │ (gateway+ │              │ (gateway+ │
        │ 心跳+rcon+│              │ 心跳+rcon+│              │ 心跳+rcon+│
        │  docker)  │              │  docker)  │              │  docker)  │
        │     │     │              │     │     │              │     │     │
        │ docker.sock              │ docker.sock              │ docker.sock
        │     │     │              │     │     │              │     │     │
        │ rehlds容器 │              │ rehlds容器 │              │ rehlds容器 │
        │ (per-match)│              │ (per-match)│              │ (per-match)│
        └───────────┘              └───────────┘              └───────────┘
```

- mosquitto、postgres、consumer、platform-backend **只部署一套**，统一对外暴露MQTT地址（内网或带认证的公网均可）
- 每台rehlds主机只跑一个**agent**进程，agent是该主机上唯一与平台/MQTT通信的组件
- rehlds容器：`network_mode: host`，per-match创建，与agent同主机

## 3. agent 进程职责

单一进程、单一MQTT连接，内部模块化：

| 模块 | 职责 |
|------|------|
| 遥测转发（gateway） | 沿用`mr1v1-server/internal/gateway`：`POST /record` → 内存队列 → 发布到`mr1v1/{match_id}` |
| 心跳 | 定期上报`host_id`、`public_ip`、`private_ip`、可用端口范围、当前占用端口 |
| 建房指令接收 | 订阅平台下发的建房指令：`match_id`、`server_name`、`port`、`p0_steamid`、`p1_steamid`、镜像tag |
| 容器生命周期 | 挂载`/var/run/docker.sock`，用Docker Go SDK `docker run`（注入环境变量）/ `docker stop`+`docker rm` |
| RCON client | 比赛结束后通过RCON触发amxx的销毁倒计时函数 |

## 4. MQTT topic 设计（集中化）

遥测topic维持现状不变：`mr1v1/{match_id}`（由agent内置的gateway模块发布，consumer订阅`mr1v1/#`）。

新增独立prefix `mr1v1-agent/` 用于控制面，避免与遥测topic混淆：

| topic | 方向 | 内容 |
|-------|------|------|
| `mr1v1-agent/{host_id}/heartbeat` | agent → 平台 | `{host_id, public_ip, private_ip, port_range:[start,end], busy_ports:[...], ts}` |
| `mr1v1-agent/{host_id}/create` | 平台 → agent | `{match_id, server_name, port, p0_steamid, p1_steamid, image}` |
| `mr1v1-agent/{host_id}/status` | agent → 平台 | `{match_id, host_id, port, state: "running"\|"stopped"\|"error", message}` |

平台后端订阅 `mr1v1-agent/+/heartbeat` 和 `mr1v1-agent/+/status` 维护agent在线状态/资源视图；下发建房指令时直接publish到目标agent的`create` topic（host_id在心跳中已知）。

`host_id`生成方式：agent启动时若本地无持久化id则生成一个UUID并写入本地文件，重启后复用（避免每次重启在平台侧产生"新主机"）。

## 5. 容器环境变量契约

agent通过Docker SDK创建rehlds容器时注入：

| 环境变量 | 含义 |
|---------|------|
| `MATCH_ID` | 比赛唯一ID，作为上报信封的`match_id` |
| `P0_STEAMID` / `P1_STEAMID` | 双方玩家SteamID（17位） |
| `SERVER_NAME` | 服务器在游戏内显示的名称 |
| `PORT` | rehlds监听端口 |
| `GATEWAY_HTTP` | agent内置gateway的`/record`地址（通常`http://127.0.0.1:<agent端口>/record`，因为`network_mode: host`） |

`start.sh`读取以上环境变量，写入amxx可读的配置文件（沿用`configs/mr1v1.ini`的key=value格式，新增`configs/mr1v1_match_mode.ini`或直接复用同一文件追加字段，具体由实现阶段确定）。

> **待定**：rehlds容器的镜像tag/拉取策略——由平台下发指定tag（`create`指令的`image`字段），还是agent固定使用`latest`本地镜像。需要在实现容器生命周期模块前确定。

## 6. amxx "比赛模式" 状态机（mr1v1_match.sma 新增）

1. **启动检测**：插件初始化时读取比赛模式配置文件，若存在`MATCH_ID`/`P0_STEAMID`/`P1_STEAMID`，进入"比赛模式"；否则维持现有手动`.start`流程
2. **等待玩家**：比赛模式下禁用`.start*`命令和bot自动补位；持续检测已连接玩家的SteamID
3. **自动开局**：双方steamid玩家均已连入 → 复用现有分边/装备发放逻辑，自动开始比赛（等价于`.start`但跳过手动确认）
4. **比赛结束**：现有`mr1v1_match_end`上报逻辑不变；新增一个可由RCON触发的销毁函数（`register_srvcmd`注册）：
   - 复用`client_print_color`广播倒计时
   - 倒计时结束后kick所有玩家
   - 插件侧到此为止；容器层面的`docker stop`/`docker rm`由agent负责

## 7. 容器生命周期模型变更

| | 旧模型 | 新模型 |
|---|--------|--------|
| 生命周期 | 常驻 + 每日5点定时硬重启（`Task_DailyRestart`/`.hardreset`） | 按局ephemeral：agent创建 → 比赛结束 → agent销毁 |
| 比赛信息注入 | 插件内手动`.start`流程生成 | 容器启动时通过环境变量注入，`start.sh`写入配置文件 |
| 端口/资源管理 | 静态配置 | 平台后端基于agent心跳上报的端口范围动态分配 |

原"每日定时硬重启"机制保留作为**未分配比赛的空闲容器**维护手段的设计参考（具体是否需要在ephemeral模型下保留待后续评估）。

## 8. 平台后端职责（属于本仓库 `mr1v1-server/` 范围）

- 撮合完成后生成`match_id`（UUID）
- 维护agent注册表：订阅`mr1v1-agent/+/heartbeat`，记录每台agent的在线状态、IP、端口占用
- 接收撮合请求（HTTP API，调用方为平台业务层，具体鉴权/接口形态待实现阶段确定）→ 选择空闲agent → 发布建房指令到`mr1v1-agent/{host_id}/create`
- 订阅`mr1v1-agent/+/status`，跟踪建房结果和容器销毁通知，更新端口占用视图

## 9. 实施顺序建议

1. `internal/agentproto`：定义心跳/建房指令/状态上报的Go结构体（agent与平台后端共用）
2. agent：在现有`internal/gateway`基础上新增心跳发布 + 建房指令订阅（先不接docker，仅打日志）
3. agent：接入Docker SDK，实现容器创建/销毁
4. agent：RCON client + 销毁触发
5. 平台后端：agent注册表 + HTTP撮合API + 建房指令下发
6. amxx：比赛模式状态机 + 销毁函数
7. `start.sh`：环境变量 → 配置文件写入
8. docker-compose拆分为"中心栈"（mosquitto/postgres/consumer/platform-backend）与"agent栈"（per-host）
