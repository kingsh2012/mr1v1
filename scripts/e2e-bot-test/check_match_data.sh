#!/bin/bash
# 查询某局比赛在Postgres里的落库情况(manager_matches状态机 + 遥测表)。
# 数据库密码只在中心栈服务器(CENTRAL_HOST)的远端shell里取值，
# 通过 --env-file 传给临时psql容器，不会出现在任何机器的命令行参数/history里。
# 用法: ./check_match_data.sh <match_id>

set -euo pipefail

MATCH_ID="${1:?用法: $0 <match_id>}"
CENTRAL_HOST="${CENTRAL_HOST:-192.144.237.182}"

ssh "root@${CENTRAL_HOST}" bash -s <<EOF
DB_PASS=\$(grep -oP 'DB_PASS=\K.*' /opt/mr1v1/docker-compose-mr1v1-central.yml | head -1)
cat > /tmp/pg.env <<INNEREOF
PGPASSWORD=\${DB_PASS}
INNEREOF
docker run --rm --env-file /tmp/pg.env postgres:16-alpine \
  psql -h 10.2.8.8 -p 5433 -U mr1v1 -d mr1v1 \
  -c "SELECT match_id,state,p0_steamid,p1_steamid,created_at,updated_at FROM manager_matches WHERE match_id='${MATCH_ID}';" \
  -c "SELECT count(*) AS rounds_reported, max(round) AS last_round FROM telemetry_round_ends WHERE match_id='${MATCH_ID}';" \
  -c "SELECT match_id,end_reason,winner_slot,wins0,wins1,ts FROM telemetry_match_ends WHERE match_id='${MATCH_ID}';"
rm -f /tmp/pg.env
EOF
