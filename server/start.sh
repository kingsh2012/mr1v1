#!/bin/bash
# CS 1.6 1v1 server startup script
# Usage: ./start.sh [extra hlds args]
# 可通过环境变量覆盖（参考 .env.example）：SERVER_HOSTNAME, MAXPLAYERS, RCON_PASSWORD, PORT

# +ip 0.0.0.0：显式声明绑定地址，跳过引擎对容器hostname的gethostbyname()解析
# （host网络模式下容器hostname无/etc/hosts记录，会报"Invalid hostname"警告），
# 同时保持绑定0.0.0.0（含127.0.0.1，本机RCON可用）
# -nobreakpad：关闭崩溃上报模块，减少容器内无Steam客户端导致的
# steamclient.so/IPC超时等无害日志噪音（不影响SteamAPI_Init失败的提示，那条无法消除）

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
export LD_LIBRARY_PATH="$SCRIPT_DIR:$LD_LIBRARY_PATH"

# hostname 含空格，引擎的 +hostname 命令行参数无法正确传递（按空格截断），
# 故改为 sed 写入 server.cfg；rcon_password 为统一管理一并放在这里
if [ -n "$SERVER_HOSTNAME" ]; then
  sed -i "s/^hostname .*/hostname \"$SERVER_HOSTNAME\"/" "$SCRIPT_DIR/cstrike/server.cfg"
fi
if [ -n "$RCON_PASSWORD" ]; then
  sed -i "s/^rcon_password .*/rcon_password \"$RCON_PASSWORD\"/" "$SCRIPT_DIR/cstrike/server.cfg"
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
  +map aim_map \
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
