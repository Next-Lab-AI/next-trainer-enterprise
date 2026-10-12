# 任务生命周期与调度（Job Lifecycle）

## 问题

任务状态机是 server、agent、协议三方共享的语义，必须一处定义。
mikazuki 的五态（CREATED/RUNNING/FINISHED/TERMINATED/FAILED）在社区版
够用，企业版多了排队、池调度和资产依赖，状态与迁移需要完整写下来。

## 状态机

```
                 server 侧                     agent 侧
              ┌──────────┐   下发    ┌──────────┐
   提交 ──▶   │  QUEUED  │ ───────▶ │ CREATED  │
              └──────────┘          └────┬─────┘
                   ▲                     │ 资产依赖检查
                   │              ┌──────▼───────┐  缺资产
                   │              │   WAITING    │ ◀────────┐
                   │              └──────┬───────┘          │
                   │                     │ asset.ready       │ asset.failed
                   │              ┌──────▼───────┐          │
                   │              │   RUNNING    │ ─────────┤
                   │              └──┬───┬───┬───┘          │
                   │     正常退出 0  │   │   │ 退出码非 0     │
                   │           ┌─────▼┐  │   └──────────────▼──┐
                   │           │FINISH│  │                     │
                   │           └──┬───┘  │  ┌───────────────┐  │
                   │              │      ├─▶│  TERMINATED   │  │
                   │              │      │  │ (stop/kill/    │  │
                   │              │      │  │  preempt)      │  │
                   │              │      │  └───────────────┘  │
                   │              │      │  ┌───────────────┐  │
                   │              │      └─▶│    FAILED     │◀─┘
                   │              │         └───────────────┘
```

| 状态 | 位置 | 含义 |
|---|---|---|
| QUEUED | server | 池队列中等待；节点空闲 GPU 匹配到才下发 |
| CREATED | agent | 已收到 job.start，正在校验配置/检查资产 |
| WAITING | agent | 资产未就绪，maintenance lane 下载中 |
| RUNNING | agent | 训练进程存活 |
| FINISHED | 终态 | 退出码 0 |
| FAILED | 终态 | 退出码非 0 / 资产拉取失败 / 配置校验失败 |
| TERMINATED | 终态 | 人为终止（stop/kill/抢占） |

事件与状态一一对应：`job.queued / job.created / job.waiting{reason,
missing[]} / job.starting / job.progress / job.finished / job.failed{code,
msg} / job.terminated{reason}`。抢占是 TERMINATED 的 reason=
`preempted`（事件别名 `job.preempted`）。

## 调度规则

- **队列按池分**：每池一条 FIFO；提交时校验用户↔池与数据集↔池授权，
  不过检直接拒绝（不进队列）。
- **派发条件**：池队列头部任务 + 池内某节点有足够**实际空闲** GPU
  （型号/labels 匹配）。池不预留资源；多池共享节点由排队天然消化。
- **agent 无资格决策**：server 派谁就是谁；agent 只做事实校验
  （GPU 真的空、配置路径合法），校验失败回报 `job.failed`。
- **节点掉线**：RUNNING 任务的心跳随连接中断而缺失 → server 标记
  节点离线，任务保持 RUNNING 直至 agent 重连上报事实
  （进程还在 → 继续；没了 → FAILED/TERMINATED）。不靠超时猜杀。

## 双 lane（agent 侧）

| lane | 内容 | 资源 |
|---|---|---|
| compute | 训练/打标/翻译任务 | 占 GPU，串行于所分配的卡 |
| maintenance | 资产下载、provision、env.gc | 不占 GPU，并发数可配（默认 2） |

- 依赖门：compute 任务 `assets[]` 未就绪 → WAITING，自动在
  maintenance lane 排队下载（按 URI 去重、引用计数），就绪自动开跑；
  下载失败 → FAILED，不悬挂。
- maintenance 任务同样上报事件（`asset.ready/failed`、
  `provision.ready/broken`），在 web 节点详情可见。

## 抢占（admin 手动）

- 入口：admin 在 web 对 RUNNING 任务执行"抢占"（用于给排队的高优任务
  腾卡；首版无自动优先级抢占）。
- 流程：server 发 `job.stop{preempt:true}` → agent SIGTERM →
  引擎在当前 step 边界 checkpoint 后退出（尽力而为）→
  事件 `job.preempted` → GPU 释放，池队列下一个任务派发。
- 宽限期可配（默认 60s），超时升级 SIGKILL。
- 计量口径：被抢占任务的用量照常入账（费率规则是 pricing 层的事）。

## 优雅停止与强杀

- `job.stop`：SIGTERM → 宽限 → SIGKILL 兜底；整棵进程树回收
  （suspend→快照→kill→wait，防 fork 竞态）。
- `job.kill`：直接 SIGKILL 整组，用于卡死场景；回收后核对 GPU
  进程列表确认显存释放。

## 不采用

- 自动优先级抢占（首版池内 FIFO + admin 手动抢占足够）。
- agent 侧排队（队列只有 server 一处，agent 收到即执行或 WAITING）。
- 节点掉线超时判死（事实派：等 agent 回来说，不猜）。
