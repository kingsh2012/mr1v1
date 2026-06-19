#!/bin/bash
# 触发一局 bot_test_mode=true 的真实比赛：走真实建房API，
# 容器内由2个专家难度Bot顶替双方slot，自动打完一整局(含真实回合/比分上报)。
# 用法: ./trigger_bot_match.sh ["server_name"]

set -euo pipefail

CENTRAL_HOST="${CENTRAL_HOST:-192.144.237.182}"
SERVER_NAME="${1:-E2E Bot Test}"
P0_STEAMID="${P0_STEAMID:-STEAM_0:0:111111}"
P1_STEAMID="${P1_STEAMID:-STEAM_0:0:222222}"

ssh "root@${CENTRAL_HOST}" bash -s <<EOF
set -a; source /opt/mr1v1/.env; set +a
curl -s -X POST -H "X-API-Key: \${INTERNAL_API_KEY}" -H 'Content-Type: application/json' \
  -d '{"p0_steamid":"${P0_STEAMID}","p1_steamid":"${P1_STEAMID}","server_name":"${SERVER_NAME}","bot_test_mode":true}' \
  http://127.0.0.1:8181/api/manager/matches
echo
EOF
