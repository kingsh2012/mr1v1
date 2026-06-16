# 设计：开枪事件(mr1v1_shoot_batch)与移动轨迹事件(mr1v1_position_batch)

## 背景

`MR1V1_EVENTS.md` 中预告了两个"后续计划"事件类型：`mr1v1_shoot_batch`（开枪）和
`mr1v1_position_batch`（玩家移动轨迹），此前未实现。已实现的 `mr1v1_combat_batch`
（命中/伤害，`mr1v1_telemetry.sma`）建立了"hook采集 → 数组缓冲 → 每1秒批量flush →
HTTP上报"的标准模式，本次两个新事件类型复用该模式，新增独立缓冲数组 + flush函数，
不改动 `mr1v1_match.sma`（仍只通过 `mr1v1_match_start`/`mr1v1_match_end` forward
与 `mr1v1_telemetry.sma` 交互）。

consumer端尚未适配（与现有 `mr1v1_combat_batch` 同样会落入default分支被丢弃），
本次不涉及Go侧改动。

## 实现位置

`server/cstrike/addons/amxmodx/scripting/mr1v1_telemetry.sma`

## 1. 开枪事件采集 (mr1v1_shoot_batch)

**Hook**：`RG_CBasePlayerWeapon_ItemPostFrame`（Post），通过比较武器实体的
`m_Weapon_iClip`（弹夹余弹）相邻两次Post回调的变化来判定"是否打了一枪"：

- 维护 `g_iLastClip[entity]`（按武器实体索引，sentinel值-1表示未观察过）
- 每次Post回调：读取当前 `clip = get_member(this, m_Weapon_iClip)`
  - 若 `g_iLastClip[this] != -1 && clip < g_iLastClip[this]` → 判定为开枪，记录事件
  - 更新 `g_iLastClip[this] = clip`

这种方式覆盖所有有弹夹的武器（手枪/步枪/狙击枪，对应1v1三个阶段的全部枪械），
对换弹（clip增加）和切枪（首次观察不触发）天然免疫；比 `RG_CBasePlayerWeapon_KickBack`
（仅覆盖部分自动武器，不含USP/Glock/Deagle/AWP等1v1关键武器）更通用。

获取玩家slot：`pev(this, pev_owner)` → `SlotOf(owner)`（复用现有函数）；
武器名：`get_member(this, m_iId)` → `xmod_get_wpnname()`（与现有combat事件同样的取名方式）。

**数据结构**（新增并行数组，仿照现有 `g_iEvt*`）：
```
g_iShootTs[MAX_BATCH_EVENTS]
g_iShootSlot[MAX_BATCH_EVENTS]
g_szShootWeapon[MAX_BATCH_EVENTS][32]
g_iShootAmmo[MAX_BATCH_EVENTS]   // 开枪后剩余弹夹
g_iShootCount
```
`PushShootEvent(slot, weapon[], ammo)`：与现有 `PushEvent()` 一样，满则先flush。

**上报payload**：
```json
{
  "match_id": "...",
  "events": [
    { "ts": 1700000000, "slot": 0, "weapon": "ak47", "ammo_remaining": 29 }
  ]
}
```
`FlushShootBatch()` → `ReportEvent("mr1v1_shoot_batch", data)`。

## 2. 移动轨迹采集 (mr1v1_position_batch)

**采样方式**：不额外hook PreThink/PostThink（每帧触发，开销大且无必要），复用现有
`Task_FlushBatch`（`FLUSH_INTERVAL=1.0`秒一次）的节奏，在该任务里对 `g_iTelePlayer[0]`
和 `g_iTelePlayer[1]`（若 `g_bActive` 且玩家存活 `is_user_alive`）各采样一次：

- `pev(id, pev_origin)` → x/y/z
- `pev(id, pev_v_angle)` → pitch/yaw（朝向）

**数据结构**：
```
g_iPosTs[MAX_BATCH_EVENTS]
g_iPosSlot[MAX_BATCH_EVENTS]
g_flPosX[MAX_BATCH_EVENTS], g_flPosY[MAX_BATCH_EVENTS], g_flPosZ[MAX_BATCH_EVENTS]
g_flPosYaw[MAX_BATCH_EVENTS], g_flPosPitch[MAX_BATCH_EVENTS]
g_iPosCount
```
每次1秒任务最多新增2条（双方各1条），`MAX_BATCH_EVENTS=32` 足够（约16秒缓冲），
满则触发flush（与现有逻辑一致）。

**上报payload**（坐标用 `json_object_set_real`/`json_array_append_real`，json.inc支持float）：
```json
{
  "match_id": "...",
  "events": [
    { "ts": 1700000000, "slot": 0, "x": 123.4, "y": -50.2, "z": 10.0, "yaw": 90.5, "pitch": -5.2 }
  ]
}
```
`FlushPositionBatch()` → `ReportEvent("mr1v1_position_batch", data)`。

## 3. 接入点

- `plugin_init`：新增 `RegisterHookChain(RG_CBasePlayerWeapon_ItemPostFrame, "CBasePlayerWeapon_ItemPostFrame_Post", true);`
- `Task_FlushBatch`（每1秒，"b"重复任务）：在现有 `FlushBatch()`（combat）之外，
  增加位置采样 `SamplePositions()` + `FlushShootBatch()` + `FlushPositionBatch()`
- `mr1v1_match_start`：重置 `g_iShootCount=0`、`g_iPosCount=0`、`arrayset(g_iLastClip, -1, ...)`
  清空上一场比赛残留的武器实体弹夹记录
- `mr1v1_match_end`：与现有 `FlushBatch()` 一样，强制flush剩余的shoot/position事件
- `PLUGIN_VERSION` 从 `"1.0"` 升级为 `"1.1"`（envelope里的`version`字段会体现）

## 4. 文档更新（MR1V1_EVENTS.md）

- 在 `mr1v1_combat_batch` 章节之后，新增 `mr1v1_shoot_batch` 和 `mr1v1_position_batch`
  两个章节，结构与现有章节一致（触发时机、json示例、字段表）
- 移除"后续计划"措辞，改为已实现
- "消费端适配状态"备注里追加这两个新type，说明同样会被consumer的default分支丢弃

## 验证

1. `amxxpc mr1v1_telemetry.sma -omr1v1_telemetry` 编译通过，无警告
2. 部署后用 `mr1v1_start_bot_test` 开一局测试比赛，观察 `cstrike/addons/amxmodx/logs`
   中 `MR1V1_TELEMETRY` 相关日志，确认无HTTP报错（`OnGatewayHttpComplete`不报error）
3. 若本地有gateway/收集端可跑，用简单HTTP server接收 `/record` 请求，人工检查
   `mr1v1_shoot_batch`/`mr1v1_position_batch` 的JSON结构是否符合上面定义
4. 编译产物 `mr1v1_telemetry.amxx` 移动到 `../plugins/`（`configs/plugins.ini` 已注册，不改动）
