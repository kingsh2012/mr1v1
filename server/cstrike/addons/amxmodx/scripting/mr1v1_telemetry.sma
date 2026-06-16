// ============================================================
// MR1V1 数据采集插件
//
// 与 mr1v1_match.sma 解耦：通过其广播的自定义forward
// mr1v1_match_start(match_id, id0, id1) / mr1v1_match_end(...)
// 获知当前比赛的match_id和双方玩家实体id，期间收集命中/伤害、
// 开枪、移动轨迹等事件，按固定周期批量HTTP POST上报到gateway
// （地址复用 configs/mr1v1.ini 的 mr1v1_gateway_http），事件类型
// 分别为 mr1v1_combat_batch / mr1v1_shoot_batch / mr1v1_position_batch。
// ============================================================

#include <amxmodx>
#include <amxmisc>
#include <fakemeta>
#include <csx>
#include <reapi>
#include <json>
#include <easy_http>

#define PLUGIN_NAME    "MR1V1 Telemetry"
#define PLUGIN_VERSION "1.1"
#define PLUGIN_AUTHOR  "1v1 Platform"

#define MAX_BATCH_EVENTS 32
#define FLUSH_INTERVAL   1.0

new g_szGatewayHttp[128];
new g_szMatchId[33];
new g_iTelePlayer[2];
new bool:g_bActive;

new g_iEvtTs[MAX_BATCH_EVENTS];
new g_iEvtAtkSlot[MAX_BATCH_EVENTS];
new g_iEvtVictimSlot[MAX_BATCH_EVENTS];
new g_szEvtWeapon[MAX_BATCH_EVENTS][32];
new g_iEvtDamage[MAX_BATCH_EVENTS];
new g_iEvtHitgroup[MAX_BATCH_EVENTS];
new g_iEvtCount;

// 开枪事件：通过比较武器实体相邻两次ItemPostFrame的m_Weapon_iClip判定
new g_iLastClip[MAX_EDICTS];
new g_iShootTs[MAX_BATCH_EVENTS];
new g_iShootSlot[MAX_BATCH_EVENTS];
new g_szShootWeapon[MAX_BATCH_EVENTS][32];
new g_iShootAmmo[MAX_BATCH_EVENTS];
new g_iShootCount;

// 移动轨迹：每FLUSH_INTERVAL秒对双方各采样一次坐标/朝向
new g_iPosTs[MAX_BATCH_EVENTS];
new g_iPosSlot[MAX_BATCH_EVENTS];
new Float:g_flPosX[MAX_BATCH_EVENTS];
new Float:g_flPosY[MAX_BATCH_EVENTS];
new Float:g_flPosZ[MAX_BATCH_EVENTS];
new Float:g_flPosYaw[MAX_BATCH_EVENTS];
new Float:g_flPosPitch[MAX_BATCH_EVENTS];
new g_iPosCount;

public plugin_init() {
	register_plugin(PLUGIN_NAME, PLUGIN_VERSION, PLUGIN_AUTHOR);

	LoadGatewayConfig();

	RegisterHookChain(RG_CBasePlayer_TraceAttack, "CBasePlayer_TraceAttack_Post", true);
	RegisterHookChain(RG_CBasePlayerWeapon_ItemPostFrame, "CBasePlayerWeapon_ItemPostFrame_Post", true);
	set_task(FLUSH_INTERVAL, "Task_FlushBatch", _, _, _, "b");
}

// ------------------------------------------------------------
// gateway 配置加载（与mr1v1_match.sma共用同一份mr1v1.ini）
// ------------------------------------------------------------

LoadGatewayConfig() {
	new fh, path[128], text[128], key[64], value[128];
	get_configsdir(path, charsmax(path));
	formatex(path, charsmax(path), "%s/mr1v1.ini", path);

	if (!file_exists(path)) {
		log_amx("MR1V1_TELEMETRY: gateway config %s not found, HTTP report disabled", path);
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

	log_amx("MR1V1_TELEMETRY: gateway http=[%s]", g_szGatewayHttp);
}

// ------------------------------------------------------------
// 来自 mr1v1_match.sma 的比赛上下文forward
// ------------------------------------------------------------

public mr1v1_match_start(const match_id[], id0, id1) {
	copy(g_szMatchId, charsmax(g_szMatchId), match_id);
	g_iTelePlayer[0] = id0;
	g_iTelePlayer[1] = id1;
	g_iEvtCount = 0;
	g_iShootCount = 0;
	g_iPosCount = 0;
	// 清空上一场比赛残留的武器实体弹夹记录，避免换弹/换枪被误判为开枪
	arrayset(g_iLastClip, -1, sizeof(g_iLastClip));
	g_bActive = true;
}

public mr1v1_match_end(winner, wins0, wins1, userid0, userid1) {
	if (g_bActive) {
		FlushBatch();
		FlushShootBatch();
		FlushPositionBatch();
	}
	g_bActive = false;
}

// ------------------------------------------------------------
// 战斗事件采集
// ------------------------------------------------------------

SlotOf(const id) {
	if (id == g_iTelePlayer[0]) {
		return 0;
	}
	if (id == g_iTelePlayer[1]) {
		return 1;
	}
	return -1;
}

public CBasePlayer_TraceAttack_Post(const victim, pevAttacker, Float:flDamage, Float:vecDir[3], tracehandle, bitsDamageType) {
	if (!g_bActive || victim == pevAttacker) {
		return HC_CONTINUE;
	}

	new victimSlot = SlotOf(victim);
	new attackerSlot = SlotOf(pevAttacker);
	if (victimSlot == -1 || attackerSlot == -1) {
		return HC_CONTINUE;
	}

	PushEvent(attackerSlot, victimSlot, pevAttacker, get_tr2(tracehandle, TR_iHitgroup), floatround(flDamage));
	return HC_CONTINUE;
}

PushEvent(attackerSlot, victimSlot, attacker, hitgroup, damage) {
	if (g_iEvtCount >= MAX_BATCH_EVENTS) {
		FlushBatch();
	}

	new idx = g_iEvtCount++;
	g_iEvtTs[idx] = get_systime();
	g_iEvtAtkSlot[idx] = attackerSlot;
	g_iEvtVictimSlot[idx] = victimSlot;
	g_iEvtDamage[idx] = damage;
	g_iEvtHitgroup[idx] = hitgroup;

	new wpnid = get_user_weapon(attacker);
	xmod_get_wpnname(wpnid, g_szEvtWeapon[idx], charsmax(g_szEvtWeapon[]));
}

public Task_FlushBatch() {
	SamplePositions();
	FlushBatch();
	FlushShootBatch();
	FlushPositionBatch();
}

FlushBatch() {
	if (!g_iEvtCount) {
		return;
	}

	new count = g_iEvtCount;
	g_iEvtCount = 0;

	if (!strlen(g_szGatewayHttp)) {
		return;
	}

	new JSON:events = json_init_array();
	for (new i = 0; i < count; i++) {
		new JSON:ev = json_init_object();
		json_object_set_number(ev, "ts", g_iEvtTs[i]);
		json_object_set_number(ev, "attacker_slot", g_iEvtAtkSlot[i]);
		json_object_set_number(ev, "victim_slot", g_iEvtVictimSlot[i]);
		json_object_set_string(ev, "weapon", g_szEvtWeapon[i]);
		json_object_set_number(ev, "damage", g_iEvtDamage[i]);
		json_object_set_number(ev, "hitgroup", g_iEvtHitgroup[i]);
		json_array_append_value(events, ev);
	}

	new JSON:data = json_init_object();
	json_object_set_string(data, "match_id", g_szMatchId);
	json_object_set_value(data, "events", events);

	ReportEvent("mr1v1_combat_batch", data);
}

// ------------------------------------------------------------
// 开枪事件采集：通过比较武器实体相邻两次ItemPostFrame的m_Weapon_iClip判定是否打了一枪，
// 覆盖所有有弹夹的武器（手枪/步枪/狙击枪），换弹（clip增加）和切枪（首次观察）不会误判
// ------------------------------------------------------------

public CBasePlayerWeapon_ItemPostFrame_Post(const weapon) {
	if (!g_bActive) {
		return HC_CONTINUE;
	}

	new owner = pev(weapon, pev_owner);
	new slot = SlotOf(owner);
	if (slot == -1) {
		return HC_CONTINUE;
	}

	new clip = get_member(weapon, m_Weapon_iClip);
	new lastClip = g_iLastClip[weapon];
	g_iLastClip[weapon] = clip;

	if (lastClip != -1 && clip < lastClip) {
		new wpnid = get_member(weapon, m_iId);
		new wpnName[32];
		xmod_get_wpnname(wpnid, wpnName, charsmax(wpnName));
		PushShootEvent(slot, wpnName, clip);
	}

	return HC_CONTINUE;
}

PushShootEvent(slot, const weapon[], ammo) {
	if (g_iShootCount >= MAX_BATCH_EVENTS) {
		FlushShootBatch();
	}

	new idx = g_iShootCount++;
	g_iShootTs[idx] = get_systime();
	g_iShootSlot[idx] = slot;
	copy(g_szShootWeapon[idx], charsmax(g_szShootWeapon[]), weapon);
	g_iShootAmmo[idx] = ammo;
}

FlushShootBatch() {
	if (!g_iShootCount) {
		return;
	}

	new count = g_iShootCount;
	g_iShootCount = 0;

	if (!strlen(g_szGatewayHttp)) {
		return;
	}

	new JSON:events = json_init_array();
	for (new i = 0; i < count; i++) {
		new JSON:ev = json_init_object();
		json_object_set_number(ev, "ts", g_iShootTs[i]);
		json_object_set_number(ev, "slot", g_iShootSlot[i]);
		json_object_set_string(ev, "weapon", g_szShootWeapon[i]);
		json_object_set_number(ev, "ammo_remaining", g_iShootAmmo[i]);
		json_array_append_value(events, ev);
	}

	new JSON:data = json_init_object();
	json_object_set_string(data, "match_id", g_szMatchId);
	json_object_set_value(data, "events", events);

	ReportEvent("mr1v1_shoot_batch", data);
}

// ------------------------------------------------------------
// 移动轨迹采集：每FLUSH_INTERVAL秒对双方各采样一次坐标/朝向
// ------------------------------------------------------------

SamplePositions() {
	if (!g_bActive) {
		return;
	}

	for (new slot = 0; slot < 2; slot++) {
		new id = g_iTelePlayer[slot];
		if (!is_user_alive(id)) {
			continue;
		}

		new Float:origin[3], Float:vAngle[3];
		pev(id, pev_origin, origin);
		pev(id, pev_v_angle, vAngle);

		PushPositionEvent(slot, origin, vAngle);
	}
}

PushPositionEvent(slot, const Float:origin[3], const Float:vAngle[3]) {
	if (g_iPosCount >= MAX_BATCH_EVENTS) {
		FlushPositionBatch();
	}

	new idx = g_iPosCount++;
	g_iPosTs[idx] = get_systime();
	g_iPosSlot[idx] = slot;
	g_flPosX[idx] = origin[0];
	g_flPosY[idx] = origin[1];
	g_flPosZ[idx] = origin[2];
	g_flPosPitch[idx] = vAngle[0];
	g_flPosYaw[idx] = vAngle[1];
}

FlushPositionBatch() {
	if (!g_iPosCount) {
		return;
	}

	new count = g_iPosCount;
	g_iPosCount = 0;

	if (!strlen(g_szGatewayHttp)) {
		return;
	}

	new JSON:events = json_init_array();
	for (new i = 0; i < count; i++) {
		new JSON:ev = json_init_object();
		json_object_set_number(ev, "ts", g_iPosTs[i]);
		json_object_set_number(ev, "slot", g_iPosSlot[i]);
		json_object_set_real(ev, "x", g_flPosX[i]);
		json_object_set_real(ev, "y", g_flPosY[i]);
		json_object_set_real(ev, "z", g_flPosZ[i]);
		json_object_set_real(ev, "yaw", g_flPosYaw[i]);
		json_object_set_real(ev, "pitch", g_flPosPitch[i]);
		json_array_append_value(events, ev);
	}

	new JSON:data = json_init_object();
	json_object_set_string(data, "match_id", g_szMatchId);
	json_object_set_value(data, "events", events);

	ReportEvent("mr1v1_position_batch", data);
}

// ------------------------------------------------------------
// gateway 上报（与mr1v1_match.sma相同的信封：timestamp/match_id/type/version/data）
// ------------------------------------------------------------

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

	new payload[2048];
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
		log_amx("MR1V1_TELEMETRY: gateway report error: %s", error);
	}
}
