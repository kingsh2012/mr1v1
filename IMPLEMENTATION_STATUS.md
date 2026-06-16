# mr1v1 平台实现状态报告

## 一、已实现（AMXX侧，本仓库 `server/`）

### 1. 比赛流程插件 `mr1v1_match.sma`
- **赛制**：51回合（10手枪/28步枪/13狙击），26胜提前结束，无加时；第1回合随机分边，之后每回合自动T/CT互换
- **装备管理**：每回合强制清空背包，按当前阶段+玩家偏好（`.1`/`.2`/`.3`/`.4`/`.guns`菜单）重发武器/满弹/护甲
- **比赛控制**：`.start`/`.start_bot`/`.start_bot_test`/`.stop`（聊天）+ 对应RCON命令；`.start_bot`系列已按"真人/观察者/无人"区分场景自动补1或2个专家Bot
- **命令菜单**：`say h` 按当前比赛状态弹出对应命令菜单
- **掉线重连**：对战玩家掉线60秒内SteamID匹配可恢复，超时自动取消比赛
- **换图**：`.map` 内置1v1地图菜单，比赛中禁止换图
- **HTTP上报**：`mr1v1_match_start`/`mr1v1_round_end`/`mr1v1_match_end` 三类事件，新信封（`timestamp`/`match_id`/`type`/`version`/`data`，`data`为嵌套JSON对象，已去掉服务器级`token`）
- **每日定时硬重启**：`.h`菜单 + 每日5点自动硬重启（`Task_DailyRestart`/`.hardreset`）——该机制是当前"常驻容器"模型下的兜底维护手段，待[AGENT_ARCHITECTURE_DESIGN.md](./AGENT_ARCHITECTURE_DESIGN.md)中"按局创建/销毁"的ephemeral容器模型落地后，定位会调整为"未分配比赛的空闲容器维护"场景

### 2. 数据采集插件 `mr1v1_telemetry.sma`（与上面解耦）
- 通过自定义forward `mr1v1_match_start`/`mr1v1_match_end` 获知当前`match_id`和双方玩家
- Hook `TraceAttack` 采集**命中/伤害事件**（攻击者/受害者slot、武器、伤害、hitgroup），每1秒（或攒满32条）批量上报 `mr1v1_combat_batch`
- 已新增**开枪事件**（`mr1v1_shoot_batch`）与**玩家移动轨迹**（`mr1v1_position_batch`）批量上报，复用同一套周期上报机制（已编译部署，待commit）

### 3. 配置与文档
- `configs/mr1v1.ini`、`plugins.ini` 已注册两个插件
- `MR1V1_EVENTS.md`/`CLAUDE.md`/`README.md` 已同步信封格式和事件字段说明
- `MR1V1_TELEMETRY_SHOOT_POSITION_DESIGN.md` 记录了开枪/轨迹采集的字段设计

---

## 二、已实现（Go侧，独立项目 `mr1v1-server/`）

与历史的 `PROCS.PRO-REHLDS-COLLECTION-SYSTEM` 完全解耦，是一个新的独立Go module（`go 1.22`），覆盖新信封格式和全部6种事件类型：

- **`internal/envelope`**：`Envelope{timestamp, match_id, type, version, data}`，6种事件类型（`mr1v1_match_start`/`mr1v1_round_end`/`mr1v1_match_end`/`mr1v1_combat_batch`/`mr1v1_shoot_batch`/`mr1v1_position_batch`）的结构体定义，字段与[MR1V1_EVENTS.md](./MR1V1_EVENTS.md)完全对齐
- **`internal/model` + `internal/consumer`**：GORM模型对应6张表，`AutoMigrate`一次性建好；consumer订阅MQTT `{prefix}/#`，按`type`路由，单行事件直接insert，批量事件（combat/shoot/position）`CreateInBatches`落库
- **`internal/gateway` + `cmd/gateway`**：HTTP `POST /record`（解码信封→内存队列→按`{prefix}/{match_id}`分流发布到MQTT，QoS1）+ `GET /healthz`
- 构建状态：`go build ./...`、`gofmt -l .`均通过；`docker-compose.yml`/`Dockerfile-gateway`/`Dockerfile-consumer`/`mosquitto.conf`已就位，但**docker compose端到端验证尚未执行**（涉及在生产主机上拉起postgres/mosquitto，需先确定部署拓扑——见下文"控制面/agent架构"）

---

## 三、未实现 / 待办

### 数据采集（AMXX侧）
- 暂无（开枪/轨迹采集已完成，见上）

### 控制面 / agent架构（设计中，详见 [AGENT_ARCHITECTURE_DESIGN.md](./AGENT_ARCHITECTURE_DESIGN.md)）
对应 [ROADMAP.md](./ROADMAP.md) 阶段三/四/五的重新设计，核心变化：
- **集中化部署拓扑**：mosquitto + postgres + consumer + 平台后端集中部署一套；每台rehlds主机只跑一个**agent**进程
- **agent职责**：遥测转发（沿用`internal/gateway`）+ 心跳上报（host_id/公网IP/内网IP/可用端口范围）+ 接收平台下发的建房指令 + 挂载`docker.sock`管理rehlds容器生命周期 + RCON客户端（注入比赛模式参数/触发销毁倒计时）
- **平台后端**（属于本仓库`mr1v1-server`范围）：撮合后生成`match_id`、维护agent在线状态/端口占用、下发建房指令到对应agent
- **容器生命周期**：从"常驻+每日定时硬重启"转为"按局ephemeral创建/销毁"
- **amxx"比赛模式"**：容器启动时通过环境变量注入`match_id`+双方steamid，`start.sh`写入配置文件，插件检测到指定玩家加入后自动分队开局；比赛结束RCON触发倒计时→kick→agent执行`docker stop/rm`释放端口

均为**设计阶段**，尚未开始编码。

### 分析/展示层（尚未开始）
- 命中率等玩家数据统计：consumer已落库shoot/combat事件，统计查询/展示尚未开发
- 2D回放/平面展示图：position事件已落库，回放前端尚未开始

---

## 小结
**AMXX游戏端**采集能力已全部到位（命中/伤害/开枪/轨迹四类事件，待commit）。**`mr1v1-server`** 完成了gateway→MQTT→consumer→PostgreSQL全链路的代码实现并通过编译，端到端docker验证待做。下一阶段的主要工作量在**控制面/agent架构**：集中化的MQTT/PG/平台后端 + 每台主机的agent（遥测转发+心跳+容器调度+RCON）+ amxx比赛模式状态机，详见[AGENT_ARCHITECTURE_DESIGN.md](./AGENT_ARCHITECTURE_DESIGN.md)。
