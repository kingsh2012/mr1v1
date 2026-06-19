# 端到端测试方案：bot_test_mode

记录"微信小程序 → 微信后端 → manager-backend → agent → rehlds容器 → 遥测上报 →
consumer → Postgres → 房间同步"全链路的端到端测试能力，及2026-06-18~19实测过程。

工具脚本见 [scripts/e2e-bot-test/](./scripts/e2e-bot-test/)。

---

## 1. 背景与目标

人工很难完整跑一遍"建房→匹配→真实建服→打完一局比赛→数据校验"全流程——
最后一步"打完一局比赛"需要两个真实CS 1.6客户端连进服务器对战。

`mr1v1_start_bot_test`（RCON命令，补Bot陪练）原本设计是给普通玩家用的，
但比赛模式(`g_bMatchModeEnabled`，平台真实建房会触发)下这条路径会被反作弊
guard 直接拒绝：

```pawn
StartWithBot(bool:testMode, const requester) {
	if (g_bMatchModeEnabled) {
		client_print_color(0, print_team_grey, "^4[1v1] ^1比赛模式下不支持Bot陪练");
		return;
	}
	...
}
```

这是有意为之——防止有人用Bot顶替真人刷真实排位赛数据。直接放开这个guard
还不够，因为比赛模式的"选人"逻辑（`SelectMatchModePlayers()`）要求双方
SteamID必须以**真实连接的客户端**身份进服并匹配，Bot的authid永远是
`"BOT"`，不可能通过这层校验。

## 2. 方案：两套模式共用核心逻辑，新增模式不触碰反作弊路径

不修改真实比赛的身份校验逻辑，而是新增一个**显式声明**的旁路：
平台建房时可选传 `bot_test_mode: true`，仅用于测试容器，正式排位赛
永远不会带这个参数。

数据链路（容器env一路传到Pawn变量）：

```
POST /api/manager/matches {"bot_test_mode": true, ...}
  → agentproto.CreateCommand.BotTestMode (MQTT)
  → dockerctl.Spec.BotTestMode → 容器环境变量 BOT_TEST_MODE=1
  → start.sh 写入 mr1v1_match_mode.ini 的 mr1v1_bot_test_mode=1
  → mr1v1_match.sma::LoadMatchModeConfig() 解析出 g_bBotTestModeEnabled
```

插件侧改动（仅3处，均为新增分支，`g_bBotTestModeEnabled=false`时原有
真实比赛逻辑完全不受影响）：

1. **`LoadMatchModeConfig()`**：读到`mr1v1_bot_test_mode=1`后，
   `set_task(3.0, "Task_EnableBotTestMode")`——延迟3秒再设置bot_quota，
   因为`LoadMatchModeConfig()`跑在`plugin_init`阶段，此时`server.cfg`
   还没执行完、服务器没完全起来，直接设置会导致引擎过早尝试补Bot失败。

2. **`Task_EnableBotTestMode()`**（新函数）：延迟后设置
   `bot_quota=2`（实测对于无任何既存人类玩家的空场景，quota=2就是精确
   补2个Bot，不是`StartWithBot()`里那条注释提到的"按队伍各补N个"规律——
   那条规律是该函数在已有人类玩家/已分队场景下的经验值，不能照搬）。

3. **`client_putinserver(id)`**：新增`is_user_bot(id) && g_bBotTestModeEnabled`
   分支，先到的Bot顶替slot0，后到的顶替slot1，**只记录slot绑定，不在这里
   调用`rg_set_user_team`**——这个时间点Bot在ReAPI内部的"已连接"状态还没
   就绪，强行调用会报`[ReAPI] rg_set_user_team: player 1 is not connected`
   运行时错误，进而触发引擎"踢出多余Bot维持quota"。真正的队伍分配交给
   `SelectMatchModePlayers()`（双方slot都绑定后，经`Task_StartMatchMode`
   延迟1秒触发），到那时Bot已经完全就绪。

`SelectMatchModePlayers()`本身**完全不需要改动**——它早就是按配置的
SteamID写入`g_szAuthId[]`（而不是从`get_user_authid()`实时读取），所以
不管slot里坐的是真人还是Bot，上报的SteamID永远是平台真实分配的那两个，
比分/回合/伤害等遥测数据全部真实可信。

## 3. 改动文件清单

| 文件 | 改动 |
|---|---|
| `mr1v1-rehlds/cstrike/addons/amxmodx/scripting/mr1v1_match.sma` | 见上 |
| `mr1v1-rehlds/start.sh` | `BOT_TEST_MODE`环境变量 → ini里的`mr1v1_bot_test_mode`key |
| `mr1v1-server/internal/agentproto/agentproto.go` | `CreateCommand.BotTestMode` |
| `mr1v1-server/internal/dockerctl/dockerctl.go` | `Spec.BotTestMode` → `BOT_TEST_MODE=1`环境变量 |
| `mr1v1-server/internal/agent/agent.go` | 把`cmd.BotTestMode`传给`dockerctl.Spec` |
| `mr1v1-server/internal/backend/backend.go` | `POST /api/manager/matches`请求体新增可选字段`bot_test_mode` |

## 4. 怎么触发一次测试

```bash
# 1. 触发一局Bot测试比赛（真实API，仅多传一个bot_test_mode:true）
./scripts/e2e-bot-test/trigger_bot_match.sh "我的测试比赛"
# => {"code":0,"data":{"match_id":"...", "public_ip":"...", "port":...}}

# 2. 看容器内比赛进度(回合/比分/报错)
./scripts/e2e-bot-test/check_match_logs.sh <match_id>

# 3. 比赛打完(26胜或51回合)后，查Postgres校验数据落库
./scripts/e2e-bot-test/check_match_data.sh <match_id>

# 4. 如需提前终止
./scripts/e2e-bot-test/destroy_match.sh <match_id>
```

发布新版rehlds镜像（含插件改动）后，注册+激活+预拉取一条龙：

```bash
./scripts/e2e-bot-test/release_rehlds_image.sh v0.2.12
```

## 5. 实测过程与踩坑记录（2026-06-18~19）

完整改动链路写完后，本地`GOMAXPROCS=1 go build -p=1 ./...`和
`amxxpc`编译均一次通过，但实机测试经历了4轮镶代-镶代-镶代才完全跑顺：

| 版本 | 现象 | 根因 | 修复 |
|---|---|---|---|
| v0.2.7 | `[ReAPI] rg_set_user_team: player 1 is not connected`，引擎报"These bots kicked to maintain quota" | `LoadMatchModeConfig()`在`plugin_init`阶段(server.cfg还没跑完)就直接设`bot_quota`，过早触发引擎补Bot | 改为`set_task(3.0,...)`延迟到服务器稳定后再设置 |
| v0.2.8 | 同样的`rg_set_user_team`报错依然出现，但没有"kicked"了 | 即使延迟3秒，`client_putinserver`这一帧Bot仍未被ReAPI标记为"已连接"，立即调用`rg_set_user_team`仍会失败 | 把`rg_set_user_team`调用从`client_putinserver`里删掉，交给后面延迟更久的`SelectMatchModePlayers()`统一设置 |
| v0.2.9 | 只补出1个Bot，卡在等第二个，一直不开赛 | 把`bot_quota`从2改成了1(误信了`StartWithBot()`注释里"quota=N按队伍各补N个"的经验规律，那条规律针对的是已有人类玩家在场的场景，本场景里全场无人，规律不适用) | 改回`bot_quota=2`，在空场景下quota=2正好补2个Bot(1个/队) |
| v0.2.10/11 | 全部正常：2个Bot各就位、双方按配置SteamID绑定、热身→开局→42回合打满→26:17结束 | — | — |
| v0.2.12 | 复测时发现决胜局`round_end`遥测丢失（见第6节），与bot_test_mode本身无关 | 见第6节 | `AnnounceMatchResult`改为延迟0.4秒的`Task_AnnounceMatchResult` |

最终验证（match_id=`a3861719c7db28a22651296210b5c5de`）：

- `manager_matches.state`: `creating → playing → finished` 全部正确流转
- `telemetry_match_starts`: 1行，地图/双方名字SteamID齐全
- `telemetry_round_ends`: 42行，逐回合比分/伤害数据齐全，phase 0/1/2三阶段全覆盖
- `telemetry_match_ends`: 1行，`end_reason=normal wins0=26 wins1=17`
- `telemetry_combat_events`(命中/伤害): 285行，`hitgroup`覆盖1~7全部命中部位
- `telemetry_shoot_events`(开枪): 931行，武器覆盖`usp`/`ak47`/`awp`(对应手枪/步枪/狙击三阶段)
- `telemetry_position_events`(坐标/朝向): 1201行，坐标和视角数值正常
- 容器在比赛结束后被agent自动销毁（teardown）
- `POST /api/wx/internal/match-ended`（房间同步通知）被consumer正确调用，
  返回200（这局没有关联真实房间，属于预期内的no-op，但调用链路本身验证通过）

`MR1V1_EVENTS.md`末尾"消费端尚未适配这三类事件"的备注是过时信息，本次测试已验证
consumer早就全部适配并正常落库，已同步更正该文档。

### 已知但未阻塞结论的小问题（待后续排查，不紧急）

1. `telemetry_match_ends.p0_name`/`p1_name`为空字符串——`Task_AnnounceMatchResult()`
   里`get_user_name(g_iPlayer[0/1], ...)`对Bot在那个时间点返回了空，
   不影响比分/胜负等核心字段，需要进一步在Pawn层定位。
2. agent销毁容器时RCON优雅倒计时超时(`read challenge response: i/o timeout`)，
   但有强制`docker stop`兜底，容器仍被正确清理，只是没走优雅流程。

## 6. 衍生发现：决胜局round_end遥测丢失（已修复，v0.2.12）

借助这套bot_test_mode测试能力，复测时对比`round_ends`最后一条和`match_ends`的
最终比分，发现两者不一致（`round_ends`停在25:17，`match_ends`却是26:17）——
**决胜局那一回合的`round_end`遥测整个丢失了，不是bot_test_mode特有问题，是所有
比赛通用的代码路径**：

```pawn
// RoundEnd_Post (T=0)
set_task(0.3, "Task_RoundDamage");      // 安排在T+0.3s上报本回合伤害明细
...
if (...达到胜场阈值...) {
    AnnounceMatchResult();              // T=0同步执行
}                                        // 内部调用ResetMatchState()
                                         // 同步清零g_iPlayer[]+踢Bot

// T+0.3s执行时
public Task_RoundDamage() {
    if (!g_iPlayer[0] || !g_iPlayer[1] || ...) {
        return;    // g_iPlayer已被清零，直接放弃上报
    }
    ...
}
```

`Task_RoundDamage`延迟0.3秒是为了等AMXModX CSStats模块把本回合最后一击的伤害/
命中数累加完整（`get_user_vstats`官方文档称读取的是"当前回合"统计，由模块自己的
forward实时累加，`RoundEnd_Post`这个post钩子触发时不能保证已经处理完最后一击）。
但`AnnounceMatchResult()`在决胜局会同步立刻清理比赛状态，跑在`Task_RoundDamage`
真正执行之前，导致决胜局的伤害明细永久丢失（最终比分不受影响，`match_end`走的
是另一条不依赖`g_iPlayer`的独立上报路径）。**这意味着生产环境历史上所有比赛的
最后一回合伤害/命中明细都缺失了这一条。**

**修复**（`mr1v1_match.sma`）：把`AnnounceMatchResult()`改名为
`public Task_AnnounceMatchResult()`并通过`set_task(0.4, ...)`延迟调用——
比`Task_RoundDamage`的0.3秒晚0.1秒，确保伤害先上报完，`ResetMatchState()`
清理动作再发生。用户感知不到这0.1秒的播报延迟。

验证（match_id=`8f1a288706786a2b3f71fceee1e414b2`，rehlds v0.2.12）：

```
round_ends: round=42, wins0=26, wins1=16, ts=1781831063
match_ends:           wins0=26, wins1=16, ts=1781831063
```

决胜局就是round 42本身（不再缺失），比分与`match_end`完全一致，时间戳同秒——
修复生效。

## 6. 安全边界（为什么这个方案不会被滥用）

- `bot_test_mode`字段只能通过调用方在建房请求体里显式声明为`true`才会生效，
  正式小程序/真实排位赛的建房请求永远不带这个字段。
- 即使有人构造请求强行传`bot_test_mode:true`，也只是把**自己这局**比赛
  变成Bot对战，不影响、不能伪造别人的比赛数据，没有越权风险。
- 真实比赛的身份校验代码路径（`client_authorized`里按真实SteamID匹配
  slot那一段）完全没有改动一行。
