// ============================================================
// MR1V1 捡枪式比赛插件
//
// 规则：
//   - 完全不做武器/购买限制，出生装备、购买菜单、拾取/丢弃/死亡掉枪全部交给
//     地图/引擎默认行为，插件不插手——地图设计本身决定了打法(比如aim_map这类
//     给AK/M4的就是步枪图，sk_系列给USP的就是手枪图)，靠地图池分类而不是插件限制
//   - 最多31回合，先达16胜者获胜，提前分出胜负即结束（无加时）
//   - 队伍分边第1回合随机决定，之后每回合结束后无论胜负T/CT自动互换
//   - 记分板T/CT回合数实时映射为"当前在该边的玩家的个人胜场数"，与自定义HUD比分保持一致
//   - 比赛开始后 mp_freezetime 设为1秒
//   - 比赛开始时一次性公示规则；回合结束显示双方本回合伤害
//   - 启动方式：聊天 .start，或后台RCON执行 mr1v1_start
//   - 停止比赛：聊天 .stop，或后台RCON执行 mr1v1_stop
//   - 换图：聊天 .map 弹出内置1v1地图菜单，或 .map <地图名> 直接指定（比赛进行中无法换图），3秒后切换
//   - 聊天框输入 h 弹出命令菜单（按当前比赛状态显示.start/.start_bot/.start_bot_test/.map
//     或.stop），免去记忆具体指令
//   - 比赛进行中加入的第三人自动设为观察者
//   - 对战玩家掉线后60秒内重连(SteamID匹配)可恢复比赛，超时则自动取消
//   - 比赛开始/每回合结束/比赛结束 通过 HTTP POST 上报到 gateway（见 configs/mr1v1.ini）
// ============================================================

#include <amxmodx>
#include <amxmisc>
#include <fakemeta>
#include <fakemeta_stocks>
#include <engine>
#include <csstats>
#include <reapi>
#include <json>
#include <easy_http>

#define PLUGIN_NAME    "MR1V1 Match"
#define PLUGIN_VERSION "2.0"
#define PLUGIN_AUTHOR  "1v1 Platform"

#define TOTAL_ROUNDS     31
#define WIN_THRESHOLD    16

new bool:g_bMatchActive;
new bool:g_bTestMode;
new g_iRoundNum;
new g_iPlayer[2];
new TeamName:g_eCurrentTeam[2];
new g_iWins[2];
new g_szAuthId[2][35];
new g_szPlayerName[2][32];
new bool:g_bPendingReconnect[2];
new g_iLastRoundNum;
new g_iLastWinnerSlot;

new bool:g_bWarmupActive;
new g_iWarmupSecLeft;
new g_pCvarWarmupTime;
new g_pCvarRoundInfinite;
new g_pCvarForceRespawn;
new g_pCvarRespawnImmunity;
new g_szRoundInfiniteOld[8];
new g_iForceRespawnOld;
new Float:g_flRespawnImmunityOld;
#define TASK_WARMUP_TIMER 7777

new g_hudSync;
new g_hudWarmupSync;
new g_hFwdMatchEnd;
new g_hFwdMatchStart;

// StartWithBot()里临时改过的bot相关cvar，比赛结束(RestoreServerCvars)时恢复成开始前的值
new bool:g_bBotCvarsModified;
new g_iBotJoinAfterPlayerOld;
new g_szBotQuotaModeOld[32];
new g_iBotDifficultyOld;

new g_szGatewayHttp[128];
new g_szMapName[32];

// 比赛唯一ID：每局比赛开始(InitMatch)时生成一次，贯穿该局所有上报事件，
// 供平台后端关联 mr1v1_match_start/mr1v1_round_end/mr1v1_match_end 三类事件
#define MATCH_ID_RAND_LEN  14
new const g_matchIdChars[] = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ";
new g_szMatchId[33];

// 比赛模式：容器由平台agent按局创建，启动时已知match_id和双方steamid
// （configs/mr1v1_match_mode.ini，由start.sh按环境变量写入），双方玩家进入后
// 自动分队/自动开局，跳过.start手动流程；非指定玩家强制观察者
new bool:g_bMatchModeEnabled;
new g_szMatchModeId[33];
new g_szMatchModeSteamId[2][35];
new TeamName:g_eMatchModeTeam[2];
new bool:g_bMatchModeConnected[2];
// 比赛模式下的Bot顶替测试：mr1v1_match_mode.ini 额外声明 mr1v1_bot_test_mode=1 时启用，
// 仅由agent在测试用容器里显式注入，真实排位赛容器不带这个key，行为完全不受影响
new bool:g_bBotTestModeEnabled;
new g_iMatchModePlayer[2];

GenerateMatchId() {
	if (g_bMatchModeEnabled && strlen(g_szMatchModeId)) {
		copy(g_szMatchId, charsmax(g_szMatchId), g_szMatchModeId);
		return;
	}

	new buf[MATCH_ID_RAND_LEN + 1], raw[48];
	for (new i = 0; i < MATCH_ID_RAND_LEN; i++) {
		buf[i] = g_matchIdChars[random_num(0, charsmax(g_matchIdChars) - 1)];
	}
	buf[MATCH_ID_RAND_LEN] = 0;

	formatex(raw, charsmax(raw), "%d%s", get_systime(), buf);
	hash_string(raw, Hash_Md5, g_szMatchId, charsmax(g_szMatchId));
}

public plugin_init() {
	register_plugin(PLUGIN_NAME, PLUGIN_VERSION, PLUGIN_AUTHOR);

	register_clcmd("say .start", "CmdStart");
	register_clcmd("say_team .start", "CmdStart");
	register_clcmd("say .start_bot", "CmdStartBot");
	register_clcmd("say_team .start_bot", "CmdStartBot");
	register_clcmd("say .start_bot_test", "CmdStartBotTest");
	register_clcmd("say_team .start_bot_test", "CmdStartBotTest");
	register_clcmd("say .stop", "CmdStop");
	register_clcmd("say_team .stop", "CmdStop");
	register_clcmd("say .refresh", "CmdRefresh");
	register_clcmd("say_team .refresh", "CmdRefresh");
	register_clcmd("say .hardreset", "CmdHardReset");
	register_clcmd("say_team .hardreset", "CmdHardReset");
	register_clcmd("say h", "CmdHelp");
	register_clcmd("say_team h", "CmdHelp");
	register_clcmd("say", "CmdSay");
	register_clcmd("say_team", "CmdSay");

	register_concmd("mr1v1_start", "CmdConsoleStart", 0, "- 开始一局1v1武器轮换比赛（先进入热身）");
	register_concmd("mr1v1_start_bot_test", "CmdConsoleStartBotTest", 0, "- 补一个专家Bot并以加速模式开始比赛(测试用)");
	register_concmd("mr1v1_stop", "CmdConsoleStop", 0, "- 停止当前进行中的比赛");

	// 比赛模式下由平台agent在比赛结束(mr1v1_match_end)后通过RCON触发，
	// 广播倒计时后踢出所有玩家，agent随后docker stop/rm销毁本容器
	register_srvcmd("mr1v1_match_destroy", "CmdRconDestroy");

	RegisterHookChain(RG_CBasePlayer_Spawn, "CBasePlayer_Spawn_Post", true);
	RegisterHookChain(RG_RoundEnd, "RoundEnd_Post", true);
	RegisterHookChain(RG_HandleMenu_ChooseTeam, "HandleMenu_ChooseTeam_Pre", false);
	set_task(1.0, "Task_EnforceSpectators", _, _, _, "b");

	g_hudSync = CreateHudSyncObj();
	g_hudWarmupSync = CreateHudSyncObj();
	g_hFwdMatchEnd = CreateMultiForward("mr1v1_match_end", ET_IGNORE, FP_CELL, FP_CELL, FP_CELL, FP_CELL, FP_CELL);
	// mr1v1_match_start(const match_id[], id0, id1)：比赛开始时广播给其他插件（如mr1v1_telemetry），
	// id0/id1为对战双方的玩家实体id（与TraceAttack等钩子里的实体id一致，全场比赛期间不变）
	g_hFwdMatchStart = CreateMultiForward("mr1v1_match_start", ET_IGNORE, FP_STRING, FP_CELL, FP_CELL);

	g_pCvarWarmupTime = create_cvar("mr1v1_warmup_time", "15", .has_min = true, .min_val = 0.0);
	g_pCvarRoundInfinite = get_cvar_pointer("mp_round_infinite");
	g_pCvarForceRespawn = get_cvar_pointer("mp_forcerespawn");
	g_pCvarRespawnImmunity = get_cvar_pointer("mp_respawn_immunitytime");

	LoadGatewayConfig();
	LoadMatchModeConfig();
	get_mapname(g_szMapName, charsmax(g_szMapName));

	// 每日定时硬重启（容器时区为Asia/Shanghai，见Dockerfile），首次延迟到下一个5点触发
	set_task(float(SecondsUntilNextDailyRestart()), "Task_DailyRestart");
}

// ------------------------------------------------------------
// gateway 配置加载
// ------------------------------------------------------------

LoadGatewayConfig() {
	new fh, path[128], text[128], key[64], value[128];
	get_configsdir(path, charsmax(path));
	formatex(path, charsmax(path), "%s/mr1v1.ini", path);

	if (!file_exists(path)) {
		log_amx("MR1V1: gateway config %s not found, HTTP report disabled", path);
		return;
	}

	fh = fopen(path, "rt");
	if (!fh) {
		return;
	}

	while (!feof(fh)) {
		fgets(fh, text, charsmax(text));
		trim(text);

		if (!strlen(text) || text[0] == ';' || (text[0] == '/' && text[1] == '/')) {
			continue;
		}

		split(text, key, charsmax(key), value, charsmax(value), "=");
		trim(key);
		trim(value);

		if (equali(key, "mr1v1_gateway_http")) {
			copy(g_szGatewayHttp, charsmax(g_szGatewayHttp), value);
		}
	}

	fclose(fh);
	log_amx("MR1V1: gateway http=[%s]", g_szGatewayHttp);
}

// ------------------------------------------------------------
// 比赛模式配置加载（agent按局创建容器时注入，由start.sh写入）
// ------------------------------------------------------------

LoadMatchModeConfig() {
	new fh, path[128], text[128], key[64], value[128];
	get_configsdir(path, charsmax(path));
	formatex(path, charsmax(path), "%s/mr1v1_match_mode.ini", path);

	if (!file_exists(path)) {
		return;
	}

	fh = fopen(path, "rt");
	if (!fh) {
		return;
	}

	while (!feof(fh)) {
		fgets(fh, text, charsmax(text));
		trim(text);

		if (!strlen(text) || text[0] == ';' || (text[0] == '/' && text[1] == '/')) {
			continue;
		}

		split(text, key, charsmax(key), value, charsmax(value), "=");
		trim(key);
		trim(value);

		if (equali(key, "mr1v1_match_id")) {
			copy(g_szMatchModeId, charsmax(g_szMatchModeId), value);
		} else if (equali(key, "mr1v1_p0_steamid")) {
			copy(g_szMatchModeSteamId[0], charsmax(g_szMatchModeSteamId[]), value);
		} else if (equali(key, "mr1v1_p1_steamid")) {
			copy(g_szMatchModeSteamId[1], charsmax(g_szMatchModeSteamId[]), value);
		} else if (equali(key, "mr1v1_bot_test_mode")) {
			g_bBotTestModeEnabled = (str_to_num(value) != 0);
		}
	}

	fclose(fh);

	if (!strlen(g_szMatchModeId) || !strlen(g_szMatchModeSteamId[0]) || !strlen(g_szMatchModeSteamId[1])) {
		log_amx("MR1V1_MATCH_MODE_CONFIG_INCOMPLETE match_id=%s p0=%s p1=%s",
			g_szMatchModeId, g_szMatchModeSteamId[0], g_szMatchModeSteamId[1]);
		return;
	}

	g_bMatchModeEnabled = true;

	if (g_bBotTestModeEnabled) {
		// LoadMatchModeConfig()在plugin_init阶段执行，此时server.cfg还没跑完、
		// 服务器尚未完全起来，直接设bot_quota会导致引擎提前补Bot失败
		// (ReAPI报"player is not connected"再被引擎按quota踢掉)，延迟到Task里设置
		set_task(3.0, "Task_EnableBotTestMode");
		log_amx("MR1V1_BOT_TEST_MODE_ENABLED match_id=%s", g_szMatchModeId);
	} else {
		set_cvar_num("bot_quota", 0);
	}

	// 第1局T/CT分边随机决定一次，之后每回合互换沿用既有逻辑(RoundEnd_Post)
	if (random(2) == 0) {
		g_eMatchModeTeam[0] = TEAM_TERRORIST;
		g_eMatchModeTeam[1] = TEAM_CT;
	} else {
		g_eMatchModeTeam[0] = TEAM_CT;
		g_eMatchModeTeam[1] = TEAM_TERRORIST;
	}

	log_amx("MR1V1_MATCH_MODE_ENABLED match_id=%s p0=%s p1=%s",
		g_szMatchModeId, g_szMatchModeSteamId[0], g_szMatchModeSteamId[1]);
}

// ------------------------------------------------------------
// 启动命令
// ------------------------------------------------------------

public CmdStart(const id) {
	if (g_bMatchModeEnabled) {
		client_print_color(id, print_team_grey, "^4[1v1] ^1比赛模式：双方玩家就位后将自动开始，无需手动启动");
		return PLUGIN_HANDLED;
	}

	new name[32];
	get_user_name(id, name, charsmax(name));
	log_amx("MR1V1_CMD_START by=%s(%d) match_active=%d", name, id, g_bMatchActive);

	if (g_bMatchActive) {
		client_print_color(id, print_team_grey, "^4[1v1] ^1比赛已经在进行中");
		return PLUGIN_HANDLED;
	}

	g_bTestMode = false;
	StartWarmup();
	return PLUGIN_HANDLED;
}

public CmdConsoleStart(const id, const level, const cid) {
	if (g_bMatchModeEnabled) {
		console_print(id, "[1v1] 比赛模式：双方玩家就位后将自动开始，无需手动启动");
		return PLUGIN_HANDLED;
	}

	if (g_bMatchActive) {
		console_print(id, "[1v1] 比赛已经在进行中");
		return PLUGIN_HANDLED;
	}

	g_bTestMode = false;
	StartWarmup();
	return PLUGIN_HANDLED;
}

public CmdStartBot(const id) { StartWithBot(false, GetMatchRequester(id)); return PLUGIN_HANDLED; }
public CmdStartBotTest(const id) { StartWithBot(true, GetMatchRequester(id)); return PLUGIN_HANDLED; }

public CmdConsoleStartBotTest(const id, const level, const cid) {
	if (g_bMatchActive) {
		console_print(id, "[1v1] 比赛已经在进行中");
		return PLUGIN_HANDLED;
	}

	StartWithBot(true, 0);
	return PLUGIN_HANDLED;
}

// 判断发起.start_bot/.start_bot_test的玩家是否已加入T/CT；
// 若其还是观察者(未选边)，返回0交给StartWithBot视为"无人参赛"，改为补2个Bot对战
GetMatchRequester(const id) {
	new TeamName:team = get_member(id, m_iTeam);
	return (team == TEAM_TERRORIST || team == TEAM_CT) ? id : 0;
}

// .start_bot/.start_bot_test：先清空场上所有旧Bot，再凑够2人对战——
// requester==0(无真人，或发起者本身是观察者)：补2个Bot互打，其余真人均设为观察者；
// requester为已在T/CT的真人：该玩家留下对战，其余真人设为观察者，补1个专家难度Bot陪练；
// testMode=true 时会缩短回合时间/冻结时间，方便快速跑完比赛做测试
StartWithBot(bool:testMode, const requester) {
	if (g_bMatchModeEnabled) {
		client_print_color(0, print_team_grey, "^4[1v1] ^1比赛模式下不支持Bot陪练");
		return;
	}

	if (g_bMatchActive) {
		client_print_color(0, print_team_grey, "^4[1v1] ^1比赛已经在进行中");
		return;
	}

	g_bTestMode = testMode;

	// 先清零bot_quota并踢掉所有旧Bot，避免遗留Bot干扰人数统计和选人
	set_cvar_num("bot_quota", 0);
	KickAllBots();

	new humans[MAX_PLAYERS], numHumans;
	get_players(humans, numHumans, "ch"); // 排除Bot和HLTV，仅统计真人

	new botsNeeded = (numHumans == 0 || requester == 0) ? 2 : 1;
	for (new i = 0; i < numHumans; i++) {
		if (humans[i] != requester && get_member(humans[i], m_iTeam) != TEAM_SPECTATOR) {
			rg_set_user_team(humans[i], TEAM_SPECTATOR, MODEL_AUTO, true, false);
			client_print_color(humans[i], print_team_grey, "^4[1v1] ^1比赛进行中，你已被设为观察者，请等待本局结束");
		}
	}

	// 只通过bot_quota+normal模式让引擎自动补Bot(bot_join_after_player=0立即生效)，
	// 不再额外调用bot_add——经实测bot_quota=N会按队伍各补N个(共2N个)，
	// 叠加手动bot_add会导致补出的Bot数翻倍
	// 改之前先记录原值，比赛结束后(RestoreServerCvars)恢复，避免影响下一局/其他用途
	g_iBotJoinAfterPlayerOld = get_cvar_num("bot_join_after_player");
	get_cvar_string("bot_quota_mode", g_szBotQuotaModeOld, charsmax(g_szBotQuotaModeOld));
	g_iBotDifficultyOld = get_cvar_num("bot_difficulty");
	g_bBotCvarsModified = true;

	set_cvar_num("bot_join_after_player", 0);
	set_cvar_string("bot_quota_mode", "normal");
	set_cvar_num("bot_difficulty", 3);
	set_cvar_num("bot_quota", botsNeeded);

	client_print_color(0, print_team_grey, "^4[1v1] ^1已添加%d个专家难度Bot，准备开始比赛...", botsNeeded);
	set_task(1.5, "Task_StartMatch");
}

public Task_StartMatch() {
	if (g_bTestMode) {
		InitMatch();
	} else {
		StartWarmup();
	}
}

public CmdStop(const id) {
	new name[32];
	get_user_name(id, name, charsmax(name));
	log_amx("MR1V1_CMD_STOP by=%s(%d) match_active=%d", name, id, g_bMatchActive);

	if (!g_bMatchActive) {
		client_print_color(id, print_team_grey, "^4[1v1] ^1当前没有进行中的比赛");
		return PLUGIN_HANDLED;
	}

	AbortMatch("比赛已被停止", "manual_stop");
	return PLUGIN_HANDLED;
}

// .refresh：手动刷新服务器（不换图），用于比赛结束后清理残留实体/比分等场景
public CmdRefresh(const id) {
	if (g_bMatchActive) {
		client_print_color(id, print_team_grey, "^4[1v1] ^1比赛进行中，无法刷新服务器");
		return PLUGIN_HANDLED;
	}

	client_print_color(0, print_team_grey, "^4[1v1] ^1服务器即将刷新");
	RefreshServer();
	return PLUGIN_HANDLED;
}

// .hardreset：硬重启服务器进程（quit），依赖docker的restart:unless-stopped自动拉起，
// 用于sv_restartround无法解决的卡死/异常状态。参照PROCS.PRO的Cmp_Quit实现
public CmdHardReset(const id) {
	if (g_bMatchActive) {
		client_print_color(id, print_team_grey, "^4[1v1] ^1比赛进行中，无法硬重启服务器，请先.stop");
		return PLUGIN_HANDLED;
	}

	client_print_color(0, print_team_grey, "^4[1v1] ^1服务器即将硬重启，约几秒后自动恢复");
	set_task(1.0, "DoHardReset");
	return PLUGIN_HANDLED;
}

public DoHardReset() {
	server_cmd("quit");
	server_exec();
}

// 每天硬重启的目标时间点（容器时区为Asia/Shanghai，见Dockerfile）
const MR1V1_DAILY_RESTART_HOUR = 5;

// 计算距离下一个目标时间点（今天或明天的MR1V1_DAILY_RESTART_HOUR:00:00）还有多少秒
SecondsUntilNextDailyRestart() {
	new szHour[3], szMin[3], szSec[3];
	get_time("%H", szHour, charsmax(szHour));
	get_time("%M", szMin, charsmax(szMin));
	get_time("%S", szSec, charsmax(szSec));

	new curSec = str_to_num(szHour) * 3600 + str_to_num(szMin) * 60 + str_to_num(szSec);
	new targetSec = MR1V1_DAILY_RESTART_HOUR * 3600;

	new delay = targetSec - curSec;
	if (delay <= 0) {
		delay += 86400;
	}
	return delay;
}

// 每日定时硬重启：比赛进行中则推迟5分钟重新检查，避免打断比赛；
// 重启后插件重新加载，plugin_init会重新计算下一次的延迟，不需要在此处重新set_task
public Task_DailyRestart() {
	if (g_bMatchActive) {
		log_amx("MR1V1_DAILY_RESTART_POSTPONED match_active=1");
		set_task(300.0, "Task_DailyRestart");
		return;
	}

	client_print_color(0, print_team_grey, "^4[1v1] ^1服务器每日定时重启，约几秒后自动恢复");
	log_amx("MR1V1_DAILY_RESTART");
	set_task(1.0, "DoHardReset");
}

public CmdConsoleStop(const id, const level, const cid) {
	if (!g_bMatchActive) {
		console_print(id, "[1v1] 当前没有进行中的比赛");
		return PLUGIN_HANDLED;
	}

	AbortMatch("比赛已被停止", "manual_stop");
	return PLUGIN_HANDLED;
}

// ------------------------------------------------------------
// 命令菜单（聊天框输入 h 唤起）
// ------------------------------------------------------------

// h：根据当前比赛状态弹出对应的命令菜单，避免玩家记不住.start/.stop/.guns等聊天指令
public CmdHelp(const id) {
	ShowHelpMenu(id);
	return PLUGIN_HANDLED;
}

ShowHelpMenu(const id) {
	new menu = menu_create("\y[1v1] 命令菜单", "HelpMenuHandler");

	if (!g_bMatchActive) {
		if (g_bMatchModeEnabled) {
			menu_additem(menu, "比赛模式：等待双方玩家就位，将自动开始", "", 0);
		} else {
			menu_additem(menu, "开始比赛 (.start)", "start", 0);
			menu_additem(menu, "开始比赛+Bot陪练 (.start_bot)", "start_bot", 0);
			menu_additem(menu, "测试模式+Bot (.start_bot_test)", "start_bot_test", 0);
			menu_additem(menu, "切换地图 (.map)", "map", 0);
			menu_additem(menu, "刷新服务器 (.refresh)", "refresh", 0);
			menu_additem(menu, "硬重启服务器 (.hardreset)", "hardreset", 0);
		}
	} else {
		menu_additem(menu, "停止比赛 (.stop)", "stop", 0);
	}

	menu_display(id, menu, 0);
}

public HelpMenuHandler(const id, menu, item) {
	if (item == MENU_EXIT) {
		menu_destroy(menu);
		return PLUGIN_HANDLED;
	}

	new access, callback, action[20], dispName[64];
	menu_item_getinfo(menu, item, access, action, charsmax(action), dispName, charsmax(dispName), callback);
	menu_destroy(menu);

	if (equal(action, "start")) {
		CmdStart(id);
	} else if (equal(action, "start_bot")) {
		CmdStartBot(id);
	} else if (equal(action, "start_bot_test")) {
		CmdStartBotTest(id);
	} else if (equal(action, "map")) {
		ShowMapMenu(id);
	} else if (equal(action, "stop")) {
		CmdStop(id);
	} else if (equal(action, "refresh")) {
		CmdRefresh(id);
	} else if (equal(action, "hardreset")) {
		CmdHardReset(id);
	}

	return PLUGIN_HANDLED;
}

// ------------------------------------------------------------
// 换图
// ------------------------------------------------------------

// 内置1v1地图列表（菜单只展示服务器上实际存在的）
new const g_mapList[][] = {
	"aim_map",
	"aim_qpad_2007",
	"aim_sk_ak_m4",
	"ak47_m4a1_dust"
};

// 捕获所有聊天消息，解析 ".map" / ".map <地图名>" 这类命令
public CmdSay(const id) {
	new text[192];
	read_args(text, charsmax(text));
	remove_quotes(text);
	trim(text);

	if (equal(text, ".map")) {
		ShowMapMenu(id);
	} else if (equal(text, ".map ", 5)) {
		CmdMap(id, text[5]);
	} else if (strlen(text) && text[0] == '.' && !equal(text, ".start") && !equal(text, ".start_bot")
		&& !equal(text, ".start_bot_test") && !equal(text, ".stop") && !equal(text, ".refresh")
		&& !equal(text, ".hardreset")) {
		// 调试：记录无法识别的"."开头指令，便于排查玩家输入但插件未响应的情况
		new name[32];
		get_user_name(id, name, charsmax(name));
		log_amx("MR1V1_CMD_UNKNOWN by=%s(%d) text=%s", name, id, text);
	}

	return PLUGIN_CONTINUE;
}

ShowMapMenu(const id) {
	if (g_bMatchActive) {
		client_print_color(id, print_team_grey, "^4[1v1] ^1比赛进行中，无法换图，请先.stop");
		return;
	}

	new menu = menu_create("\y[1v1] 选择地图", "MapMenuHandler");
	for (new i = 0; i < sizeof(g_mapList); i++) {
		if (is_map_valid(g_mapList[i])) {
			menu_additem(menu, g_mapList[i], "", 0);
		}
	}
	menu_display(id, menu, 0);
}

public MapMenuHandler(const id, menu, item) {
	if (item == MENU_EXIT) {
		menu_destroy(menu);
		return PLUGIN_HANDLED;
	}

	new access, callback, info[2], mapname[32];
	menu_item_getinfo(menu, item, access, info, charsmax(info), mapname, charsmax(mapname), callback);
	menu_destroy(menu);

	CmdMap(id, mapname);
	return PLUGIN_HANDLED;
}

CmdMap(const id, const args[]) {
	new mapname[32];
	copy(mapname, charsmax(mapname), args);
	trim(mapname);

	if (!strlen(mapname)) {
		client_print_color(id, print_team_grey, "^4[1v1] ^1用法:.map <地图名>");
		return;
	}

	if (!is_map_valid(mapname)) {
		client_print_color(id, print_team_grey, "^4[1v1] ^1地图%s不存在", mapname);
		return;
	}

	if (g_bMatchActive) {
		client_print_color(id, print_team_grey, "^4[1v1] ^1比赛进行中，无法换图，请先.stop");
		return;
	}

	new name[32];
	get_user_name(id, name, charsmax(name));
	client_print_color(0, print_team_grey, "^4[1v1] ^1%s请求切换地图到%s，3秒后切换...", name, mapname);
	log_amx("MR1V1_MAP_CHANGE map=%s by=%s(%d)", mapname, name, id);

	set_task(3.0, "Task_ChangeMap", _, mapname, strlen(mapname) + 1);
}

public Task_ChangeMap(const mapname[]) {
	server_cmd("changelevel %s", mapname);
}

// ------------------------------------------------------------
// 热身环节
// ------------------------------------------------------------

// 进入热身：选定双方玩家，开放购买/拾取/丢弃，无限重生自由练习，
// 固定时长倒计时结束后自动进入正式比赛
StartWarmup() {
	if (!SelectMatchPlayers()) {
		return;
	}

	g_bMatchActive = true;
	g_bWarmupActive = true;
	g_bPendingReconnect[0] = false;
	g_bPendingReconnect[1] = false;
	g_iWins[0] = 0;
	g_iWins[1] = 0;

	// 热身期间开启无限重生，参数沿用PROCS.PRO warmup.sma参考实现：
	// forcerespawn=3 + respawn_immunitytime=3（复活后3秒无敌），
	// 避免Bot高频死亡/复活导致内部寻路缓存悬空指针SIGSEGV
	get_pcvar_string(g_pCvarRoundInfinite, g_szRoundInfiniteOld, charsmax(g_szRoundInfiniteOld));
	g_iForceRespawnOld = get_pcvar_num(g_pCvarForceRespawn);
	g_flRespawnImmunityOld = get_pcvar_float(g_pCvarRespawnImmunity);

	set_pcvar_string(g_pCvarRoundInfinite, "1");
	set_pcvar_num(g_pCvarForceRespawn, 3);
	set_pcvar_float(g_pCvarRespawnImmunity, 3.0);

	set_member_game(m_bCompleteReset, true);
	set_member_game(m_bGameStarted, true);
	rg_round_end(0.5, WINSTATUS_DRAW, ROUND_GAME_COMMENCE, "");

	g_iWarmupSecLeft = get_pcvar_num(g_pCvarWarmupTime);
	set_task(1.0, "Task_WarmupTimer", TASK_WARMUP_TIMER, _, _, "b");

	new name1[32], name2[32];
	get_user_name(g_iPlayer[0], name1, charsmax(name1));
	get_user_name(g_iPlayer[1], name2, charsmax(name2));

	client_print_color(0, print_team_grey, "^4[1v1] ^1热身开始！%d秒后比赛开始", g_iWarmupSecLeft);
	log_amx("MR1V1_WARMUP_START p0=%d(%s) p1=%d(%s) time=%d", g_iPlayer[0], name1, g_iPlayer[1], name2, g_iWarmupSecLeft);

	RefreshVoiceState();
}

public Task_WarmupTimer() {
	if (!g_bWarmupActive) {
		return;
	}

	g_iWarmupSecLeft--;
	if (g_iWarmupSecLeft <= 0) {
		EndWarmup();
		return;
	}

	for (new slot = 0; slot < 2; slot++) {
		new id = g_iPlayer[slot];
		if (!is_user_connected(id)) {
			continue;
		}
		set_hudmessage(135, 206, 235, -1.0, 0.15, 0, 0.0, 1.1, 0.0, 0.0, -1);
		ShowSyncHudMsg(id, g_hudWarmupSync, "热身中: %d秒后比赛开始", g_iWarmupSecLeft);
	}
}

// 热身结束：恢复服务器cvar，进入正式比赛
EndWarmup() {
	remove_task(TASK_WARMUP_TIMER);

	set_pcvar_string(g_pCvarRoundInfinite, g_szRoundInfiniteOld);
	set_pcvar_num(g_pCvarForceRespawn, g_iForceRespawnOld);
	set_pcvar_float(g_pCvarRespawnImmunity, g_flRespawnImmunityOld);

	g_bWarmupActive = false;
	g_bMatchActive = false;

	client_print_color(0, print_team_grey, "^4[1v1] ^1热身结束，比赛即将开始！");
	log_amx("MR1V1_WARMUP_END");

	// 双方玩家及阵营已在 StartWarmup -> SelectMatchPlayers 中确定，
	// 此处不再重新选人/调用 rg_set_user_team：对已运行了一段时间(有AI状态)的Bot
	// 再次同步切换队伍会导致Bot AI缓存的nav area/hiding spot指针悬空而SIGSEGV
	InitMatch(false);
}

// ------------------------------------------------------------
// 比赛流程
// ------------------------------------------------------------

// 为Bot生成唯一ID(BOT_<userid>_<随机数>)，替代固定的"BOT"，避免上报数据中authid冲突
GenerateBotAuthId(id, buf[], len) {
	formatex(buf, len, "BOT_%d_%06d", get_user_userid(id), random_num(0, 999999));
}

// 比赛模式：双方指定steamid玩家已通过client_putinserver自动分队完毕，
// 直接采用其当前连接id/阵营，不做随机抽取
bool:SelectMatchModePlayers() {
	if (!g_bMatchModeConnected[0] || !g_bMatchModeConnected[1]
		|| !is_user_connected(g_iMatchModePlayer[0]) || !is_user_connected(g_iMatchModePlayer[1])) {
		log_amx("MR1V1_MATCH_MODE_SELECT_FAIL connected0=%d connected1=%d", g_bMatchModeConnected[0], g_bMatchModeConnected[1]);
		return false;
	}

	g_iPlayer[0] = g_iMatchModePlayer[0];
	g_iPlayer[1] = g_iMatchModePlayer[1];
	copy(g_szAuthId[0], charsmax(g_szAuthId[]), g_szMatchModeSteamId[0]);
	copy(g_szAuthId[1], charsmax(g_szAuthId[]), g_szMatchModeSteamId[1]);
	g_eCurrentTeam[0] = g_eMatchModeTeam[0];
	g_eCurrentTeam[1] = g_eMatchModeTeam[1];

	new selName0[32], selName1[32];
	get_user_name(g_iPlayer[0], selName0, charsmax(selName0));
	get_user_name(g_iPlayer[1], selName1, charsmax(selName1));
	copy(g_szPlayerName[0], charsmax(g_szPlayerName[]), selName0);
	copy(g_szPlayerName[1], charsmax(g_szPlayerName[]), selName1);

	rg_set_user_team(g_iPlayer[0], g_eCurrentTeam[0], MODEL_AUTO, true, false);
	rg_set_user_team(g_iPlayer[1], g_eCurrentTeam[1], MODEL_AUTO, true, false);

	log_amx("MR1V1_MATCH_MODE_SELECT_RESULT p0=%d(%s,%s) p1=%d(%s,%s)",
		g_iPlayer[0], selName0, g_szAuthId[0], g_iPlayer[1], selName1, g_szAuthId[1]);

	return true;
}

// 选定本局对战的两名玩家：仅从已加入T/CT的玩家中随机抽取两人，
// 仍在选队菜单(TEAM_UNASSIGNED)或已是观察者的玩家不参与抽取，多余玩家强制设为观察者
// 设置 g_iPlayer/g_szAuthId/g_eCurrentTeam，并重置武器选择记忆
bool:SelectMatchPlayers() {
	if (g_bMatchModeEnabled) {
		return SelectMatchModePlayers();
	}

	new players[MAX_PLAYERS], num;
	get_players(players, num, "h");

	// 候选人仅限已实际加入T或CT的玩家(含Bot)；观察者及未选队玩家不参与配对
	new candidates[MAX_PLAYERS], numCandidates;
	for (new i = 0; i < num; i++) {
		new TeamName:team = get_member(players[i], m_iTeam);
		if (team == TEAM_TERRORIST || team == TEAM_CT) {
			candidates[numCandidates++] = players[i];
		}
	}

	// 调试：记录候选玩家及其当前队伍，便于排查"选错人/漏选人"的问题
	for (new i = 0; i < num; i++) {
		new dbgName[32];
		get_user_name(players[i], dbgName, charsmax(dbgName));
		log_amx("MR1V1_SELECT_CANDIDATE id=%d(%s) team=%d", players[i], dbgName, get_member(players[i], m_iTeam));
	}

	if (numCandidates < 2) {
		client_print_color(0, print_team_grey, "^4[1v1] ^1玩家数量不足，至少需要2名玩家（含Bot）已加入T/CT才能开始");
		log_amx("MR1V1_SELECT_FAIL numCandidates=%d", numCandidates);
		return false;
	}

	// 从候选人中随机抽取两人作为本局对战玩家(Fisher-Yates局部打乱)
	for (new i = numCandidates - 1; i > 0; i--) {
		new j = random_num(0, i);
		new tmp = candidates[i];
		candidates[i] = candidates[j];
		candidates[j] = tmp;
	}

	g_iPlayer[0] = candidates[0];
	g_iPlayer[1] = candidates[1];

	get_user_authid(g_iPlayer[0], g_szAuthId[0], charsmax(g_szAuthId[]));
	get_user_authid(g_iPlayer[1], g_szAuthId[1], charsmax(g_szAuthId[]));

	// Bot的get_user_authid固定返回"BOT"，多个Bot对局会冲突，
	// 生成形如 BOT_<userid>_<随机数> 的唯一ID替代，仅用于上报区分
	if (is_user_bot(g_iPlayer[0])) {
		GenerateBotAuthId(g_iPlayer[0], g_szAuthId[0], charsmax(g_szAuthId[]));
	}
	if (is_user_bot(g_iPlayer[1])) {
		GenerateBotAuthId(g_iPlayer[1], g_szAuthId[1], charsmax(g_szAuthId[]));
	}

	new selName0[32], selName1[32];
	get_user_name(g_iPlayer[0], selName0, charsmax(selName0));
	get_user_name(g_iPlayer[1], selName1, charsmax(selName1));
	copy(g_szPlayerName[0], charsmax(g_szPlayerName[]), selName0);
	copy(g_szPlayerName[1], charsmax(g_szPlayerName[]), selName1);
	log_amx("MR1V1_SELECT_RESULT p0=%d(%s,%s) p1=%d(%s,%s)",
		g_iPlayer[0], selName0, g_szAuthId[0], g_iPlayer[1], selName1, g_szAuthId[1]);

	if (random(2) == 0) {
		g_eCurrentTeam[0] = TEAM_TERRORIST;
		g_eCurrentTeam[1] = TEAM_CT;
	} else {
		g_eCurrentTeam[0] = TEAM_CT;
		g_eCurrentTeam[1] = TEAM_TERRORIST;
	}

	rg_set_user_team(g_iPlayer[0], g_eCurrentTeam[0], MODEL_AUTO, true, false);
	rg_set_user_team(g_iPlayer[1], g_eCurrentTeam[1], MODEL_AUTO, true, false);

	// 比赛开始前已在场但未入选的多余玩家，强制设为观察者
	for (new i = 0; i < num; i++) {
		if (players[i] != g_iPlayer[0] && players[i] != g_iPlayer[1]
			&& get_member(players[i], m_iTeam) != TEAM_SPECTATOR) {
			rg_set_user_team(players[i], TEAM_SPECTATOR, MODEL_AUTO, true, false);
			client_print_color(players[i], print_team_grey, "^4[1v1] ^1比赛进行中，你已被设为观察者，请等待本局结束");
		}
	}

	return true;
}

// 比赛开始时一次性公示规则：捡枪式，无强制武器
AnnounceMatchRules() {
	client_print_color(0, print_team_red, "^4[1v1] ^1本场比赛决胜方式：^3最多31回合，先到16胜");
	client_print_color(0, print_team_grey, "^4[1v1] ^1不发武器不限购买，一切按地图默认来，能买就买，捡到什么用什么");
	client_print_color(0, print_team_grey, "^4[1v1] ^1聊天框输入h可随时弹出命令菜单");
}

InitMatch(bool:selectPlayers = true) {
	if (selectPlayers && !SelectMatchPlayers()) {
		return;
	}

	g_iWins[0] = 0;
	g_iWins[1] = 0;
	g_iRoundNum = 1;
	g_bMatchActive = true;
	g_bWarmupActive = false;
	g_bPendingReconnect[0] = false;
	g_bPendingReconnect[1] = false;

	GenerateMatchId();

	new name1[32], name2[32];
	get_user_name(g_iPlayer[0], name1, charsmax(name1));
	get_user_name(g_iPlayer[1], name2, charsmax(name2));

	AnnounceMatchRules();
	log_amx("MR1V1_MATCH_START match_id=%s p0=%d(%s) p1=%d(%s)", g_szMatchId, g_iPlayer[0], name1, g_iPlayer[1], name2);

	if (g_bTestMode) {
		set_cvar_num("mp_freezetime", 1);
		set_cvar_float("mp_roundtime", 0.5);
	} else {
		set_cvar_num("mp_freezetime", 1);
	}

	set_member_game(m_bCompleteReset, true);
	set_member_game(m_bGameStarted, true);
	rg_round_end(1.0, WINSTATUS_DRAW, ROUND_GAME_COMMENCE, "");

	new retFwd;
	ExecuteForward(g_hFwdMatchStart, retFwd, g_szMatchId, g_iPlayer[0], g_iPlayer[1]);

	ReportMatchStart();
	RefreshVoiceState();
}

bool:IsMatchPlayer(const id) {
	return (g_bMatchActive && (id == g_iPlayer[0] || id == g_iPlayer[1]));
}

// 语音权限矩阵：比赛(含热身)进行中，只有T/CT两名对战玩家的语音可被所有人听到，
// 观察者的语音不会被任何人听到；非比赛状态下恢复全员互听(all-talk)
RefreshVoiceState() {
	new players[MAX_PLAYERS], num;
	get_players(players, num, "h");

	for (new i = 0; i < num; i++) {
		new sender = players[i];
		new bool:senderCanBeHeard = !g_bMatchActive || IsMatchPlayer(sender);

		for (new j = 0; j < num; j++) {
			new receiver = players[j];
			if (receiver == sender) {
				continue;
			}
			EF_SetClientListening(receiver, sender, senderCanBeHeard);
		}
	}
}

GetSlot(const id) {
	return (id == g_iPlayer[0]) ? 0 : 1;
}

// ------------------------------------------------------------
// 出生装备
// ------------------------------------------------------------

public CBasePlayer_Spawn_Post(const id) {
	if (!IsMatchPlayer(id) || !is_user_alive(id)) {
		return;
	}

	if (g_bWarmupActive) {
		// 热身不强制发装备，跟正式比赛一样交给引擎/地图默认
		return;
	} else {
		// 换边后在重生时刷新模型与记分板队伍颜色，此时旧尸体已不存在，无违和感；
		// 出生装备完全交给引擎/地图默认，不再由插件强制发武器
		new before = get_member(id, m_iTeam);
		rg_set_user_team(id, g_eCurrentTeam[GetSlot(id)], MODEL_AUTO, true, false);
		log_amx("MR1V1_SPAWN round=%d slot=%d id=%d team_before=%d team_after=%d",
			g_iRoundNum, GetSlot(id), id, before, _:g_eCurrentTeam[GetSlot(id)]);
	}
}

// ------------------------------------------------------------
// 回合结束：计分 / 换边
// ------------------------------------------------------------

public RoundEnd_Post(WinStatus:status, ScenarioEventEndRound:event) {
	if (!g_bMatchActive || g_bWarmupActive || event == ROUND_GAME_COMMENCE) {
		return HC_CONTINUE;
	}

	new winnerSlot = -1;
	if (status == WINSTATUS_TERRORISTS) {
		winnerSlot = (g_eCurrentTeam[0] == TEAM_TERRORIST) ? 0 : 1;
	} else if (status == WINSTATUS_CTS) {
		winnerSlot = (g_eCurrentTeam[0] == TEAM_CT) ? 0 : 1;
	}

	if (winnerSlot != -1) {
		g_iWins[winnerSlot]++;
	}

	ShowScore();

	g_iLastRoundNum = g_iRoundNum;
	g_iLastWinnerSlot = winnerSlot;
	set_task(0.3, "Task_RoundDamage");

	log_amx("MR1V1 round_end round=%d status=%d winner_slot=%d wins=%d:%d",
		g_iRoundNum, _:status, winnerSlot, g_iWins[0], g_iWins[1]);

	if (g_iRoundNum >= TOTAL_ROUNDS || g_iWins[0] >= WIN_THRESHOLD || g_iWins[1] >= WIN_THRESHOLD) {
		UpdateNativeTeamScores();
		// 比Task_RoundDamage(0.3s)晚0.1秒执行，确保决胜局伤害先上报完，
		// 清理动作(ResetMatchState清空g_iPlayer/踢Bot)再发生，见Task_AnnounceMatchResult注释
		set_task(0.4, "Task_AnnounceMatchResult");
		return HC_CONTINUE;
	}

	// 延迟到下一帧再换边：RG_RoundEnd post钩子触发时引擎/Bot AI仍在处理本帧的回合结束逻辑，
	// 此时同步调用 rg_set_user_team 切换Bot队伍会让Bot AI缓存的nav area/hiding spot指针悬空，
	// 在Bot下一次AI思考时(如FindNearbyRetreatSpot)解引用野指针导致SIGSEGV崩溃
	set_task(0.0, "Task_AfterRoundEnd");

	return HC_CONTINUE;
}

public Task_AfterRoundEnd() {
	SwapSides();

	g_iRoundNum++;
}

// 每回合结束后无论胜负都换边：仅切换内部队伍标记和出生点归属(MODEL_UNASSIGNED不刷新模型)，
// 避免刚阵亡玩家的尸体瞬间变换皮肤；模型刷新延后到下回合 CBasePlayer_Spawn_Post 中进行
SwapSides() {
	new TeamName:tmp = g_eCurrentTeam[0];
	g_eCurrentTeam[0] = g_eCurrentTeam[1];
	g_eCurrentTeam[1] = tmp;

	rg_set_user_team(g_iPlayer[0], g_eCurrentTeam[0], MODEL_UNASSIGNED, false, false);
	rg_set_user_team(g_iPlayer[1], g_eCurrentTeam[1], MODEL_UNASSIGNED, false, false);

	log_amx("MR1V1_SWAP round=%d new_team0=%d new_team1=%d wins=%d:%d",
		g_iRoundNum, _:g_eCurrentTeam[0], _:g_eCurrentTeam[1], g_iWins[0], g_iWins[1]);

	UpdateNativeTeamScores();
}

// 把记分板T/CT回合数覆盖为"当前在该边的玩家的个人胜场数"，
// 使Tab记分板显示与自定义HUD比分始终一致
UpdateNativeTeamScores() {
	new ctsWins, tsWins;
	if (g_eCurrentTeam[0] == TEAM_CT) {
		ctsWins = g_iWins[0];
		tsWins = g_iWins[1];
	} else {
		ctsWins = g_iWins[1];
		tsWins = g_iWins[0];
	}
	rg_update_teamscores(ctsWins, tsWins, false);
}

// ------------------------------------------------------------
// 本回合伤害统计
// ------------------------------------------------------------

public Task_RoundDamage() {
	if (!g_iPlayer[0] || !g_iPlayer[1] || !is_user_connected(g_iPlayer[0]) || !is_user_connected(g_iPlayer[1])) {
		return;
	}

	new stats0[STATSX_MAX_STATS], body0[MAX_BODYHITS];
	new stats1[STATSX_MAX_STATS], body1[MAX_BODYHITS];

	get_user_vstats(g_iPlayer[0], g_iPlayer[1], stats0, body0);
	get_user_vstats(g_iPlayer[1], g_iPlayer[0], stats1, body1);

	new health1 = is_user_alive(g_iPlayer[1]) ? FormatHealth(get_user_health(g_iPlayer[1])) : 0;
	new health0 = is_user_alive(g_iPlayer[0]) ? FormatHealth(get_user_health(g_iPlayer[0])) : 0;

	// 仅比赛双方各自可见本回合的命中/伤害数据，观察者不显示（格式参考PROCS.PRO procs_pdetails.sma）；
	// 1v1对手唯一，不再显示对方名字
	client_print_color(g_iPlayer[0], print_team_grey, "^1命中^4%d^1次^4%d^1伤害 被击中^4%d^1次^4%d^1伤害 对手剩余^4%d^1HP",
		stats0[STATSX_HITS], FormatDamage(stats0[STATSX_DAMAGE]), stats1[STATSX_HITS], FormatDamage(stats1[STATSX_DAMAGE]), health1);
	client_print_color(g_iPlayer[1], print_team_grey, "^1命中^4%d^1次^4%d^1伤害 被击中^4%d^1次^4%d^1伤害 对手剩余^4%d^1HP",
		stats1[STATSX_HITS], FormatDamage(stats1[STATSX_DAMAGE]), stats0[STATSX_HITS], FormatDamage(stats0[STATSX_DAMAGE]), health0);

	ReportRoundEnd(stats0[STATSX_DAMAGE], stats0[STATSX_HITS], stats1[STATSX_DAMAGE], stats1[STATSX_HITS]);
}

// 与PROCS.PRO procs_pdetails.sma一致：伤害显示上限100，血量下限0
FormatDamage(value) {
	return (value > 100) ? 100 : value;
}

FormatHealth(value) {
	return (value < 0) ? 0 : value;
}

ShowScore() {
	new name1[32], name2[32];
	get_user_name(g_iPlayer[0], name1, charsmax(name1));
	get_user_name(g_iPlayer[1], name2, charsmax(name2));

	set_hudmessage(255, 255, 255, -1.0, 0.15, 0, 6.0, 4.0);
	ShowSyncHudMsg(0, g_hudSync, "第%d/%d回合 %s %d:%d %s", g_iRoundNum, TOTAL_ROUNDS, name1, g_iWins[0], g_iWins[1], name2);
}

// 比0.3秒后执行的Task_RoundDamage晚0.1秒触发，确保决胜局的伤害/命中明细
// 先上报完，再执行本函数里的ResetMatchState()(会清空g_iPlayer/踢Bot)——
// 否则Task_RoundDamage执行时g_iPlayer已被清零，guard直接return，
// 决胜局的round_end遥测会永久丢失（比分本身不受影响，因为走的是
// ReportMatchEnd另一条不依赖g_iPlayer的独立上报路径）
public Task_AnnounceMatchResult() {
	new name1[32], name2[32];
	get_user_name(g_iPlayer[0], name1, charsmax(name1));
	get_user_name(g_iPlayer[1], name2, charsmax(name2));

	new winner = -1;
	if (g_iWins[0] > g_iWins[1]) {
		winner = 0;
	} else if (g_iWins[1] > g_iWins[0]) {
		winner = 1;
	}

	set_hudmessage(0, 255, 0, -1.0, 0.35, 0, 6.0, 6.0);
	if (winner == -1) {
		client_print_color(0, print_team_grey, "^4[1v1] ^1比赛结束！平局%d:%d", g_iWins[0], g_iWins[1]);
		ShowSyncHudMsg(0, g_hudSync, "比赛结束！平局 %d:%d", g_iWins[0], g_iWins[1]);
	} else {
		new winnerName[32];
		get_user_name(g_iPlayer[winner], winnerName, charsmax(winnerName));
		client_print_color(0, print_team_grey, "^4[1v1] ^1比赛结束！%s获胜！比分%d:%d", winnerName, g_iWins[0], g_iWins[1]);
		ShowSyncHudMsg(0, g_hudSync, "比赛结束！%s 获胜 %d:%d", winnerName, g_iWins[0], g_iWins[1]);

		new loser = 1 - winner;
		client_print_color(g_iPlayer[loser], print_team_grey, "^4[1v1] ^1很遗憾你失败了，别气馁，再来一把干回来！");
	}

	log_amx("MR1V1_MATCH_END match_id=%s winner_slot=%d score=%d:%d p0=%d(%s) p1=%d(%s)",
		g_szMatchId, winner, g_iWins[0], g_iWins[1], g_iPlayer[0], name1, g_iPlayer[1], name2);

	new ret;
	ExecuteForward(g_hFwdMatchEnd, ret, winner, g_iWins[0], g_iWins[1],
		get_user_userid(g_iPlayer[0]), get_user_userid(g_iPlayer[1]));

	ReportMatchEnd(winner, name1, name2, "normal");

	RestoreServerCvars();
	g_bMatchActive = false;
	RefreshVoiceState();

	ResetMatchState();
}

RestoreServerCvars() {
	set_cvar_num("mp_freezetime", 0);
	if (g_bTestMode) {
		set_cvar_float("mp_roundtime", 1.0);
		g_bTestMode = false;
	}

	if (g_bWarmupActive) {
		remove_task(TASK_WARMUP_TIMER);
		set_pcvar_string(g_pCvarRoundInfinite, g_szRoundInfiniteOld);
		set_pcvar_num(g_pCvarForceRespawn, g_iForceRespawnOld);
		set_pcvar_float(g_pCvarRespawnImmunity, g_flRespawnImmunityOld);
		g_bWarmupActive = false;
	}

	// StartWithBot()改过的bot相关cvar恢复成开始前的值（bot_quota本身在ResetMatchState里清零）
	if (g_bBotCvarsModified) {
		set_cvar_num("bot_join_after_player", g_iBotJoinAfterPlayerOld);
		set_cvar_string("bot_quota_mode", g_szBotQuotaModeOld);
		set_cvar_num("bot_difficulty", g_iBotDifficultyOld);
		g_bBotCvarsModified = false;
	}
}

// 中止比赛并恢复服务器状态（.stop / 掉线超时 / Bot掉线 等场景共用）：
// 移除比赛Bot、清空比分/回合/阶段等状态，并刷新地图让服务器回到干净的初始状态
// endReason为上报用的机器可读原因：manual_stop / disconnect / disconnect_timeout
AbortMatch(const reason[], const endReason[]) {
	client_print_color(0, print_team_grey, "^4[1v1] ^1%s，服务器即将刷新", reason);
	log_amx("MR1V1_MATCH_ABORT reason=%s end_reason=%s", reason, endReason);

	ReportMatchEnd(-1, g_szPlayerName[0], g_szPlayerName[1], endReason);

	RestoreServerCvars();
	g_bMatchActive = false;
	g_bPendingReconnect[0] = false;
	g_bPendingReconnect[1] = false;
	RefreshVoiceState();

	ResetMatchState();
}

// 清空比分/回合等比赛状态，移除比赛Bot，并刷新当前地图（不换图）
// 比赛正常结束(Task_AnnounceMatchResult)和被中止(.stop/AbortMatch)后共用
ResetMatchState() {
	g_iPlayer[0] = 0;
	g_iPlayer[1] = 0;
	g_iWins[0] = 0;
	g_iWins[1] = 0;
	g_iRoundNum = 0;
	g_bPendingReconnect[0] = false;
	g_bPendingReconnect[1] = false;

	// 先清零bot_quota，否则.start_bot流程设的quota=1会让引擎在KickAllBots后立刻补一个新Bot
	set_cvar_num("bot_quota", 0);
	KickAllBots();

	RefreshServer();
}

// 不换图刷新服务器：清空残留实体/掉落武器/比分显示，恢复到干净的初始状态
// 参照PROCS.PRO的Cmp_Server_RestartRound实现
RefreshServer() {
	server_cmd("sv_restartround 1");
	server_exec();
}

// 踢出所有Bot（.stop / 中止比赛时清理比赛用Bot）
KickAllBots() {
	new bots[MAX_PLAYERS], numBots;
	get_players(bots, numBots, "d");
	for (new i = 0; i < numBots; i++) {
		server_cmd("kick #%d", get_user_userid(bots[i]));
	}
}

// ------------------------------------------------------------
// 容器销毁倒计时（RCON触发）
// 比赛模式下，平台agent在收到mr1v1_match_end上报后通过RCON执行
// mr1v1_match_destroy，本插件广播倒计时后踢出所有玩家，
// agent随后docker stop/rm销毁本容器并释放端口
// ------------------------------------------------------------

#define TASK_DESTROY_COUNTDOWN  9999
const MR1V1_DESTROY_COUNTDOWN_SECONDS = 5;
new g_iDestroyCountdown;

public CmdRconDestroy(const id, const level, const cid) {
	log_amx("MR1V1_RCON_DESTROY");

	g_iDestroyCountdown = MR1V1_DESTROY_COUNTDOWN_SECONDS;
	set_task(1.0, "Task_DestroyCountdown", TASK_DESTROY_COUNTDOWN, _, _, "b");
	return PLUGIN_HANDLED;
}

public Task_DestroyCountdown() {
	if (g_iDestroyCountdown <= 0) {
		remove_task(TASK_DESTROY_COUNTDOWN);

		new players[MAX_PLAYERS], num;
		get_players(players, num);
		for (new i = 0; i < num; i++) {
			server_cmd("kick #%d", get_user_userid(players[i]));
		}
		server_exec();
		return;
	}

	client_print_color(0, print_team_grey, "^4[1v1] ^1服务器即将关闭，剩余%d秒", g_iDestroyCountdown);
	g_iDestroyCountdown--;
}

// ------------------------------------------------------------
// 玩家断线 / 重连 / 旁观者处理
// ------------------------------------------------------------

public client_disconnected(id, bool:drop, message[], maxlen) {
	if (!IsMatchPlayer(id)) {
		return;
	}

	new slot = GetSlot(id);
	g_iPlayer[slot] = 0;

	// Bot没有"重连"概念，直接中止比赛
	if (equal(g_szAuthId[slot], "BOT")) {
		AbortMatch("玩家掉线，比赛已取消", "disconnect");
		return;
	}

	g_bPendingReconnect[slot] = true;
	client_print_color(0, print_team_grey, "^4[1v1] ^1玩家掉线，等待60秒重连，比赛暂不取消");
	log_amx("MR1V1_MATCH_PLAYER_DROP slot=%d authid=%s", slot, g_szAuthId[slot]);
	set_task(60.0, "Task_AbortIfNoReconnect", slot + 1000);
}

public Task_AbortIfNoReconnect(slotPlus) {
	new slot = slotPlus - 1000;
	if (g_bMatchActive && g_bPendingReconnect[slot]) {
		AbortMatch("玩家掉线超时未重连，比赛已取消", "disconnect_timeout");
	}
}

// Bot顶替测试模式：服务器完全起来(server.cfg跑完)之后才设bot_quota，
// 让引擎补Bot；补出的Bot在client_putinserver里顶替双方slot(见该函数)
public Task_EnableBotTestMode() {
	set_cvar_num("bot_join_after_player", 0);
	set_cvar_string("bot_quota_mode", "normal");
	set_cvar_num("bot_difficulty", 3);
	set_cvar_num("bot_quota", 2);
}

// 比赛模式：本局指定的双方玩家均已连入并入座对应阵营，自动进入热身->开局
public Task_StartMatchMode() {
	if (g_bMatchActive) {
		return;
	}

	client_print_color(0, print_team_grey, "^4[1v1] ^1比赛模式：双方玩家已就位，准备开始");
	log_amx("MR1V1_MATCH_MODE_BOTH_CONNECTED");

	g_bTestMode = false;
	StartWarmup();
}

// 根据SteamID识别掉线玩家重连，恢复其在比赛中的位置；
// 比赛模式下还需识别本局指定的双方玩家是否已连入，凑齐后自动开局
public client_authorized(id, const authid[]) {
	if (g_bMatchModeEnabled && !g_bMatchActive) {
		for (new slot = 0; slot < 2; slot++) {
			if (!g_bMatchModeConnected[slot] && equal(g_szMatchModeSteamId[slot], authid)) {
				g_bMatchModeConnected[slot] = true;
				g_iMatchModePlayer[slot] = id;
				log_amx("MR1V1_MATCH_MODE_PLAYER_JOIN slot=%d authid=%s id=%d", slot, authid, id);

				if (g_bMatchModeConnected[0] && g_bMatchModeConnected[1]) {
					set_task(1.0, "Task_StartMatchMode");
				}
				return;
			}
		}
	}

	if (!g_bMatchActive) {
		return;
	}

	for (new slot = 0; slot < 2; slot++) {
		if (g_bPendingReconnect[slot] && equal(g_szAuthId[slot], authid)) {
			g_iPlayer[slot] = id;
			g_bPendingReconnect[slot] = false;
			client_print_color(0, print_team_grey, "^4[1v1] ^1玩家已重连，比赛继续");
			log_amx("MR1V1_MATCH_PLAYER_RECONNECT slot=%d", slot);
			return;
		}
	}
}

// 比赛进行中，重连的对战玩家恢复队伍；非对战的第三人强制设为观察者；
// 比赛模式下、比赛尚未开始前，本局指定玩家自动入座对应阵营，其他人强制观察者
public client_putinserver(id) {
	if (g_bMatchModeEnabled && !g_bMatchActive && is_user_bot(id)) {
		// Bot顶替测试模式：先到的Bot顶替slot0，后到的顶替slot1，无需匹配steamid
		if (g_bBotTestModeEnabled) {
			// 此时ReAPI对该Bot的内部"已连接"状态可能还没就绪，rg_set_user_team
			// 会报"player is not connected"；team由SelectMatchModePlayers()
			// 在Task_StartMatchMode之后统一设置，这里只记录slot绑定
			for (new slot = 0; slot < 2; slot++) {
				if (!g_bMatchModeConnected[slot]) {
					g_bMatchModeConnected[slot] = true;
					g_iMatchModePlayer[slot] = id;
					log_amx("MR1V1_BOT_TEST_MODE_PLAYER_JOIN slot=%d id=%d", slot, id);

					if (g_bMatchModeConnected[0] && g_bMatchModeConnected[1]) {
						set_task(1.0, "Task_StartMatchMode");
					}
					break;
				}
			}
		}
		return;
	}

	if (g_bMatchModeEnabled && !g_bMatchActive && !is_user_bot(id)) {
		new authid[35];
		get_user_authid(id, authid, charsmax(authid));

		for (new slot = 0; slot < 2; slot++) {
			if (equal(g_szMatchModeSteamId[slot], authid)) {
				rg_set_user_team(id, g_eMatchModeTeam[slot], MODEL_AUTO, true, false);
				client_print_color(id, print_team_grey, "^4[1v1] ^1比赛模式：等待双方玩家就位后自动开始");
				return;
			}
		}

		rg_set_user_team(id, TEAM_SPECTATOR, MODEL_AUTO, true, false);
		client_print_color(id, print_team_grey, "^4[1v1] ^1本服务器为指定对局专用，你已被设为观察者");
		return;
	}

	if (!g_bMatchActive || is_user_bot(id)) {
		return;
	}

	if (IsMatchPlayer(id)) {
		rg_set_user_team(id, g_eCurrentTeam[GetSlot(id)], MODEL_AUTO, true, false);
		client_print_color(id, print_team_grey, "^4[1v1] ^1欢迎回来，比赛继续，当前比分%d:%d", g_iWins[0], g_iWins[1]);
		log_amx("MR1V1_PUTINSERVER_MATCHPLAYER id=%d slot=%d team=%d", id, GetSlot(id), _:g_eCurrentTeam[GetSlot(id)]);
	} else {
		rg_set_user_team(id, TEAM_SPECTATOR, MODEL_AUTO, true, false);
		client_print_color(id, print_team_grey, "^4[1v1] ^1比赛进行中，你已被设为观察者，请等待本局结束");
	}

	RefreshVoiceState();
}

// 比赛进行中，非对战玩家不允许通过队伍菜单/jointeam脱离观察者
public HandleMenu_ChooseTeam_Pre(const id, const MenuChooseTeam:slot) {
	if (g_bMatchActive && !is_user_bot(id) && !IsMatchPlayer(id)) {
		client_print_color(id, print_team_grey, "^4[1v1] ^1比赛进行中，你只能观察，请等待本局结束");
		new name[32];
		get_user_name(id, name, charsmax(name));
		log_amx("MR1V1_TEAMMENU_BLOCKED by=%s(%d) requested_slot=%d", name, id, _:slot);
		return HC_SUPERCEDE;
	}
	return HC_CONTINUE;
}

// 持续兜底：把所有非对战的人类玩家强制打回观察者
public Task_EnforceSpectators() {
	if (!g_bMatchActive) {
		return;
	}

	new players[MAX_PLAYERS], num;
	get_players(players, num, "h");

	for (new i = 0; i < num; i++) {
		if (!is_user_bot(players[i]) && !IsMatchPlayer(players[i])
			&& get_member(players[i], m_iTeam) != TEAM_SPECTATOR) {
			new teamBefore = get_member(players[i], m_iTeam);
			rg_set_user_team(players[i], TEAM_SPECTATOR, MODEL_AUTO, true, false);
			client_print_color(players[i], print_team_grey, "^4[1v1] ^1比赛进行中，你已被设为观察者，请等待本局结束");

			new name[32];
			get_user_name(players[i], name, charsmax(name));
			log_amx("MR1V1_ENFORCE_SPECTATOR id=%d(%s) team_before=%d", players[i], name, teamBefore);
		}
	}

	RefreshVoiceState();
}

// ------------------------------------------------------------
// gateway 上报（HTTP POST /record，JSON信封：timestamp/match_id/type/version/data）
// match_id用于gateway按{prefix}/{match_id}分流MQTT topic，
// 不再使用服务器级token——1v1服务器打完即销毁，match_id本身已是唯一标识
// ------------------------------------------------------------

// data为各事件自行构造的JSON对象，归属权转移给本函数：
// 正常路径下被挂到object下随json_free(object)一起释放；
// gateway未配置(直接返回)的路径下需要单独释放，避免泄漏
ReportEvent(const type[], JSON:data) {
	if (!strlen(g_szGatewayHttp)) {
		json_free(data);
		return;
	}

	new JSON:object = json_init_object();
	json_object_set_number(object, "timestamp", get_systime());
	json_object_set_string(object, "match_id", g_szMatchId);
	json_object_set_string(object, "type", type);
	json_object_set_string(object, "version", PLUGIN_VERSION);
	json_object_set_value(object, "data", data);

	new payload[1024];
	json_serial_to_string(object, payload, charsmax(payload));
	json_free(object);

	new url[160];
	formatex(url, charsmax(url), "%s/record", g_szGatewayHttp);

	new EzHttpOptions:options_id = ezhttp_create_options();
	ezhttp_option_set_header(options_id, "Content-Type", "application/json");
	ezhttp_option_set_body(options_id, payload);
	ezhttp_option_set_timeout(options_id, 5000);

	ezhttp_post(url, "OnGatewayHttpComplete", options_id);
}

public OnGatewayHttpComplete(EzHttpRequest:request_id) {
	if (ezhttp_get_error_code(request_id) != EZH_OK) {
		new error[64];
		ezhttp_get_error_message(request_id, error, charsmax(error));
		log_amx("MR1V1: gateway report error: %s", error);
	}
}

ReportMatchStart() {
	new JSON:data = json_init_object();
	json_object_set_string(data, "match_id", g_szMatchId);
	json_object_set_string(data, "map", g_szMapName);

	new name[32];

	get_user_name(g_iPlayer[0], name, charsmax(name));
	json_object_set_string(data, "p0.name", name, true);
	json_object_set_string(data, "p0.authid", g_szAuthId[0], true);
	json_object_set_number(data, "p0.userid", get_user_userid(g_iPlayer[0]), true);

	get_user_name(g_iPlayer[1], name, charsmax(name));
	json_object_set_string(data, "p1.name", name, true);
	json_object_set_string(data, "p1.authid", g_szAuthId[1], true);
	json_object_set_number(data, "p1.userid", get_user_userid(g_iPlayer[1]), true);

	ReportEvent("mr1v1_match_start", data);
}

ReportRoundEnd(dmg0, hits0, dmg1, hits1) {
	new JSON:data = json_init_object();

	json_object_set_string(data, "match_id", g_szMatchId);
	json_object_set_number(data, "round", g_iLastRoundNum);
	// phase字段保留兼容旧上报格式(consumer的envelope.RoundEnd结构体仍有该字段)，
	// 捡枪模式不分阶段，固定上报0
	json_object_set_number(data, "phase", 0);
	json_object_set_number(data, "winner_slot", g_iLastWinnerSlot);
	json_object_set_number(data, "wins0", g_iWins[0]);
	json_object_set_number(data, "wins1", g_iWins[1]);
	json_object_set_number(data, "p0.damage", dmg0, true);
	json_object_set_number(data, "p0.hits", hits0, true);
	json_object_set_number(data, "p1.damage", dmg1, true);
	json_object_set_number(data, "p1.hits", hits1, true);

	ReportEvent("mr1v1_round_end", data);
}

// end_reason: normal(正常打满/提前胜出) / manual_stop(.stop手动停止) /
// disconnect(玩家掉线立即取消) / disconnect_timeout(玩家掉线超时未重连)
ReportMatchEnd(winner, const name1[], const name2[], const endReason[]) {
	new JSON:data = json_init_object();

	json_object_set_string(data, "match_id", g_szMatchId);
	json_object_set_string(data, "end_reason", endReason);
	json_object_set_number(data, "winner_slot", winner);
	json_object_set_number(data, "wins0", g_iWins[0]);
	json_object_set_number(data, "wins1", g_iWins[1]);
	json_object_set_string(data, "p0.name", name1, true);
	json_object_set_string(data, "p0.authid", g_szAuthId[0], true);
	json_object_set_string(data, "p1.name", name2, true);
	json_object_set_string(data, "p1.authid", g_szAuthId[1], true);

	ReportEvent("mr1v1_match_end", data);
}
