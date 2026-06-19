#!/bin/bash
# 查看某局比赛容器的关键日志(进度/报错)。容器名格式: mr1v1-match-<match_id>
# 用法: ./check_match_logs.sh <match_id> [tail行数]

set -euo pipefail

MATCH_ID="${1:?用法: $0 <match_id> [tail行数]}"
TAIL="${2:-200}"
REHLDS_HOST="${REHLDS_HOST:-60.205.152.236}"

ssh "root@${REHLDS_HOST}" \
  "docker ps --filter name=mr1v1-match-${MATCH_ID} --format '{{.Names}} {{.Status}}'; echo ---; \
   docker logs mr1v1-match-${MATCH_ID} --tail ${TAIL} 2>&1 | \
   grep -iE 'MR1V1|bot|error|run time|kicked|wins=' || true"
