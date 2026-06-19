#!/bin/bash
# 构建+推送新版rehlds镜像，并在manager-backend注册并激活为当前生效配置。
# 用法: ./release_rehlds_image.sh vX.Y.Z
#
# 前置条件：
#   - 已 docker login registry.cn-beijing.aliyuncs.com
#   - 中心栈主机(CENTRAL_HOST)的 /opt/mr1v1/.env 或 docker-compose-mr1v1-central.yml
#     里有 INTERNAL_API_KEY / DB_PASS，本脚本通过SSH heredoc在远端取值，
#     密码不会出现在本机或远端的命令行参数里（避免ps aux/history泄露）
#   - rehlds主机(REHLDS_HOST)上的agent能访问到同一个阿里云镜像仓库

set -euo pipefail

VERSION="${1:?用法: $0 vX.Y.Z}"
IMAGE="registry.cn-beijing.aliyuncs.com/kingsh2012/mr1v1-rehlds:${VERSION}"
CENTRAL_HOST="${CENTRAL_HOST:-192.144.237.182}"
REHLDS_HOST="${REHLDS_HOST:-60.205.152.236}"
REHLDS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../mr1v1-rehlds" && pwd)"

echo "==> 构建镜像 ${IMAGE}"
docker build -t "${IMAGE}" "${REHLDS_DIR}"

echo "==> 推送镜像"
docker push "${IMAGE}"

echo "==> 在manager-backend注册并激活该镜像配置"
ssh "root@${CENTRAL_HOST}" bash -s <<EOF
set -a; source /opt/mr1v1/.env; set +a
CONFIG_ID=\$(curl -s -X POST -H "X-API-Key: \${INTERNAL_API_KEY}" -H 'Content-Type: application/json' \
  -d '{"image":"${IMAGE}","version":"${VERSION}"}' \
  http://127.0.0.1:8181/api/manager/rehlds-configs | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["id"])')
curl -s -X PATCH -H "X-API-Key: \${INTERNAL_API_KEY}" \
  http://127.0.0.1:8181/api/manager/rehlds-configs/\${CONFIG_ID}/activate
echo
echo "已激活 config id=\${CONFIG_ID}"
EOF

echo "==> 在rehlds主机预拉取镜像，避免下一局建房时冷拉取耗时"
ssh "root@${REHLDS_HOST}" "docker pull ${IMAGE}"

echo "==> 完成：${IMAGE} 已构建、推送、注册、激活、预拉取"
