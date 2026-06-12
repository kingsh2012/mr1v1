---
name: release
description: 发版流程——commit & push未提交改动，打git tag，本地docker build并push镜像到阿里云ACR，更新docker-compose.prod.yml镜像tag并提交。当用户说"发版"/"commit & push & 发版"/"release"时使用。
---

# 发版流程 (release)

本仓库（CS 1.6 1v1平台 `/data/rehlds`）没有CI/CD，发版=本地手动完成以下步骤。
**每一步都涉及推送到远程（git push / docker push），属于影响共享状态的操作**，
执行前按下方"确认点"与用户确认，不要自行跳过。

## 步骤

1. **确定新版本号**
   - 当前生产版本号以 `server/docker-compose.prod.yml` 中 `image:` 的tag为准（如 `v0.2.3`）
   - 默认规则：patch号 +1（`v0.2.3` → `v0.2.4`）。如果改动明显是功能性的，可以和用户确认是否要+1 minor号
   - 检查 `git tag -l` 确保新tag不重复

2. **commit & push 代码改动**
   ```bash
   git status
   git add <相关文件>
   git commit -m "..."
   git push
   ```
   按仓库惯例：commit message 用中文，第一行 `<type>: <概述>`（type如feat/docs/chore/fix），
   正文说明改了什么、为什么。

3. **创建并推送 git tag**
   ```bash
   git tag -a vX.Y.Z -m "vX.Y.Z: <本次发版核心内容一句话总结>"
   git push origin vX.Y.Z
   ```

4. **本地 docker build**（确认点：构建上下文是`server/`目录，COPY了整个目录含地图资源，
   构建产物较大——build前可用`df -h .`检查磁盘剩余空间，确保>2GB）
   ```bash
   cd /data/rehlds/server
   docker build -t registry.cn-beijing.aliyuncs.com/kingsh2012/rm1v1:vX.Y.Z .
   ```
   注意：`apt-get install` 那一层通常会被docker层缓存命中（不变则秒过），
   主要耗时在 `COPY . .` 和 export镜像层。

5. **docker push 到镜像仓库**（确认点：这是生产镜像仓库，push后线上重新拉取该tag即生效；
   先确认 `~/.docker/config.json` 里已有 `registry.cn-beijing.aliyuncs.com` 的登录凭证）
   ```bash
   docker push registry.cn-beijing.aliyuncs.com/kingsh2012/rm1v1:vX.Y.Z
   ```

6. **更新 `server/docker-compose.prod.yml` 镜像tag**
   - 注意：本地build/push用的endpoint是 `registry.cn-beijing.aliyuncs.com`（公网），
     但 `docker-compose.prod.yml` 里用的是 `registry-vpc.cn-beijing.aliyuncs.com`（VPC内网，生产服务器拉取用）。
     **两者指向同一个ACR仓库/命名空间**，只改tag号，不要改endpoint前缀。
   ```bash
   # 把 image: registry-vpc.cn-beijing.aliyuncs.com/kingsh2012/rm1v1:v旧版本
   # 改成 image: registry-vpc.cn-beijing.aliyuncs.com/kingsh2012/rm1v1:vX.Y.Z
   ```

7. **commit & push 这个compose文件改动**
   ```bash
   git add server/docker-compose.prod.yml
   git commit -m "chore: 生产镜像tag更新到vX.Y.Z

   对应已构建并推送的 registry.cn-beijing.aliyuncs.com/kingsh2012/rm1v1:vX.Y.Z（说明本次镜像内容）"
   git push
   ```

## 注意事项

- 如果用户只说"commit & push"而没说"发版"，**不要**自动执行3-7步（tag/docker build/push）——
  只做第2步。"发版"才触发完整流程。
- 第4-5步（docker build/push）耐心等待，build可能需要1-2分钟，push取决于网络。
- 如果磁盘空间紧张（`df -h .` 剩余<2GB），先用 `docker system df` 看是否有可回收的旧镜像/build cache，
  和用户确认后再清理（不要自行删除其他不相关的镜像）。
