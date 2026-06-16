---
name: release
description: 发版流程——明确哪些镜像本地打包、哪些走GitHub CI；打tag/构建/推镜像/部署到对应服务器/自测。当用户说"发版"/"release"/"部署"时使用。
---

# 发版流程 (release)

## 打包方式分工

| 镜像 | 打包方式 | 原因 |
|---|---|---|
| `mr1v1-rehlds` (ReHLDS游戏服务器) | **本地 docker build + push** | 含大型游戏二进制/地图，CI runner带宽慢；本地已有缓存层 |
| `mr1v1-consumer` | **GitHub Actions** (`mr1v1-consumer-v*` tag触发) | Go跨平台编译，CI更干净；无大文件 |
| `mr1v1-backend` | **GitHub Actions** (`mr1v1-backend-v*` tag触发) | 同上 |
| `mr1v1-agent` | **GitHub Actions** (`mr1v1-agent-v*` tag触发) | 同上 |

镜像仓库统一：`registry.cn-beijing.aliyuncs.com/kingsh2012/`
（生产机拉取用 `registry-vpc.cn-beijing.aliyuncs.com`，同一仓库不同endpoint）

---

## 部署目标

| 机器 | 用途 | 服务 |
|---|---|---|
| 192.144.237.182 | 中心栈 | mr1v1-consumer、mr1v1-backend |
| 60.205.152.236 | rehlds主机 + agent | mr1v1-agent、mr1v1-rehlds容器（按局拉起） |

---

## 一、rehlds 游戏服务器镜像（本地打包）

### 版本号
- 当前tag以 `server/docker-compose.prod.yml` 里的 `image:` tag 为准
- 默认 patch +1，功能性改动可 +1 minor

### 步骤

1. **检查磁盘空间**
   ```bash
   df -h /data/rehlds
   ```

2. **本地 docker build**
   ```bash
   cd /data/rehlds/server
   docker build -t registry.cn-beijing.aliyuncs.com/kingsh2012/mr1v1-rehlds:vX.Y.Z .
   ```

3. **docker push**
   ```bash
   docker push registry.cn-beijing.aliyuncs.com/kingsh2012/mr1v1-rehlds:vX.Y.Z
   ```

4. **更新 docker-compose.prod.yml 并提交**
   - 修改 `server/docker-compose.prod.yml` 里的 `image:` tag
   - `git add server/docker-compose.prod.yml && git commit -m "chore: 生产rehlds镜像tag更新到vX.Y.Z" && git push`
   - 同步更新 `docker/docker-compose-mr1v1-agent.yml` 中 `DOCKER_IMAGE` 环境变量的 tag

---

## 二、Go 服务（GitHub CI 打包）

### 步骤

1. **确认代码已 push**
   ```bash
   git status && git push
   ```

2. **打 tag 触发构建**（只打需要更新的服务）
   ```bash
   git tag mr1v1-consumer-v0.1.0 -m "mr1v1-consumer v0.1.0" && git push origin mr1v1-consumer-v0.1.0
   git tag mr1v1-backend-v0.1.0  -m "mr1v1-backend v0.1.0"  && git push origin mr1v1-backend-v0.1.0
   git tag mr1v1-agent-v0.1.0   -m "mr1v1-agent v0.1.0"    && git push origin mr1v1-agent-v0.1.0
   ```

3. **跟踪构建进度**
   ```bash
   gh run list --repo kingsh2012/procs-1v1 --limit 5
   gh run watch --repo kingsh2012/procs-1v1
   ```

4. **CI 成功后镜像自动推入阿里云仓库**

---

## 三、部署

### 首次部署（传 compose 文件）
```bash
ssh root@192.144.237.182 "mkdir -p /opt/mr1v1"
scp /data/rehlds/docker/docker-compose-mr1v1-central.yml root@192.144.237.182:/opt/mr1v1/

ssh root@60.205.152.236 "mkdir -p /opt/mr1v1-agent"
scp /data/rehlds/docker/docker-compose-mr1v1-agent.yml root@60.205.152.236:/opt/mr1v1-agent/
```

### 中心栈（192.144.237.182）
```bash
ssh root@192.144.237.182 "
  cd /opt/mr1v1 &&
  docker compose -f docker-compose-mr1v1-central.yml pull &&
  docker compose -f docker-compose-mr1v1-central.yml up -d &&
  docker compose -f docker-compose-mr1v1-central.yml ps
"
```

### Agent 栈（60.205.152.236）
```bash
ssh root@60.205.152.236 "
  cd /opt/mr1v1-agent &&
  docker compose -f docker-compose-mr1v1-agent.yml pull &&
  docker compose -f docker-compose-mr1v1-agent.yml up -d &&
  docker compose -f docker-compose-mr1v1-agent.yml ps
"
```

---

## 四、自测验证

### 1. 检查服务健康
```bash
curl -sf http://192.144.237.182:8080/healthz && echo OK
curl -s http://192.144.237.182:8080/agents | python3 -m json.tool
ssh root@192.144.237.182 "docker logs mr1v1-consumer --tail 30"
ssh root@192.144.237.182 "docker logs mr1v1-backend --tail 30"
ssh root@60.205.152.236 "docker logs mr1v1-agent --tail 30"
```

### 2. 撮合测试
```bash
curl -s -X POST http://192.144.237.182:8080/matches \
  -H "Content-Type: application/json" \
  -d '{"p0_steamid":"STEAM_0:0:12345","p1_steamid":"STEAM_0:0:67890"}' \
  | python3 -m json.tool
```
返回 `match_id / host_id / port` 表示撮合成功。

### 3. 验证 rehlds 容器（由 agent 拉起）
```bash
ssh root@60.205.152.236 "docker ps | grep mr1v1-match"
ssh root@60.205.152.236 "docker logs mr1v1-match-<match_id> --tail 50"
```

### 4. 文档错误修复
如果 skill 文档或设计文档有误，直接 Edit 修改对应文件（`.claude/skills/release/SKILL.md`、
`AGENT_ARCHITECTURE_DESIGN.md` 等），修完后 `git add && git commit && git push`。

---

## 注意事项

- GitHub CI Secrets 需提前配置：`REGISTRY_USERNAME`、`REGISTRY_PASSWORD`
- 首次 docker push 前确认已登录：`docker login registry.cn-beijing.aliyuncs.com`
- 192.144.237.182 的 PG 库已建好（mr1v1/Mr1v1Db2026!），consumer 首次启动会 AutoMigrate
- agent compose 里 `DOCKER_IMAGE` 需与 rehlds 最新 tag 保持同步
