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

### 2. 数据采集插件 `mr1v1_telemetry.sma`（新增，与上面解耦）
- 通过自定义forward `mr1v1_match_start`/`mr1v1_match_end` 获知当前`match_id`和双方玩家
- Hook `TraceAttack` 采集**命中/伤害事件**（攻击者/受害者slot、武器、伤害、hitgroup）
- 每1秒（或攒满32条）批量上报 `mr1v1_combat_batch`

### 3. 配置与文档
- `configs/mr1v1.ini`、`plugins.ini` 已注册两个插件
- `MR1V1_EVENTS.md`/`CLAUDE.md`/`README.md` 已同步信封格式和事件字段说明

---

## 二、未实现 / 待办

### 数据采集（AMXX侧）
- **开枪事件**：ReAPI没有现成的`PrimaryAttack` hookchain，需要找替代方案（如`ItemPostFrame`+弹药变化检测），尚未实现
- **玩家移动轨迹**：用于未来2D战局回放，尚未设计采样频率和采集方式，未开始

### Go侧（PROCS.PRO-REHLDS-COLLECTION-SYSTEM，本次按要求未改动）
- `pkg/mes` 信封结构仍是旧版（`token` + `data`字符串），未跟进AMXX侧的`match_id`+嵌套对象新格式
- consumer 不识别 `mr1v1_match_start`/`mr1v1_round_end`/`mr1v1_match_end`/`mr1v1_combat_batch`，落入`default`分支被丢弃，**未真正落库**
- gateway 未实现按`match_id`分流MQTT topic（`{prefix}/{match_id}`）
- 数据库尚无对应这些新事件的表结构/字段

### 分析/展示层（尚未开始）
- 命中率等玩家数据统计：依赖上面战斗事件先落库
- 2D回放/平面展示图：依赖移动轨迹采集 + 回放前端，均为空白

---

## 小结
**AMXX游戏端**这一轮的设计目标（信封简化为match_id+嵌套JSON、采集与比赛流程解耦、命中事件批量上报）已落地并实测验证。**整条数据链路的"下半段"（gateway分流、consumer落库、统计/回放）还是空白**，是后续主要工作量所在；游戏端的开枪/移动轨迹采集也还没做。
