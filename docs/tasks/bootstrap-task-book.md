# Next Trainer Enterprise 总任务书（Bootstrap）

## 计划元数据

- Plan ID: EE-BOOTSTRAP-20261011
- Version: v1-docs-first
- Last updated: 2026-10-11 Asia/Shanghai
- Canonical progress file: `docs/tasks/bootstrap-task-book.md`
- Current branch: `dev`
- Current active phase: Phase 0 文档与骨架（本文件）
- Design docs:
  - `docs/design/architecture.md`
  - `docs/design/server-agent-protocol.md`
  - `docs/design/engine-process-contract.md`
  - `docs/design/metering-and-billing.md`
  - `docs/design/environment-provisioning.md`
- Sub task books:
  - `docs/tasks/agent-server-phase12-task-book.md`（Phase 1+2 细化，2026-10-12）

## 目标

交付面向多卡多机多用户的企业训练平台 MVP：
Go 主控（用户/队列/计量/展示）+ Go 被控端（agent-pull 注册）+
vendor patched 引擎源码（进程边界标准化），
三种交付形态（引擎源码 / agent / 全家桶）。

## 范围与约束

- In scope:
  - agent：注册/心跳/重连、provision 执行、任务拉起/停止/进程树回收、
    nvml 采样与事件上报、本地 WAL。
  - server：节点管理、任务队列（单机多节点调度）、用户/团队、
    计量落库与简单计价器、Vue 展示层（embed）。
  - 引擎契约：JSONL 结构化输出 wrapper，sd-scripts 首个适配。
  - CI：三种交付形态打包。
- Out of scope（MVP 不做）:
  - 多机分布式训练（字段预留）。
  - next-integrated 自研引擎深度优化（先跑通标准流程，卖点级）。
  - 容器化隔离、防篡改计量。
  - 与社区版的任何代码合并。
- Constraints:
  - 主控/被控协议 schema 先于实现定版，新增字段 optional。
  - 训练核心逻辑不改，标准化只加 wrapper 层。
  - 每个 Phase 完成更新本任务书 + 验收证据。

## 执行阶段

### Phase 0: 文档与骨架（当前）

- Outputs: monorepo 骨架、vendor 引擎快照、本任务书 + 5 份设计文档。
- Completion criteria: 设计文档 review 通过，协议/引擎契约无悬而未决的硬问题。
- 状态：✅ 骨架与 vendor 迁移完成（dev 分支），设计文档待 review。

### Phase 1: agent 骨架

- Purpose: 先跑通"exe 启动 → 注册 → 收指令 → 拉起一个 python 进程"全链路。
- Outputs: `agent/` 可构建 exe；注册握手、心跳重连、进程树回收；
  双 lane 与资产依赖门。（细化见 `agent-server-phase12-task-book.md`；
  2026-10-12 起 agent 无状态无 WAL，TUI 暂缓）
- Validation: 对真实 server 的端到端联调；杀进程恢复测试；断网缓冲补传测试。

### Phase 2: server 核心 + 协议落地

- Purpose: 协议的真实实现，节点/任务/事件的最小闭环。
- Outputs: `server/`：ws 接入、节点表、资源池与授权、任务队列（按池 FIFO +
  资源匹配起步）、事件流接收落库、用户系统（RBAC）、数据集管理
  （local/s3 backend）；`server/web` 页面（登录/节点/任务/数据集/池/用户）。
  （细化见 `agent-server-phase12-task-book.md`）
- Validation: agent+server 端到端跑通 sd-scripts 单卡训练（用契约 wrapper）。

### Phase 3: 引擎契约适配（sd-scripts 先行）

- Purpose: 验证"配置文件进、JSONL 出"契约的可行性，形成改造模板。
- Outputs: sd-scripts wrapper + `engine.toml`；进度/artifact/error 帧规范落地；
  单卡/多卡（本机 accelerate）拉起。
- Validation: 真实训练任务指标流完整到达 server；SIGTERM 优雅停止有 checkpoint。
- 后续引擎按难度排队：musubi-tuner → anima-lora → ai-toolkit → diffsynth-studio。

### Phase 4: 计量计费

- Outputs: nvml 采样链路、事件流用量聚合、费率表计价器、
  用量/利用率/排队 dashboard 数据接口 + 开放查询 API。
- Validation: 用量流水与真实训练时长对账一致；断网补传后数据无洞。

### Phase 5: 展示层与多用户

- Outputs: 团队/配额（叠加在资源池之上）、任务提交与详情页完善、
  节点池视图、计量 dashboard。（用户系统与 RBAC 已在 Phase 2 就位）
- Validation: 多用户并发提交，队列与配额行为符合预期。

### Phase 6: 交付加固

- Outputs: 三档打包 CI（引擎源码 / agent / 全家桶）、离线介质、
  安装文档、私有部署网络配置指南。
- Validation: 干净机器从 exe 到训练完成的全程演练。

## 风险

| 风险 | 缓解 |
|---|---|
| 协议定版后发现硬伤 | Phase 1/2 只做骨架协议，真实任务跑通后再冻结字段 |
| vendor 引擎标准化改造量超预期 | sd-scripts 先行验证模板；diffsynth 圈功能子集再适配 |
| next-integrated 拖主线 | 明确"卖点级"定位，独立分支，不阻塞 1-5 |
| 双轨 bug 修复遗漏 | vendor 快照升级走"重新导出 + 验证"流程，记录来源 commit |
