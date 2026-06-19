#!/bin/bash
# 手动销毁一局测试比赛(容器+RCON倒计时，失败则强制stop兜底)。
# 用法: ./destroy_match.sh <match_id>

set -euo pipefail

MATCH_ID="${1:?用法: $0 <match_id>}"
CENTRAL_HOST="${CENTRAL_HOST:-192.144.237.182}"

ssh "root@${CENTRAL_HOST}" bash -s <<EOF
set -a; source /opt/mr1v1/.env; set +a
curl -s -X POST -H "X-API-Key: \${INTERNAL_API_KEY}" \
  http://127.0.0.1:8181/api/manager/matches/${MATCH_ID}/destroy
echo
EOF
