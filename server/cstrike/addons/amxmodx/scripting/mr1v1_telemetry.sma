// ============================================================
// MR1V1 数据采集插件
//
// 与 mr1v1_match.sma 解耦：通过其广播的自定义forward
// mr1v1_match_start(match_id, id0, id1) / mr1v1_match_end(...)
// 获知当前比赛的match_id和双方玩家实体id，期间收集战斗事件
// （目前为命中/伤害，开枪事件待后续补充），按固定周期批量
// HTTP POST 上报到 gateway（地址复用 configs/mr1v1.ini 的
// mr1v1_gateway_http），事件类型 mr1v1_combat_batch。
// ============================================================

#include <amxmodx>
#include <amxmisc>
#include <fakemeta>
#include <csx>
#include <reapi>
#include <json>
#include <easy_http>

#define PLUGIN_NAME    "MR1V1 Telemetry"
#define PLUGIN_VERSION "1.0"
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

public plugin_init() {
	register_plugin(PLUGIN_NAME, PLUGIN_VERSION, PLUGIN_AUTHOR);

	LoadGatewayConfig();

	RegisterHookChain(RG_CBasePlayer_TraceAttack, "CBasePlayer_TraceAttack_Post", true);
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
	g_bActive = true;
}

public mr1v1_match_end(winner, wins0, wins1, userid0, userid1) {
	if (g_bActive) {
		FlushBatch();
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
	FlushBatch();
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
