// ============================================================
// MR1V1 武器轮换比赛插件
//
// 规则：
//   - 最多51回合(10手枪/28步枪/13狙击)，先达26胜者获胜，提前分出胜负即结束（无加时）
//   - 队伍分边第1回合随机决定，之后每回合结束后无论胜负T/CT自动互换
//   - 记分板T/CT回合数实时映射为"当前在该边的玩家的个人胜场数"，与自定义HUD比分保持一致
//   - 第1-10回合：手枪局，半甲，默认USP，.1=USP .2=格洛克 .3=沙漠之鹰
//   - 第11-38回合：步枪局，全甲，默认AK47，.1=AK47 .2=M4A1 .3=法玛斯 .4=加利尔
//   - 第39-51回合：狙击局，全甲，默认AWP，.1=AWP .2=鸟狙（另发副武器手枪：CT=USP T=格洛克）
//   - .1/.2/.3/.4 选择按阶段记忆，进入新阶段重置为默认武器；.guns 弹出菜单按当前阶段选择
//   - 每回合开始强制清空背包并按阶段+偏好重发武器/满弹/护甲，无手雷
//   - 禁止购买菜单/丢弃/拾取/死亡掉枪；地图自带武器/弹药拾取物持续清除
//   - 比赛开始后 mp_freezetime 设为2秒，结束后恢复为0
//   - 比赛开始时一次性公示回合机制+各阶段武器选项；回合结束显示双方本回合伤害
//   - 启动方式：聊天 .start，或后台RCON执行 mr1v1_start
//   - 停止比赛：聊天 .stop，或后台RCON执行 mr1v1_stop
//   - 换图：聊天 .map 弹出内置1v1地图菜单，或 .map <地图名> 直接指定（比赛进行中无法换图），3秒后切换
//   - 比赛进行中加入的第三人自动设为观察者
//   - 对战玩家掉线后30秒内重连(SteamID匹配)可恢复比赛，超时则自动取消
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
#define PLUGIN_VERSION "1.0"
#define PLUGIN_AUTHOR  "1v1 Platform"

#define TOTAL_ROUNDS     51
#define WIN_THRESHOLD    26
#define PHASE_PISTOL_END 10
#define PHASE_RIFLE_END  38

enum MatchPhase {
	PHASE_PISTOL = 0,
	PHASE_RIFLE,
	PHASE_SNIPER
};

// 武器表索引
#define WPN_USP     0
#define WPN_GLOCK18 1
#define WPN_DEAGLE  2
#define WPN_AK47    3
#define WPN_M4A1    4
#define WPN_AWP     5
#define WPN_SCOUT   6
#define WPN_FAMAS   7
#define WPN_GALIL   8

enum _:WeaponEntry {
	WPN_CLASSNAME[32],
	WeaponIdType:WPN_ID,
	WPN_DISPLAY[16]
};

new const g_weaponTable[][WeaponEntry] = {
	{"weapon_usp",     WEAPON_USP,     "USP"},
	{"weapon_glock18", WEAPON_GLOCK18, "格洛克"},
	{"weapon_deagle",  WEAPON_DEAGLE,  "沙漠之鹰"},
	{"weapon_ak47",    WEAPON_AK47,    "AK-47"},
	{"weapon_m4a1",    WEAPON_M4A1,    "M4A1"},
	{"weapon_awp",     WEAPON_AWP,     "AWP"},
	{"weapon_scout",   WEAPON_SCOUT,   "鸟狙"},
	{"weapon_famas",   WEAPON_FAMAS,   "法玛斯"},
	{"weapon_galil",   WEAPON_GALIL,   "加利尔"}
};

new bool:g_bMatchActive;
new bool:g_bTestMode;
new g_iRoundNum;
new MatchPhase:g_ePhase;
new g_iPlayer[2];
new TeamName:g_eCurrentTeam[2];
new g_iWins[2];
new g_iWeaponChoice[MAX_PLAYERS + 1][3];
new g_szAuthId[2][35];
new bool:g_bPendingReconnect[2];
new g_iLastRoundNum;
new MatchPhase:g_eLastPhase;
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

new HookChain:g_hookAddItem;
new g_hudSync;
new g_hudWarmupSync;
new g_hFwdMatchEnd;

new g_szGatewayHttp[128];
new g_szGatewayToken[64];
new g_szMapName[32];

// 比赛唯一ID：每局比赛开始(InitMatch)时生成一次，贯穿该局所有上报事件，
// 供平台后端关联 mr1v1_match_start/mr1v1_round_end/mr1v1_match_end 三类事件
#define MATCH_ID_RAND_LEN  14
new const g_matchIdChars[] = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ";
new g_szMatchId[33];

GenerateMatchId() {
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
	register_clcmd("say .1", "CmdChoice1");
	register_clcmd("say_team .1", "CmdChoice1");
	register_clcmd("say .2", "CmdChoice2");
	register_clcmd("say_team .2", "CmdChoice2");
	register_clcmd("say .3", "CmdChoice3");
	register_clcmd("say_team .3", "CmdChoice3");
	register_clcmd("say .4", "CmdChoice4");
	register_clcmd("say_team .4", "CmdChoice4");
	register_clcmd("say .guns", "CmdGuns");
	register_clcmd("say_team .guns", "CmdGuns");
	register_clcmd("say .stop", "CmdStop");
	register_clcmd("say_team .stop", "CmdStop");
	register_clcmd("say", "CmdSay");
	register_clcmd("say_team", "CmdSay");

	register_concmd("mr1v1_start", "CmdConsoleStart", 0, "- 开始一局1v1武器轮换比赛（先进入热身）");
	register_concmd("mr1v1_start_bot_test", "CmdConsoleStartBotTest", 0, "- 补一个专家Bot并以加速模式开始比赛(测试用)");
	register_concmd("mr1v1_stop", "CmdConsoleStop", 0, "- 停止当前进行中的比赛");

	g_hookAddItem = RegisterHookChain(RG_CBasePlayer_AddPlayerItem, "CBasePlayer_AddPlayerItem_Pre", false);
	RegisterHookChain(RG_CBasePlayer_Spawn, "CBasePlayer_Spawn_Post", true);
	RegisterHookChain(RG_CBasePlayer_OnSpawnEquip, "CBasePlayer_OnSpawnEquip_Pre", false);
	RegisterHookChain(RG_CBasePlayer_HasRestrictItem, "CBasePlayer_HasRestrictItem_Pre", false);
	RegisterHookChain(RG_ShowVGUIMenu, "ShowVGUIMenu_Pre", false);
	RegisterHookChain(RG_BuyWeaponByWeaponID, "BuyWeaponByWeaponID_Pre", false);
	RegisterHookChain(RG_CBasePlayer_DropPlayerItem, "CBasePlayer_DropPlayerItem_Pre", false);
	RegisterHookChain(RG_CSGameRules_DeadPlayerWeapons, "CSGameRules_DeadPlayerWeapons_Pre", false);
	RegisterHookChain(RG_RoundEnd, "RoundEnd_Post", true);
	RegisterHookChain(RG_HandleMenu_ChooseTeam, "HandleMenu_ChooseTeam_Pre", false);
	set_task(1.0, "Task_EnforceSpectators", _, _, _, "b");

	g_hudSync = CreateHudSyncObj();
	g_hudWarmupSync = CreateHudSyncObj();
	g_hFwdMatchEnd = CreateMultiForward("mr1v1_match_end", ET_IGNORE, FP_CELL, FP_CELL, FP_CELL, FP_CELL, FP_CELL);

	g_pCvarWarmupTime = create_cvar("mr1v1_warmup_time", "15", .has_min = true, .min_val = 0.0);
	g_pCvarRoundInfinite = get_cvar_pointer("mp_round_infinite");
	g_pCvarForceRespawn = get_cvar_pointer("mp_forcerespawn");
	g_pCvarRespawnImmunity = get_cvar_pointer("mp_respawn_immunitytime");

	LoadGatewayConfig();
	get_mapname(g_szMapName, charsmax(g_szMapName));

	// 部分地图自带武器/弹药拾取物（如 ak47_m4a1_dust），持续清除（含拾取后重新生成的情况），
	// 避免与按阶段强制重发装备的规则冲突，同时不让玩家看到地图自带枪械模型
	set_task(2.0, "Task_RemoveMapWeapons", _, _, _, "b");
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
		} else if (equali(key, "mr1v1_gateway_token")) {
			copy(g_szGatewayToken, charsmax(g_szGatewayToken), value);
		}
	}

	fclose(fh);
	log_amx("MR1V1: gateway http=[%s] token=[%s]", g_szGatewayHttp, g_szGatewayToken);
}

// ------------------------------------------------------------
// 清除地图自带的武器/弹药拾取实体
// ------------------------------------------------------------

public Task_RemoveMapWeapons() {
	// 仅在正式回合中清理地图自带武器/弹药；非比赛/热身期间不清理，
	// 否则会把玩家手持/背包中的武器实体一并误删，导致出生没有武器
	if (!g_bMatchActive || g_bWarmupActive) {
		return;
	}

	new classname[32], removed = 0;

	for (new ent = 1; ent < entity_count(); ent++) {
		if (!is_valid_ent(ent))
			continue;

		// 跳过有主人的武器实体（玩家手持/背包中的武器），只清理地图上无主的拾取物
		if (pev(ent, pev_owner))
			continue;

		pev(ent, pev_classname, classname, charsmax(classname));

		if (equal(classname, "weapon_", 7) || equal(classname, "ammo_", 5)) {
			rg_remove_entity(ent);
			removed++;
		}
	}

	if (removed > 0)
		log_amx("MR1V1: removed %d map weapon/ammo entities", removed);
}

// ------------------------------------------------------------
// 启动命令
// ------------------------------------------------------------

public CmdStart(const id) {
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
	if (g_bMatchActive) {
		console_print(id, "[1v1] 比赛已经在进行中");
		return PLUGIN_HANDLED;
	}

	g_bTestMode = false;
	StartWarmup();
	return PLUGIN_HANDLED;
}

public CmdStartBot(const id) { StartWithBot(false); return PLUGIN_HANDLED; }
public CmdStartBotTest(const id) { StartWithBot(true); return PLUGIN_HANDLED; }

public CmdConsoleStartBotTest(const id, const level, const cid) {
	if (g_bMatchActive) {
		console_print(id, "[1v1] 比赛已经在进行中");
		return PLUGIN_HANDLED;
	}

	StartWithBot(true);
	return PLUGIN_HANDLED;
}

// 真人优先级最高：仅当前只有1名真人时才补一个专家难度(最高智商)Bot陪练；
// 真人数>=2时忽略Bot，直接走正常流程(.start一致)
// testMode=true 时会缩短回合时间/冻结时间，方便快速跑完比赛做测试
StartWithBot(bool:testMode) {
	if (g_bMatchActive) {
		client_print_color(0, print_team_grey, "^4[1v1] ^1比赛已经在进行中");
		return;
	}

	g_bTestMode = testMode;

	new humans[MAX_PLAYERS], numHumans;
	get_players(humans, numHumans, "ch"); // 排除Bot和HLTV，仅统计真人

	if (numHumans == 1) {
		set_cvar_num("bot_join_after_player", 0);
		set_cvar_string("bot_quota_mode", "normal");
		set_cvar_num("bot_difficulty", 3);
		set_cvar_num("bot_quota", 1);
		server_cmd("bot_add");

		client_print_color(0, print_team_grey, "^4[1v1] ^1已添加专家难度Bot，准备开始比赛...");
		set_task(1.5, "Task_StartMatch");
		return;
	}

	if (testMode) {
		InitMatch();
	} else {
		StartWarmup();
	}
}

public Task_StartMatch() {
	if (g_bTestMode) {
		InitMatch();
	} else {
		StartWarmup();
	}
}

public CmdChoice1(const id) { SetWeaponChoice(id, 1); return PLUGIN_HANDLED; }
public CmdChoice2(const id) { SetWeaponChoice(id, 2); return PLUGIN_HANDLED; }
public CmdChoice3(const id) { SetWeaponChoice(id, 3); return PLUGIN_HANDLED; }
public CmdChoice4(const id) { SetWeaponChoice(id, 4); return PLUGIN_HANDLED; }

public CmdStop(const id) {
	new name[32];
	get_user_name(id, name, charsmax(name));
	log_amx("MR1V1_CMD_STOP by=%s(%d) match_active=%d", name, id, g_bMatchActive);

	if (!g_bMatchActive) {
		client_print_color(id, print_team_grey, "^4[1v1] ^1当前没有进行中的比赛");
		return PLUGIN_HANDLED;
	}

	AbortMatch("比赛已被停止");
	return PLUGIN_HANDLED;
}

public CmdConsoleStop(const id, const level, const cid) {
	if (!g_bMatchActive) {
		console_print(id, "[1v1] 当前没有进行中的比赛");
		return PLUGIN_HANDLED;
	}

	AbortMatch("比赛已被停止");
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
		&& !equal(text, ".start_bot_test") && !equal(text, ".stop")
		&& !equal(text, ".1") && !equal(text, ".2") && !equal(text, ".3") && !equal(text, ".4")
		&& !equal(text, ".guns")) {
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

// 给热身玩家发放练习装备：AK47+USP+全甲，无手雷，购买/拾取/丢弃不受限
EquipPlayerForWarmup(const id) {
	rg_remove_all_items(id);
	rg_set_user_armor(id, 100, ARMOR_VESTHELM);

	rg_give_item(id, "weapon_knife", GT_APPEND);

	GiveWeaponFullAmmo(id, WPN_AK47);
	GiveWeaponFullAmmo(id, WPN_USP);
}

// ------------------------------------------------------------
// 比赛流程
// ------------------------------------------------------------

// 选定本局对战的两名玩家：仅从已加入T/CT的玩家中随机抽取两人，
// 仍在选队菜单(TEAM_UNASSIGNED)或已是观察者的玩家不参与抽取，多余玩家强制设为观察者
// 设置 g_iPlayer/g_szAuthId/g_eCurrentTeam，并重置武器选择记忆
bool:SelectMatchPlayers() {
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

	new selName0[32], selName1[32];
	get_user_name(g_iPlayer[0], selName0, charsmax(selName0));
	get_user_name(g_iPlayer[1], selName1, charsmax(selName1));
	log_amx("MR1V1_SELECT_RESULT p0=%d(%s,%s) p1=%d(%s,%s)",
		g_iPlayer[0], selName0, g_szAuthId[0], g_iPlayer[1], selName1, g_szAuthId[1]);

	arrayset(g_iWeaponChoice[g_iPlayer[0]], 0, 3);
	arrayset(g_iWeaponChoice[g_iPlayer[1]], 0, 3);

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

// 比赛开始时一次性公示规则：回合机制 + 各阶段武器选择
AnnounceMatchRules() {
	client_print_color(0, print_team_grey, "^4[1v1] ^1本场比赛决胜方式：10手枪、28步枪、13狙击，共51回合26胜");
	client_print_color(0, print_team_grey, "^4[1v1] ^1聊天框输入.guns唤起武器选择菜单，或直接输入下面.1 .2 .3可快速切枪");
	client_print_color(0, print_team_grey, "^4[1v1] ^1手枪局:.1=USP(默认) .2=格洛克 .3=沙漠之鹰");
}

InitMatch(bool:selectPlayers = true) {
	if (selectPlayers && !SelectMatchPlayers()) {
		return;
	}

	g_iWins[0] = 0;
	g_iWins[1] = 0;
	g_iRoundNum = 1;
	g_ePhase = PHASE_PISTOL;
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
		EquipPlayerForWarmup(id);
	} else {
		// 换边后在重生时刷新模型与记分板队伍颜色，此时旧尸体已不存在，无违和感
		new before = get_member(id, m_iTeam);
		rg_set_user_team(id, g_eCurrentTeam[GetSlot(id)], MODEL_AUTO, true, false);
		log_amx("MR1V1_SPAWN round=%d slot=%d id=%d team_before=%d team_after=%d",
			g_iRoundNum, GetSlot(id), id, before, _:g_eCurrentTeam[GetSlot(id)]);
		EquipPlayerForCurrentRound(id);
	}
}

public CBasePlayer_OnSpawnEquip_Pre(const id) {
	if (IsMatchPlayer(id)) {
		return HC_SUPERCEDE;
	}
	return HC_CONTINUE;
}

GetWeaponIndexForChoice(MatchPhase:phase, const choice) {
	switch (phase) {
		case PHASE_PISTOL: {
			switch (choice) {
				case 2: return WPN_GLOCK18;
				case 3: return WPN_DEAGLE;
				default: return WPN_USP;
			}
		}
		case PHASE_RIFLE: {
			switch (choice) {
				case 2: return WPN_M4A1;
				case 3: return WPN_FAMAS;
				case 4: return WPN_GALIL;
				default: return WPN_AK47;
			}
		}
		case PHASE_SNIPER: {
			switch (choice) {
				case 2: return WPN_SCOUT;
				default: return WPN_AWP;
			}
		}
	}

	return WPN_USP;
}

GetWeaponIndexForPhase(const id) {
	return GetWeaponIndexForChoice(g_ePhase, g_iWeaponChoice[id][_:g_ePhase]);
}

GetMaxChoiceForPhase(MatchPhase:phase) {
	switch (phase) {
		case PHASE_PISTOL: return 3;
		case PHASE_RIFLE:  return 4;
		default:           return 2;
	}
	return 2;
}

// 副武器(手枪)：从手枪局开始可通过 .1/.2/.3 切换(g_iWeaponChoice记录在PHASE_PISTOL)，
// 该选择不会随阶段切换被重置，因此步枪局/狙击局的副武器沿用同一份选择
GetSecondaryPistolIndex(const id) {
	switch (g_iWeaponChoice[id][_:PHASE_PISTOL]) {
		case 2: return WPN_GLOCK18;
		case 3: return WPN_DEAGLE;
	}

	return WPN_USP;
}

GiveWeaponFullAmmo(const id, const wpnIdx) {
	new ent = rg_give_item(id, g_weaponTable[wpnIdx][WPN_CLASSNAME], GT_APPEND);
	if (ent > 0) {
		new maxAmmo = rg_get_iteminfo(ent, ItemInfo_iMaxAmmo1);
		rg_set_user_bpammo(id, g_weaponTable[wpnIdx][WPN_ID], maxAmmo);
	}
}

EquipPlayerForCurrentRound(const id) {
	rg_remove_all_items(id);

	new ArmorType:armor = (g_ePhase == PHASE_PISTOL) ? ARMOR_KEVLAR : ARMOR_VESTHELM;
	rg_set_user_armor(id, 100, armor);

	new wpnIdx = GetWeaponIndexForPhase(id);

	DisableHookChain(g_hookAddItem);

	rg_give_item(id, "weapon_knife", GT_APPEND);
	GiveWeaponFullAmmo(id, wpnIdx);

	// 步枪局/狙击局额外携带副武器(手枪)，沿用手枪局的 .1/.2/.3 选择
	if (g_ePhase != PHASE_PISTOL) {
		GiveWeaponFullAmmo(id, GetSecondaryPistolIndex(id));
	}

	EnableHookChain(g_hookAddItem);

	// 强制切换为本阶段主武器，避免Bot/玩家出生后停留在小刀或副武器上
	client_cmd(id, g_weaponTable[wpnIdx][WPN_CLASSNAME]);

	new name[32];
	get_user_name(id, name, charsmax(name));

	log_amx("MR1V1 round=%d phase=%d player=%s(%d) team=%d weapon=%s armor=%d",
		g_iRoundNum, _:g_ePhase, name, id, _:g_eCurrentTeam[GetSlot(id)],
		g_weaponTable[wpnIdx][WPN_CLASSNAME], _:armor);
}

// ------------------------------------------------------------
// 武器选择
// ------------------------------------------------------------

SetWeaponChoice(const id, const choice) {
	if (!IsMatchPlayer(id) || g_bWarmupActive) {
		return;
	}

	new maxChoice = GetMaxChoiceForPhase(g_ePhase);
	if (choice > maxChoice) {
		client_print_color(id, print_team_grey, "^4[1v1] ^1当前阶段没有.%d这个选项", choice);
		return;
	}

	new name[32];
	get_user_name(id, name, charsmax(name));
	log_amx("MR1V1_CMD_CHOICE by=%s(%d) phase=%d choice=%d alive=%d", name, id, _:g_ePhase, choice, is_user_alive(id));

	g_iWeaponChoice[id][_:g_ePhase] = choice;

	new wpnIdx = GetWeaponIndexForPhase(id);
	client_print_color(id, print_team_grey, "^4[1v1] ^1已选择武器:%s（下回合/复活后生效）", g_weaponTable[wpnIdx][WPN_DISPLAY]);

	if (is_user_alive(id)) {
		EquipPlayerForCurrentRound(id);
	}
}

// .guns：弹出菜单按当前阶段选择武器，等价于直接执行对应的 .1/.2/.3/.4
public CmdGuns(const id) {
	if (!IsMatchPlayer(id) || g_bWarmupActive) {
		return PLUGIN_HANDLED;
	}

	ShowGunsMenu(id);
	return PLUGIN_HANDLED;
}

ShowGunsMenu(const id) {
	new menu = menu_create("\y[1v1] 选择武器", "GunsMenuHandler");

	new maxChoice = GetMaxChoiceForPhase(g_ePhase);
	for (new choice = 1; choice <= maxChoice; choice++) {
		new wpnIdx = GetWeaponIndexForChoice(g_ePhase, choice);
		menu_additem(menu, g_weaponTable[wpnIdx][WPN_DISPLAY], "", 0);
	}

	menu_display(id, menu, 0);
}

public GunsMenuHandler(const id, menu, item) {
	if (item == MENU_EXIT) {
		menu_destroy(menu);
		return PLUGIN_HANDLED;
	}

	menu_destroy(menu);
	SetWeaponChoice(id, item + 1);
	return PLUGIN_HANDLED;
}

// ------------------------------------------------------------
// 禁止购买/丢弃/拾取
// ------------------------------------------------------------

public CBasePlayer_HasRestrictItem_Pre(const id) {
	if (IsMatchPlayer(id) && !g_bWarmupActive) {
		SetHookChainReturn(ATYPE_BOOL, true);
		return HC_SUPERCEDE;
	}
	return HC_CONTINUE;
}

public ShowVGUIMenu_Pre(const id, VGUIMenu:menuType) {
	if (IsMatchPlayer(id) && !g_bWarmupActive) {
		switch (menuType) {
			case VGUI_Menu_Buy, VGUI_Menu_Buy_Pistol, VGUI_Menu_Buy_ShotGun, VGUI_Menu_Buy_Rifle,
				 VGUI_Menu_Buy_SubMachineGun, VGUI_Menu_Buy_MachineGun, VGUI_Menu_Buy_Item: {
				return HC_SUPERCEDE;
			}
		}
	}
	return HC_CONTINUE;
}

public BuyWeaponByWeaponID_Pre(const id) {
	if (IsMatchPlayer(id) && !g_bWarmupActive) {
		SetHookChainReturn(ATYPE_INTEGER, 0);
		return HC_SUPERCEDE;
	}
	return HC_CONTINUE;
}

public CBasePlayer_DropPlayerItem_Pre(const id) {
	if (IsMatchPlayer(id) && !g_bWarmupActive) {
		SetHookChainReturn(ATYPE_INTEGER, 0);
		return HC_SUPERCEDE;
	}
	return HC_CONTINUE;
}

public CBasePlayer_AddPlayerItem_Pre(const id) {
	if (IsMatchPlayer(id) && !g_bWarmupActive) {
		SetHookChainReturn(ATYPE_INTEGER, 0);
		return HC_SUPERCEDE;
	}
	return HC_CONTINUE;
}

public CSGameRules_DeadPlayerWeapons_Pre(const id) {
	if (IsMatchPlayer(id) && !g_bWarmupActive) {
		SetHookChainReturn(ATYPE_INTEGER, GR_PLR_DROP_GUN_NO);
		return HC_SUPERCEDE;
	}
	return HC_CONTINUE;
}

// ------------------------------------------------------------
// 回合结束：计分 / 换边 / 阶段切换
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
	g_eLastPhase = g_ePhase;
	g_iLastWinnerSlot = winnerSlot;
	set_task(0.3, "Task_RoundDamage");

	log_amx("MR1V1 round_end round=%d phase=%d status=%d winner_slot=%d wins=%d:%d",
		g_iRoundNum, _:g_ePhase, _:status, winnerSlot, g_iWins[0], g_iWins[1]);

	if (g_iRoundNum >= TOTAL_ROUNDS || g_iWins[0] >= WIN_THRESHOLD || g_iWins[1] >= WIN_THRESHOLD) {
		UpdateNativeTeamScores();
		AnnounceMatchResult();
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
	AdvancePhaseIfNeeded();
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

AdvancePhaseIfNeeded() {
	new MatchPhase:newPhase;

	if (g_iRoundNum <= PHASE_PISTOL_END) {
		newPhase = PHASE_PISTOL;
	} else if (g_iRoundNum <= PHASE_RIFLE_END) {
		newPhase = PHASE_RIFLE;
	} else {
		newPhase = PHASE_SNIPER;
	}

	if (newPhase != g_ePhase) {
		g_ePhase = newPhase;

		g_iWeaponChoice[g_iPlayer[0]][_:g_ePhase] = 0;
		g_iWeaponChoice[g_iPlayer[1]][_:g_ePhase] = 0;

		new const phaseNames[][] = {"手枪局", "步枪局", "狙击局"};
		new const phaseWeaponInfo[][] = {
			".1=USP(默认) .2=格洛克 .3=沙漠之鹰",
			".1=AK47(默认) .2=M4A1 .3=法玛斯 .4=加利尔",
			".1=AWP(默认) .2=鸟狙"
		};
		client_print_color(0, print_team_grey, "^4[1v1] ^1%s:%s", phaseNames[_:g_ePhase], phaseWeaponInfo[_:g_ePhase]);
		log_amx("MR1V1_PHASE_CHANGE round=%d new_phase=%d", g_iRoundNum, _:g_ePhase);
	}
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

AnnounceMatchResult() {
	new name1[32], name2[32];
	get_user_name(g_iPlayer[0], name1, charsmax(name1));
	get_user_name(g_iPlayer[1], name2, charsmax(name2));

	new winner = -1;
	if (g_iWins[0] > g_iWins[1]) {
		winner = 0;
	} else if (g_iWins[1] > g_iWins[0]) {
		winner = 1;
	}

	if (winner == -1) {
		client_print_color(0, print_team_grey, "^4[1v1] ^1比赛结束！平局%d:%d", g_iWins[0], g_iWins[1]);
	} else {
		new winnerName[32];
		get_user_name(g_iPlayer[winner], winnerName, charsmax(winnerName));
		client_print_color(0, print_team_grey, "^4[1v1] ^1比赛结束！%s获胜！比分%d:%d", winnerName, g_iWins[0], g_iWins[1]);

		new loser = 1 - winner;
		client_print_color(g_iPlayer[loser], print_team_grey, "^4[1v1] ^1很遗憾你失败了，别气馁，再来一把干回来！");
	}

	log_amx("MR1V1_MATCH_END match_id=%s winner_slot=%d score=%d:%d p0=%d(%s) p1=%d(%s)",
		g_szMatchId, winner, g_iWins[0], g_iWins[1], g_iPlayer[0], name1, g_iPlayer[1], name2);

	new ret;
	ExecuteForward(g_hFwdMatchEnd, ret, winner, g_iWins[0], g_iWins[1],
		get_user_userid(g_iPlayer[0]), get_user_userid(g_iPlayer[1]));

	ReportMatchEnd(winner, name1, name2);

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
}

// 中止比赛并恢复服务器状态（.stop / 掉线超时 / Bot掉线 等场景共用）：
// 移除比赛Bot、清空比分/回合/阶段等状态，并刷新地图让服务器回到干净的初始状态
AbortMatch(const reason[]) {
	client_print_color(0, print_team_grey, "^4[1v1] ^1%s，服务器即将刷新", reason);
	log_amx("MR1V1_MATCH_ABORT reason=%s", reason);

	RestoreServerCvars();
	g_bMatchActive = false;
	g_bPendingReconnect[0] = false;
	g_bPendingReconnect[1] = false;
	RefreshVoiceState();

	ResetMatchState();
}

// 清空比分/回合/阶段/武器选择等比赛状态，移除比赛Bot，并刷新当前地图（不换图）
// 比赛正常结束(AnnounceMatchResult)和被中止(.stop/AbortMatch)后共用
ResetMatchState() {
	arrayset(g_iWeaponChoice[g_iPlayer[0]], 0, 3);
	arrayset(g_iWeaponChoice[g_iPlayer[1]], 0, 3);
	g_iPlayer[0] = 0;
	g_iPlayer[1] = 0;
	g_iWins[0] = 0;
	g_iWins[1] = 0;
	g_iRoundNum = 0;
	g_ePhase = PHASE_PISTOL;
	g_bPendingReconnect[0] = false;
	g_bPendingReconnect[1] = false;

	// 先清零bot_quota，否则.start_bot流程设的quota=1会让引擎在KickAllBots后立刻补一个新Bot
	set_cvar_num("bot_quota", 0);
	KickAllBots();

	// 不换图，仅用引擎自带的 mp_restartgame 重启当前回合，
	// 清空残留实体/掉落武器/比分显示，恢复到干净的初始状态
	set_cvar_num("mp_restartgame", 1);
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
		AbortMatch("玩家掉线，比赛已取消");
		return;
	}

	g_bPendingReconnect[slot] = true;
	client_print_color(0, print_team_grey, "^4[1v1] ^1玩家掉线，等待30秒重连，比赛暂不取消");
	log_amx("MR1V1_MATCH_PLAYER_DROP slot=%d authid=%s", slot, g_szAuthId[slot]);
	set_task(30.0, "Task_AbortIfNoReconnect", slot + 1000);
}

public Task_AbortIfNoReconnect(slotPlus) {
	new slot = slotPlus - 1000;
	if (g_bMatchActive && g_bPendingReconnect[slot]) {
		AbortMatch("玩家掉线超时未重连，比赛已取消");
	}
}

// 根据SteamID识别掉线玩家重连，恢复其在比赛中的位置
public client_authorized(id, const authid[]) {
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

// 比赛进行中，重连的对战玩家恢复队伍；非对战的第三人强制设为观察者
public client_putinserver(id) {
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
// gateway 上报（HTTP POST /record，JSON信封：timestamp/token/type/version/data）
// ------------------------------------------------------------

ReportEvent(const type[], const data[]) {
	if (!strlen(g_szGatewayHttp)) {
		return;
	}

	new JSON:object = json_init_object();
	json_object_set_number(object, "timestamp", get_systime());
	json_object_set_string(object, "token", g_szGatewayToken);
	json_object_set_string(object, "type", type);
	json_object_set_string(object, "version", PLUGIN_VERSION);
	json_object_set_string(object, "data", data);

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

	new buf[512];
	json_serial_to_string(data, buf, charsmax(buf));
	json_free(data);

	ReportEvent("mr1v1_match_start", buf);
}

ReportRoundEnd(dmg0, hits0, dmg1, hits1) {
	new JSON:data = json_init_object();

	json_object_set_string(data, "match_id", g_szMatchId);
	json_object_set_number(data, "round", g_iLastRoundNum);
	json_object_set_number(data, "phase", _:g_eLastPhase);
	json_object_set_number(data, "winner_slot", g_iLastWinnerSlot);
	json_object_set_number(data, "wins0", g_iWins[0]);
	json_object_set_number(data, "wins1", g_iWins[1]);
	json_object_set_number(data, "p0.damage", dmg0, true);
	json_object_set_number(data, "p0.hits", hits0, true);
	json_object_set_number(data, "p1.damage", dmg1, true);
	json_object_set_number(data, "p1.hits", hits1, true);

	new buf[512];
	json_serial_to_string(data, buf, charsmax(buf));
	json_free(data);

	ReportEvent("mr1v1_round_end", buf);
}

ReportMatchEnd(winner, const name1[], const name2[]) {
	new JSON:data = json_init_object();

	json_object_set_string(data, "match_id", g_szMatchId);
	json_object_set_number(data, "winner_slot", winner);
	json_object_set_number(data, "wins0", g_iWins[0]);
	json_object_set_number(data, "wins1", g_iWins[1]);
	json_object_set_string(data, "p0.name", name1, true);
	json_object_set_string(data, "p0.authid", g_szAuthId[0], true);
	json_object_set_string(data, "p1.name", name2, true);
	json_object_set_string(data, "p1.authid", g_szAuthId[1], true);

	new buf[512];
	json_serial_to_string(data, buf, charsmax(buf));
	json_free(data);

	ReportEvent("mr1v1_match_end", buf);
}
