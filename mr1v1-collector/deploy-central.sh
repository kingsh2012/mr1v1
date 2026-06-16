#!/bin/bash
# 在 192.144.237.182 上执行：部署 mr1v1 consumer + backend
# 依赖：go 1.21+，screen 或 nohup
set -e

DEPLOY_DIR="/opt/mr1v1"
REPO_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== 1. 创建PG数据库和用户 ==="
PGPASSWORD='Dw2Nvep6435i19kd' psql -h 10.2.8.8 -p 5433 -U root -d postgres <<'EOSQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'mr1v1') THEN
    CREATE USER mr1v1 WITH PASSWORD 'Mr1v1Db2026!';
  END IF;
END$$;

SELECT 'CREATE DATABASE mr1v1 OWNER mr1v1'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'mr1v1')\gexec

GRANT ALL PRIVILEGES ON DATABASE mr1v1 TO mr1v1;
EOSQL
echo "PG 用户/库已就绪"

echo "=== 2. 编译 ==="
mkdir -p "$DEPLOY_DIR"
cd "$REPO_DIR"
go build -o "$DEPLOY_DIR/mr1v1-consumer" ./cmd/consumer
go build -o "$DEPLOY_DIR/mr1v1-backend" ./cmd/backend
echo "编译完成"

echo "=== 3. 拷贝配置文件 ==="
cp cmd/consumer/consumer.yml "$DEPLOY_DIR/consumer.yml"
cp cmd/backend/backend.yml   "$DEPLOY_DIR/backend.yml"

echo "=== 4. 启动服务（screen） ==="
screen -S mr1v1-consumer -X quit 2>/dev/null || true
screen -S mr1v1-backend  -X quit 2>/dev/null || true

screen -dmS mr1v1-consumer bash -c "cd $DEPLOY_DIR && ./mr1v1-consumer -conf consumer.yml >> consumer.log 2>&1"
screen -dmS mr1v1-backend  bash -c "cd $DEPLOY_DIR && ./mr1v1-backend  -conf backend.yml  >> backend.log  2>&1"

sleep 2
echo "=== 5. 检查进程 ==="
screen -ls | grep mr1v1 || true
echo ""
echo "=== 完成 ==="
echo "日志位置: $DEPLOY_DIR/consumer.log  $DEPLOY_DIR/backend.log"
echo "backend API: http://192.144.237.182:8080"
echo "查看日志: screen -r mr1v1-consumer / screen -r mr1v1-backend"
