# 总体架构（Architecture）

## 问题

ToB 场景与 ToC（lora-scripts-next 社区版）的目标互斥：

| | ToC 社区版 | ToB 企业版 |
|---|---|---|
| 用户 | 单用户，"点了就跑" | 多用户（算法工程师团队） |
| 算力 | 单卡单机 | 多卡多机，算力池化 |
| 调度 | 无队列，本地直跑 | 任务队列、配额、抢占 |
| 部署 | 双击启动 localhost | 私有部署，可能有网闸/上层网络隔离 |
| 支持对象 | 小白用户 | 拿工资的运维/工程师 |

两套需求塞在同一 codebase 里会互相拖累，因此企业版独立成仓、独立架构，**不与社区版合并，也不共享核心代码**。

## 架构总览

```
┌─────────────────────────────────────────────┐
│ server（Go 主控，单 exe，embed Vue 前端）      │
│  用户系统 / 任务队列 / 计量计费 / 展示层        │
└──────────────▲──────────────────────────────┘
               │ 长连接（节点主动发起，agent-pull）
     ┌─────────┴─────────┐─────────────┐
     │ agent（Go 被控端） │  agent      │  agent  ×N
     │  注册/心跳/执行     │  ...        │  ...
     └─────────┬─────────┘
               │ 进程拉起（cmd）
        ┌──────┴───────┐
        │ vendor 引擎    │  Python venv（按引擎/任务隔离，照旧）
        │ sd-scripts    │
        │ ai-toolkit    │
        │ ...           │
```

## 关键决策

1. **Go 控制面 + Python 训练面**。
   调度、队列、API、进程管理、计量用 Go（静态编译、无 GIL/打包问题）；
   Python 只负责"算"——训练进程从 vendor 源码拉起，agent 管生命周期。

2. **agent-pull 部署模型**。
   主控可放公网/上层网络；算力节点只要出网即可，
   `agent --server <addr> --token <access-token>` 启动即注册。
   节点不开入站端口，客户防火墙零改动。
   （参照：GitLab Runner、Tailscale、kubelet。）

3. **vendor 快照交付，不做运行时 patch**。
   企业客户要的是"别人改过、验证过的本土可用源码"，
   vendor/ 下为 pinned + patched 的完整快照（见 README 引擎表）。
   不采用：安装时拉上游再打 patch（部署风险不可控）。

4. **web 放在 server/ 内部**。
   主控交付形态是单个 exe，前端 build 产物用 Go embed 打进二进制。
   agent 侧无前端，最多一个简单 TUI（本地排障用）。

5. **交付形态三档**：仅引擎源码（客户自行改造）/ 仅 agent / 全家桶。
   CI 需要对应三条打包路径。

## Monorepo 布局

```
server/        Go 主控
server/web/    Vue 前端（embed 进 server）
agent/         Go 被控端
vendor/        训练引擎源码快照（含 next-integrated 自研引擎）
docs/design/   设计文档（本目录）
docs/tasks/    任务书 / 验收报告
```

## 与社区版的边界

- 不共享代码、不互相合并；bug 修复如需双轨，人工同步。
- 社区版继续做减法（单进程管一张卡），企业版做池化和队列。
- vendor 引擎快照**从社区仓导出**（`extensions/*/source`、`vendor/sd-scripts`），
  导出后独立演进，升级时重新导出验证。
