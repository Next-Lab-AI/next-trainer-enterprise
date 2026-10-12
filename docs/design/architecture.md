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
│  用户/RBAC · 资源池 · 队列 · 数据集存储        │
│  （local/S3）· 计量 · 展示层                  │
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

6. **资源池（pool）是调度与访问控制的一等模型**。
   节点↔池多对多，归属是 server 侧元数据（admin 划分，
   agent 启动参数 `--pool` 只是初始建议）；用户↔池、数据集↔池授权；
   队列按池分 FIFO，池不预留资源，任务排队到节点有空闲 GPU 才下发。

7. **数据集与产物由 server 统一管理**。
   存储 backend 抽象（默认 server 本地磁盘，可选 S3/MinIO）；
   agent 不持有数据目录权威，按 URI+sha256 主动拉取，支持
   file/s3/https-server 三种 scheme；训练产物回传同一存储层。

8. **日志全链路统一 JSONL schema**（ts/level/source/node_id/job_id/msg）。
   协议 log 帧与节点落盘文件同一份格式，Filebeat/Logstash 可直接
   tail 采集，不依赖平台通路。

9. **完整用户系统从 Phase 2 就在**（登录/RBAC/access token 签发），
   不是后期补的。agent 零入站端口、无任何自升级通路，
   升级由运维手动替换二进制。

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
