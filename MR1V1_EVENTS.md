# mr1v1 插件上报事件

数据上报由两个插件分别负责，地址均见 `configs/mr1v1.ini`（`mr1v1_gateway_http` 留空则不上报）：

- `server/cstrike/addons/amxmodx/scripting/mr1v1_match.sma`：比赛流程插件，比赛开始/每回合结束/比赛结束时各上报一次（`mr1v1_match_start` / `mr1v1_round_end` / `mr1v1_match_end`）。
- `server/cstrike/addons/amxmodx/scripting/mr1v1_telemetry.sma`：数据采集插件，与比赛流程插件解耦——通过其广播的自定义forward `mr1v1_match_start(match_id, id0, id1)` / `mr1v1_match_end(...)` 获知当前比赛上下文，期间收集命中/伤害等战斗事件，按固定周期（默认1秒）批量上报一次（`mr1v1_combat_batch`）。后续计划加入开枪、移动轨迹等事件，均会走这个插件、复用同一套批量上报机制。

## 消息信封

每次上报都是对 `{gateway_http}/record` 发起一次 POST，Content-Type: `application/json`，
body为统一信封：

```json
{
  "timestamp": 1700000000,
  "match_id": "1700000000ABC123",
  "type": "mr1v1_match_start | mr1v1_round_end | mr1v1_match_end",
  "version": "<插件版本号>",
  "data": { /* 见下方各事件的data结构，为嵌套JSON对象，不是字符串 */ }
}
```

`match_id`为本场比赛唯一ID（与`data`里的`match_id`字段一致），gateway可据此将同一场比赛的
所有事件分流到MQTT topic `{prefix}/{match_id}`。1v1服务器打完即销毁、一台对应一场比赛，
因此不再使用服务器级`token`字段。

`data`为嵌套JSON对象（非转义字符串），插件用 `json_object_set_value()` 直接挂载到信封下。

以下分别说明三种 `type` 对应的 `data` 内容（已反序列化展示）、字段含义、触发时机。

## mr1v1_match_start

**触发时机**：`InitMatch()` 完成选人、双方分边后，比赛正式开始时上报一次
（`.start` / `.start_bot` / `.start_bot_test` 任一方式开始比赛都会触发）。

```json
{
  "match_id": "1700000000ABC123",
  "map": "de_dust2",
  "p0.name": "Player1",
  "p0.authid": "STEAM_0:0:123456",
  "p0.userid": 2,
  "p1.name": "Player2",
  "p1.authid": "STEAM_0:1:654321",
  "p1.userid": 3
}
```

| 字段 | 说明 |
| --- | --- |
| `match_id` | 本场比赛唯一ID（开始时生成） |
| `map` | 当前地图名 |
| `p0.*` / `p1.*` | 两名对战玩家的名字/SteamID/userid。Bot的`authid`为生成的`BOT_<userid>_<随机数>`（见下方"Bot唯一ID"说明），不是固定的`"BOT"` |

## mr1v1_round_end

**触发时机**：每回合 `RoundEnd_Post`（非热身、非 `ROUND_GAME_COMMENCE`）结束后，
延迟0.3秒（等 `Task_RoundDamage` 统计完伤害）上报一次。**比赛打满/提前结束的那一回合也会上报本事件**
（之后再额外上报一次 `mr1v1_match_end`）。

```json
{
  "match_id": "1700000000ABC123",
  "round": 5,
  "phase": 0,
  "winner_slot": 0,
  "wins0": 3,
  "wins1": 2,
  "p0.damage": 45,
  "p0.hits": 3,
  "p1.damage": 52,
  "p1.hits": 4
}
```

| 字段 | 说明 |
| --- | --- |
| `match_id` | 对应 `mr1v1_match_start` 的 `match_id` |
| `round` | 本回合序号（1 ~ 51） |
| `phase` | 本回合所属阶段：`0`=手枪局(1-10) / `1`=步枪局(11-38) / `2`=狙击局(39-51) |
| `winner_slot` | 本回合获胜者：`0`/`1`，平局（如时间到双方均未达成胜利条件）为`-1` |
| `wins0` / `wins1` | 本回合结束后双方的**累计**胜场数（即实时比分） |
| `p0.damage` / `p0.hits` / `p1.damage` / `p1.hits` | 双方本回合对对方造成的伤害（上限100）和命中次数 |

## mr1v1_match_end

**触发时机**：比赛结束时上报一次，覆盖以下4种场景（由 `end_reason` 区分）：

| `end_reason` | 触发条件 |
| --- | --- |
| `normal` | 正常结束：达到 `TOTAL_ROUNDS`(51) 或某一方达到 `WIN_THRESHOLD`(26胜) |
| `manual_stop` | 管理员/玩家通过 `.stop`（聊天）或 RCON `mr1v1_stop` 手动停止 |
| `disconnect` | 对战玩家掉线后立即判定无法重连（如Bot掉线），比赛取消 |
| `disconnect_timeout` | 对战玩家掉线，60秒内未通过原SteamID重连，比赛取消 |

```json
{
  "match_id": "1700000000ABC123",
  "end_reason": "normal",
  "winner_slot": 0,
  "wins0": 26,
  "wins1": 18,
  "p0.name": "Player1",
  "p0.authid": "STEAM_0:0:123456",
  "p1.name": "Player2",
  "p1.authid": "STEAM_0:1:654321"
}
```

| 字段 | 说明 |
| --- | --- |
| `match_id` | 对应 `mr1v1_match_start` 的 `match_id` |
| `end_reason` | 见上表 |
| `winner_slot` | 获胜者：`0`/`1`；平局（`normal`且双方比分相同）或中止（`manual_stop`/`disconnect*`）均为`-1` |
| `wins0` / `wins1` | 结束时双方的最终/中止时比分 |
| `p0.name` / `p0.authid` / `p1.name` / `p1.authid` | 双方名字/SteamID。中止场景下玩家可能已断线，名字取的是比赛开始时记录的名字 |

## mr1v1_combat_batch

由 `mr1v1_telemetry.sma` 上报。**触发时机**：比赛进行期间，每隔1秒（事件缓冲区满则提前）打包上报一批战斗事件；
比赛结束（`mr1v1_match_end` forward）时会强制flush一次剩余事件。若期间没有任何事件则不上报。

```json
{
  "match_id": "1700000000ABC123",
  "events": [
    {
      "ts": 1700000000,
      "attacker_slot": 0,
      "victim_slot": 1,
      "weapon": "ak47",
      "damage": 26,
      "hitgroup": 4
    }
  ]
}
```

| 字段 | 说明 |
| --- | --- |
| `match_id` | 对应 `mr1v1_match_start` 的 `match_id` |
| `events` | 本批次内按发生顺序排列的命中事件数组 |
| `events[].ts` | 事件发生时的Unix时间戳（秒精度，同一秒内多个事件靠数组顺序排序） |
| `events[].attacker_slot` / `events[].victim_slot` | 攻击者/受害者：`0`/`1`，对应 `mr1v1_match_start` 的 `p0`/`p1` |
| `events[].weapon` | 攻击武器classname（去掉`weapon_`前缀，如`ak47`/`usp`） |
| `events[].damage` | 本次命中造成的伤害（护甲减免前，四舍五入取整） |
| `events[].hitgroup` | 命中部位：`1`=头 `2`=胸 `3`=腹 `4`/`5`=左右臂 `6`/`7`=左右腿（HL引擎`HIT_*`常量） |

**后续计划**：开枪事件、玩家移动轨迹采集，均会新增 `type`（如 `mr1v1_shoot_batch` / `mr1v1_position_batch`），复用本插件的批量上报机制。

## 备注

- **Bot唯一ID**：Bot的 `get_user_authid` 固定返回 `"BOT"`，插件会替换为 `BOT_<userid>_<6位随机数>` 格式作为 `authid` 上报，避免多场Bot对局在数据库里authid冲突。
- **消费端适配状态**：以上事件类型目前PROCS.PRO-REHLDS-COLLECTION-SYSTEM的consumer尚未识别（switch落入`default`分支被丢弃），数据还未真正落库，需要后续适配（新增`pkg/mes`结构体 + consumer的case分支 + 表注册，详见讨论记录）。
