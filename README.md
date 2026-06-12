# CS 1.6 1v1 对战平台 — 服务端

基于 PROCS.PRO 架构参考，重新立项开发的 CS 1.6 1v1 竞技服务端平台。

## 参考来源

PROCS.PRO 是作者自 2020-2024 年独立开发的 CS 1.6 **5v5** 竞技服务端，由服务端、启动器、消费端三部分组成。本项目以此为架构原型重新立项，详细介绍见 [PROCS.PRO_PROJECT_OVERVIEW.md](./PROCS.PRO_PROJECT_OVERVIEW.md)。

## 与 PROCS.PRO 的关系

**参考复用的设计：**
- 整体三层架构（服务端 + 启动器 + 消费端）
- 消息信封格式（timestamp / token / type / version / data）
- 数据采集字段设计（PlayerDamage / PlayerDeath / PlayerDetails）
- 配置模板系统（按模式渲染写入配置文件）
- WebSocket 上报宿主机状态的机制
- Docker 容器化部署方式

**重新开发的部分：**
- 各组件依赖升级到最新版本
- 1v1 比赛插件（AMXModX .sma）全新开发，不沿用 `procs_cmps.sma`（原插件按 5v5 设计，逻辑差异较大）
- 启动器适配新的调度接口
- 消费端根据新数据字段需求调整

## 阶段目标

见 [ROADMAP.md](./ROADMAP.md)。

## 数据上报

`mr1v1_match.sma` 上报到gateway的事件类型、JSON字段及触发条件，见 [MR1V1_EVENTS.md](./MR1V1_EVENTS.md)。

## 参考文档

- [docs.hlds.run](https://docs.hlds.run/) — HLDS/ReHLDS 相关文档（cvar、部署、引擎行为等）
