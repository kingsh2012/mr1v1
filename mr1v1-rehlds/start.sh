#!/bin/bash
# CS 1.6 1v1 server startup script
# Usage: ./start.sh [extra hlds args]
# 可通过环境变量覆盖（参考 .env.example）：SERVER_HOSTNAME, MAXPLAYERS, RCON_PASSWORD, PORT, MAP
# MAP 由agent按局按地图池随机选定后注入(捡枪式赛制按手枪/步枪/狙击分池)，留空则用aim_map兜底

# +ip 0.0.0.0：显式声明绑定地址，跳过引擎对容器hostname的gethostbyname()解析
# （host网络模式下容器hostname无/etc/hosts记录，会报"Invalid hostname"警告），
# 同时保持绑定0.0.0.0（含127.0.0.1，本机RCON可用）
# -nobreakpad：关闭崩溃上报模块，减少容器内无Steam客户端导致的
# steamclient.so/IPC超时等无害日志噪音（不影响SteamAPI_Init失败的提示，那条无法消除）

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
export LD_LIBRARY_PATH="$SCRIPT_DIR:$LD_LIBRARY_PATH"

# hostname 含空格，引擎的 +hostname 命令行参数无法正确传递（按空格截断），
# 故改为 sed 写入 server.cfg；rcon_password 为统一管理一并放在这里
# SERVER_NAME 为agent按局注入的服务器显示名，与手动维护的SERVER_HOSTNAME二选一(优先SERVER_NAME)
HOSTNAME_OVERRIDE="${SERVER_NAME:-$SERVER_HOSTNAME}"
if [ -n "$HOSTNAME_OVERRIDE" ]; then
  sed -i "s/^hostname .*/hostname \"$HOSTNAME_OVERRIDE\"/" "$SCRIPT_DIR/cstrike/server.cfg"
fi
if [ -n "$RCON_PASSWORD" ]; then
  sed -i "s/^rcon_password .*/rcon_password \"$RCON_PASSWORD\"/" "$SCRIPT_DIR/cstrike/server.cfg"
fi

# 比赛模式：agent按局创建容器时注入 MATCH_ID/P0_STEAMID/P1_STEAMID，
# 写入 mr1v1_match.sma 在 plugin_init 时读取的配置文件，三者齐备才生效，
# 否则维持现有手动 .start 流程（见 AGENT_ARCHITECTURE_DESIGN.md 第6节）
MATCH_MODE_INI="$SCRIPT_DIR/cstrike/addons/amxmodx/configs/mr1v1_match_mode.ini"
if [ -n "$MATCH_ID" ] && [ -n "$P0_STEAMID" ] && [ -n "$P1_STEAMID" ]; then
  cat > "$MATCH_MODE_INI" <<EOF
; 比赛模式配置，由start.sh按容器环境变量生成，请勿手动编辑
mr1v1_match_id = $MATCH_ID
mr1v1_p0_steamid = $P0_STEAMID
mr1v1_p1_steamid = $P1_STEAMID
EOF
  # BOT_TEST_MODE=1：仅供端到端测试容器使用，双方slot由2个Bot顶替，无需真实玩家连入；
  # 正式排位赛容器不注入此变量，不影响真实比赛的身份校验流程
  if [ -n "$BOT_TEST_MODE" ]; then
    echo "mr1v1_bot_test_mode = $BOT_TEST_MODE" >> "$MATCH_MODE_INI"
  fi
else
  rm -f "$MATCH_MODE_INI"
fi

# GATEWAY_HTTP 为agent注入的完整上报地址(.../record)，mr1v1.ini的mr1v1_gateway_http
# 不含/record后缀(由ReportEvent拼接)，这里去掉末尾的/record再写入
if [ -n "$GATEWAY_HTTP" ]; then
  sed -i "s#^mr1v1_gateway_http.*#mr1v1_gateway_http = ${GATEWAY_HTTP%/record}#" \
    "$SCRIPT_DIR/cstrike/addons/amxmodx/configs/mr1v1.ini"
fi

# 外部cvar覆盖：如果 config-overrides/server.cfg 存在（通常由外部volume挂载提供），
# 逐行读取里面的 cvar "value"，合并进 cstrike/server.cfg
# （已存在则覆盖值并保留注释，不存在则追加新行）。
# 覆盖文件里只需写要改的cvar，不用是完整server.cfg。
OVERRIDE_CFG="$SCRIPT_DIR/config-overrides/server.cfg"
if [ -f "$OVERRIDE_CFG" ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    # 跳过空行/注释行（// 或 ;）
    [[ "$line" =~ ^[[:space:]]*(//|;|$) ]] && continue

    cvar=$(awk '{print $1}' <<< "$line")
    value=$(sed -E 's/^[^"]*"([^"]*)".*/\1/' <<< "$line")
    [ -z "$cvar" ] && continue

    "$SCRIPT_DIR/set_cvar.sh" "$cvar" "$value" "$SCRIPT_DIR/cstrike/server.cfg"
  done < "$OVERRIDE_CFG"
fi

exec "$SCRIPT_DIR/hlds_linux" \
  -game cstrike \
  +map "${MAP:-aim_map}" \
  +maxplayers "${MAXPLAYERS:-10}" \
  +port "${PORT:-27015}" \
  -norestart \
  -insecure \
  -nobreakpad \
  +sv_lan 1 \
  -pingboost 2 \
  -bots \
  +bot_enable 1 \
  +ip 0.0.0.0 \
  "$@"
