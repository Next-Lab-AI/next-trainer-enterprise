# Phase 1+2 实施任务书：agent 与 server 最小闭环

## 计划元数据

- Plan ID: EE-P12-20261012
- Version: v1
- Last updated: 2026-10-12 Asia/Shanghai
- Canonical progress file: 本文件
- 上位文档：`docs/tasks/bootstrap-task-book.md`（Phase 1/2 的细化）
- 设计文档：`docs/design/*`（2026-10-12 决策已回写）
- 待产出：`docs/api/protocol-v1.md`（动工第 0 步）

## 0. 目标

跑通最小闭环：**agent 注册 → server 下发 sd-scripts 单卡任务 → 日志/进度回流 web**。
对应总任务书 Phase 1（agent 骨架）+ Phase 2（server 核心 + 协议落地），
Phase 3 的 sd-scripts wrapper 仅做到"能被拉起、能转发 stdout"的最小程度。

## 1. 已拍板决策（2026-10-12，优先于设计文档旧表述，冲突条款已回写）

1. **agent 无状态**：不引入 BoltDB/SQLite；计算节点不保存任何可能过期的元数据，
   一切以实时观测事实（进程存活、GPU 占用、磁盘上的 envs/ 目录）为准。
   唯一例外：单个 JSON 身份文件（node_id + session_token，0600），握手恢复会话用。
2. **不做 mock server**：agent 测试直接对真实 server 联调
   （server 先行出最小 ws 接入，agent 联调即集成测试）。
3. **GPU 抽象层厂商无关**：`agent/internal/gpu/` 定义接口
   （Sampler / Topology / ProcessLister），NVML（go-nvlib）是第一个实现；
   协议指标字段保持中性（`gpu.util` / `gpu.mem_used`，vendor 作为卡属性），
   预留 AMD ROCm / 国产卡实现位。
4. **server 存储 provider 抽象**：`server/internal/store/` 定义 repository 接口，
   首版 SQLite 实现，预留 Postgres/MySQL provider。
5. **前端 Vue 3**，embed 进 server 二进制；包管理器动工时参照社区仓确认。
6. **Go 用常见稳定版**（动工时 `go version` 确认并回填工作区 AGENTS.md）。
7. **agent 任务模式与安装方式对齐 mikazuki**（语义平移，Go 重写）：
   - 任务状态机以 mikazuki TaskStatus 五态为基础（CREATED/RUNNING/FINISHED/
     TERMINATED/FAILED），企业版扩展 WAITING（见决策 15）；
   - 进程树回收沿用 `kill_proc_tree` 语义：先 suspend 父进程再快照子树
     （防 elastic launcher 在快照间隙 fork 新 worker，社区版 #291 的教训）；
   - 任务分 lane（compute / maintenance），敏感环境变量（TOKEN/KEY/SECRET/
     PASSWORD 模式）不落盘；
   - 安装方式：agent 本体单 exe；引擎环境沿袭"按引擎独立 venv"，
     触发方式为 admin 运维操作（决策 14），依赖指纹复用不变
     （见 environment-provisioning）。
8. **日志统一（企业版预期有 Logstash 类采集）**：
   - 全链路一种格式：JSONL，统一字段
     `{ts, level, source, node_id, job_id?, msg, ...}`；
     agent 自身日志、训练进程 stdout/stderr、server 日志同 schema；
   - 协议 `log` 帧与节点落盘文件**同一份 schema**，server 不发明第二种格式；
   - agent 本地落盘 `jobs/<job_id>/stdout.jsonl`（契约 §6）+ `logs/agent.jsonl`，
     Filebeat/Logstash 可直接 tail 文件采集，不依赖本平台通路；
   - server 侧 web 日志视图与对外采集共用同一事件流，可选转发输出
     （文件/syslog）留给交付配置。
9. **安全模型 = agent 零入站 + 任务分配制**：
   - agent 不监听任何端口，所有行为起点是 server 经长连接下发的指令；
   - 指令鉴权 = 握手期 session_token，无 token 无任务；
   - 注入面收敛到两处并显式防御：TOML 配置渲染（路径白名单校验，
     禁止逃出 `<agent_root>`）与资产下载（URI scheme 白名单 + sha256 校验）；
   - web/API 侧为完整用户系统（RBAC），见决策 16；
   - **agent 永无自升级通路**：升级由运维手动替换 exe
     （自升级机制本身是攻击面）。
10. **资产获取走 URI 拉取，不经协议传大文件**：
    - 数据集、底座模型、引擎包统一为 asset：`{uri, dest, sha256, size?}`，
      `job.start` / `provision` 内嵌资产清单，agent 本地拉取；
    - scheme 白名单：`file://`（NAS/共享盘/本地路径）、`s3://`
      （含 MinIO 等兼容实现，endpoint 可配指客户内网）、
      `https://`（仅 server 自身数据集分发端点，见决策 11）；
    - 凭证不出 server：S3 凭证配置在**节点本地**（env/配置文件），
      server 只传 URI——凭证泄漏面不随任务流转；
    - 拉取落盘 `<agent_root>/datasets/`（按 URI 哈希寻址可复用），
      校验失败即任务失败并上报，不带病起跑。
11. **数据集管理收敛在 server**：
    - server 持有数据集注册表（store 落库：名称/版本/sha256/大小/标签），
      存储为 **backend 抽象**：默认 server 本地磁盘（`<server_data>/datasets/`），
      可选 S3（含 MinIO，指客户内网）——与 store/provider 同一套 provider 思路；
    - 上传入口在 server（web/API 上传 → 落 backend → 算 sha256 入注册表）；
    - 下发路径随 backend 不同：本地盘 backend 时 agent 经
      `https://server/datasets/<id>` 认证下载（session_token 鉴权，
      仍是 agent 主动 outbound，不违背零入站模型）；S3 backend 时直接下发
      s3 URI 由 agent 直拉（决策 10 通路）；
    - 训练产物（checkpoint）回传复用同一存储层：默认传回 server 存储，
      归为"产物数据集"，web 可下载。
12. **资源池（pool）是一等模型，归属关系是 server 侧节点元数据**：
    - agent 启动参数 `--description "<节点描述>" --pool <pool_name>`，
      握手 Hello 携带；`--pool` 只是**初始归属建议**，缺省进 `default` 池，
      新池名由 server 自动创建；
    - **节点↔池是多对多关系，以 server 元数据为准**：admin 在 web 划节点，
      可 assign 进多个池、可移出；agent 上报的 pool 值不影响已有归属
      （防止节点侧擅自改归属绕过授权）；description 同理可改；
    - pool 是访问控制维度：admin 授权**用户 ↔ pool**、**数据集 ↔ pool**；
      越池访问在 server 侧拒绝，agent 不感知；
      自动创建的新池默认无授权（仅 admin 可用）；
    - 队列按 pool 分（每池独立 FIFO + 池内 GPU 匹配）；池不做资源预留，
      多池共享节点时没有超额分配问题——任务一律排队，节点有实际空闲
      GPU 才下发；
    - Phase 5 的配额/团队挂在 pool 之上叠加，不推翻本模型。
13. **计量数据从第一天就记录，计价器后置**：
    - 用量流水粒度：`(user, pool, job_id, node_id, gpu_index, gpu_model,
      start, end)`——哪张卡、跑了多久是必备字段；
      start/end 以 server 收到事件的时间落库，agent 另附 elapsed 相对时长
      供校验（不依赖节点绝对时钟，跨时区节点安全）；
    - 不加新管道：event 帧带 GPU 分配与生命周期信息，job 生命周期事件
      全部落 server 事件表，用量是事件流的派生物；
    - metrics 帧按 gpu_index 逐卡采样，不只报节点汇总；
    - Phase 2 只保证**数据落全**；聚合查询/费率计价器在 Phase 4，
      届时历史数据可直接重算，不补洞。
14. **引擎安装是 admin 运维操作**（provision 落地方式）：
    - admin 在 web 节点详情发起"安装引擎"（选引擎/版本/依赖约束）→
      server 下发 `provision` 指令 → agent 幂等执行；
    - 引擎包分发复用 asset 通路：vendor 快照作为资产纳入 server 存储层
      （随全家桶预装或 admin 上传），agent 拉取 + sha256 校验；
    - agent 侧执行 = 建/复用 venv（依赖指纹）→ 装依赖 → 冒烟审计
      （import 自检、torch/CUDA、GPU 可见性）→ 回报 ready/broken + 审计日志；
    - 环境清单由事实扫描（`envs/` 目录）推导，无本地状态库；
    - provision 在 Phase 2 内实现（运维安装是核心闭环的一部分）。
15. **计算/维护双 lane + 资产依赖链**（对齐 mikazuki lane 模型并扩展）：
    - **maintenance lane**：数据集/引擎包下载、provision 安装、env.gc——
      不占 GPU，可与 compute 并行（并发数可配）；
    - **compute lane**：训练/打标任务，占 GPU，池队列派发；
    - **依赖链**：compute 任务的 `assets[]` 未就绪 → 任务不开始，
      进入 **WAITING** 态，agent 自动在 maintenance lane 排下载任务，
      就绪后自动转 RUNNING；
    - 依赖判定在 agent 侧（事实在节点磁盘上），server 只看状态流转：
      `job.waiting {reason:"assets", missing:[...]}` → `asset.ready` →
      `job.starting`；下载失败 `asset.failed` → 任务 FAILED，不无限等待；
    - 同节点多任务依赖同一资产：下载按 URI 去重，引用计数，只下一遍。
16. **完整用户系统从 Phase 2 就在**（企业级，不做"先裸奔后补"）：
    登录/登出、用户 CRUD、RBAC 角色骨架（admin/普通用户）、
    argon2id 密码哈希、会话 token；agent access token 由 admin 在 web
    创建/撤销；首启引导建 admin。团队/配额留 Phase 5（角色体系就位后
    只是加维度）。
17. **节点注册双模式：token 白名单 + 注册窗口**：
    - token 模式为默认（白名单制，见决策 9）；
    - **注册窗口**：admin 在 web 打开 toggle，窗口期内 Hello 免 token——
      方便云算力与小规模场景批量接入；用完由 admin 手动关掉，
      关闭后恢复仅 token。窗口开关只有 admin 能操作；
    - **免 token 仅限首次握手**：两种模式下注册成功都由 server 生成
      节点凭证随 Welcome 下发，agent 存入身份文件；此后该节点一切连接
      凭此凭证认证。顶替已注册节点需要拿到节点本地身份文件，
      窗口期注册的节点与 token 注册的安全性一致。

## 2. 协议 v1 schema 先行（动工第 0 步）

补 `server-agent-protocol.md` 附录 + 出 `docs/api/protocol-v1.md`
（逐消息字段表：类型、必填性、版本规则）：

- Hello / Welcome（node_id 恢复语义、身份文件约定；
  Hello 携带 `description` / `pool`；注册模式 token / 注册窗口（窗口期
  免 token）；不校验节点时钟——调度只看上报事实）
- job.start / job.stop / job.kill（command_id 幂等规则；
  job.start 内嵌 `assets[]` 资产清单 + 渲染后 TOML 配置；
  依赖未就绪时 agent 回 `job.waiting {reason, missing[]}`）
- event / metrics / log（帧格式、序号、去重规则、gap 标记；
  log 帧字段 = 统一 JSONL 日志 schema；event 帧携带 GPU 分配与
  生命周期信息，metrics 帧按 gpu_index 逐卡，用量流水可从事件表派生）
- asset.ready / asset.failed（含 sha256 实测值）
- provision（engine/version/constraints/command_id；
  回报 provision.ready / provision.broken + 审计日志）
- `file` 帧收窄为小文件用途（配置下发、artifact 元数据）；
  大文件一律走决策 10 的 URI 拉取，不进协议
- 分支 `docs/protocol-v1`，一个 commit
- 验收：照 schema 能徒手写两端编解码，无歧义

## 3. agent 实施拆分

Go module `next-trainer-ee/agent`（protocol 包见 §5）。

```
agent/
├── cmd/agent/          main + flag（--server --token --root
│                       --description --pool）
├── internal/conn/      ws 长连接、指数退避重连、心跳、握手恢复
├── internal/exec/      进程拉起、stdout/stderr 分流、进程树回收（Linux 先行，
│                       语义对齐 mikazuki kill_proc_tree：suspend→快照→kill→wait）
├── internal/fetch/     资产拉取：URI scheme 插件（file/s3/https-server）、
│                       sha256 校验、datasets/ 目录寻址复用、失败上报
├── internal/provision/ 引擎环境安装：venv 建/复用（依赖指纹）、pip 安装、
│                       冒烟审计（import/torch/CUDA/GPU 可见性）、幂等回报
├── internal/gpu/       厂商无关接口 + 实现注册表
│   └── nvml/           go-nvlib 实现（首版可 stub 出静态拓扑）
├── internal/jobs/      任务状态机（内存态，五态 + WAITING）、
│                       lane 调度（compute/maintenance 分离，维护不占 GPU）、
│                       资产依赖门（WAITING→下载→自动开跑）、下载按 URI 去重、
│                       command_id 幂等去重、事件/日志/metrics 有界缓冲批量上报
├── internal/log/       统一 JSONL 日志：自身日志 + 训练流归一化落盘，
│                       敏感 env 过滤（TOKEN/KEY/SECRET/PASSWORD 模式）
└── internal/config/    启动 flag + 身份文件（node_id/session_token JSON，0600）
```

关键语义：

- **无状态**：重启后通过观测恢复——扫描 jobs/ 目录确认残留进程、
  GPU 进程列表核对，向 server 上报 facts，由 server 侧 reconcile。
- **幂等**：command_id 内存去重；agent 重启后重复指令通过事实校验兜底
  （任务进程已存在 → 回报 already running，不重复拉起）。
- **进程树回收**：拉起时建独立 process group，stop 走 SIGTERM → 宽限 →
  SIGKILL 整组；回收后核对 GPU 进程列表确认显存释放。
- **缓冲**：event/log/metrics 共用有界 ring buffer（默认 8MB / 5min 量级，
  可配），断网溢出时丢弃最老并记 gap，重连后先发 gap 事件再补传。

测试策略：

- 单元测试：状态机、幂等去重、缓冲溢出、进程组回收（用 sleep/假 python
  脚本）、依赖门（WAITING 流转）、fetch 校验失败路径。
- 集成测试：对真实 server 联调（注册/心跳/断线重连/杀进程恢复），
  作为 server 侧端到端验收的一部分，不单设 mock。

## 4. server 实施拆分

Go module `next-trainer-ee/server`。

```
server/
├── cmd/server/            main + flag（--listen --db --data-root）
├── internal/ws/           ws 接入、握手鉴权（access token → session_token）、
│                          连接生命周期、心跳超时标记离线
├── internal/registry/     节点表（在线状态、GPU 拓扑、description、labels）+
│                          节点↔池多对多元数据（admin 划分）——内存态 + 落库
├── internal/pools/        资源池 CRUD、节点 assign（多池）、
│                          用户↔pool / 数据集↔pool 授权校验
├── internal/queue/        按 pool 分队列：池内 FIFO + GPU 资源匹配起步
├── internal/jobs/         任务生命周期、事件流接收、日志落盘转发
├── internal/datasets/     数据集注册表 + 存储 backend 抽象（local 默认 /
│                          s3 可选）、上传/下载/校验、agent 认证下载端点、
│                          artifact 回传接收
├── internal/store/        repository 接口（Node/Job/Event/Dataset/Pool/User
│                          六组起步）
│   └── sqlite/            首版实现（单文件，WAL 模式）
├── internal/api/          web 用 REST/SSE（节点、任务、数据集、池、日志流）
├── internal/auth/         用户系统：登录会话、argon2id 密码哈希、
│                          RBAC（admin/user 角色骨架）、access token 签发/撤销、
│                          注册窗口 toggle
├── internal/log/          统一 JSONL 日志（与 agent 同 schema），
│                          web 日志视图与对外采集共用同一事件流
└── web/                   Vue 3 + Vite，build 产物 go:embed
```

关键语义：

- **状态收敛在 server**：节点/任务的可展示状态全部由 agent 上报事实推导，
  agent 断线只影响"在线"判定，不产生需要 agent 本地回放的元数据。
- **store provider 抽象**：repository 接口先行，SQLite 实现与接口同 PR 落地，
  接口设计对照 SQL 语义（不发明 KV 式 API），PG/MySQL 后续平移。
- **web 页面**：登录页、节点列表（在线/GPU/池/描述/任务数；节点详情含
  引擎环境列表与"安装引擎"运维操作；**注册窗口 toggle**）、
  任务列表（状态/日志入口）、
  数据集管理（上传/列表/池授权）、admin 用户管理页（含 access token 签发、
  用户↔池授权）、admin 资源池管理页（节点 assign）。
  dev 时 vite proxy 到 server，生产 go:embed。

测试策略：

- 单元测试：queue 匹配、store sqlite 实现（内存库）、ws 握手鉴权、
  池授权校验（越池拒绝）、auth（哈希/会话/RBAC）。
- 集成测试：真实 agent 联调（即 §3 集成测试的另一端）。

## 5. 共享 protocol 模块

- `pkg/protocol/`（或独立 module `next-trainer-ee/protocol`，两端 replace 引入）：
  消息类型常量、frame struct、编解码、protocol_version 校验。
- 协议字段即 `docs/api/protocol-v1.md` 的代码镜像，两端共用避免漂移。
- commit 独立于 agent/server 骨架，便于后续单独打 tag 冻结。

## 6. 分支与 commit 划分

| 顺序 | 分支 | commits |
|---|---|---|
| 0 | `docs/protocol-v1` | 协议附录 + api/protocol-v1.md |
| 1 | `chore/go-bootstrap` | go mod 三件套（agent/server/protocol）、lint/test 基线、工具链回填 AGENTS.md |
| 2 | `feat/server-bootstrap` | ws 接入+握手 / store 接口+sqlite / auth 用户系统 / pools 资源池与授权 / registry+queue / datasets+存储 backend / web 脚手架+embed / 登录+各页面 |
| 3 | `feat/agent-bootstrap` | conn / config+身份文件 / exec+进程树 / fetch 资产拉取 / provision venv+审计 / gpu 抽象+nvml stub / jobs 状态机+lane |
| 4 | `feat/e2e-sd-scripts` | sd-scripts 最小 wrapper、端到端联调、验收记录 |

server 排前（第 2 步先于 agent 主体），保证 agent 联调有真实对端。

## 7. 验收标准（Phase 1+2 出口）

1. 干净机器上 `server` 单 exe 启动，首启引导建 admin；登录后 web 可用，
   未登录访问一律 401 跳登录。
2. admin 在 web 创建用户与 agent access token（可撤销）；
   普通用户可提交任务，无权管理用户/节点 token（RBAC 校验）。
   注册窗口：打开时免 token 可注册，关闭后无 token / 错误 token 均被拒。
3. `agent --server ... --token ... --description "..." --pool vision`
   注册成功，web 节点列表可见描述/池/GPU 拓扑；新池名自动创建且默认
   仅 admin 可用；不带 `--pool` 进 default 池。
4. 池授权生效：未授权用户向 `vision` 池提任务被拒；未授权数据集被
   `vision` 池任务引用被拒。admin 把同一节点 assign 进两个池后，
   两个池的任务均可调度到该节点（按实际空闲 GPU）。
5. web 上传数据集（local backend）→ 提交引用该数据集的 sd-scripts
   单卡任务 → agent 认证下载并校验 sha256 → 拉起 → 日志实时回流 web。
6. SIGTERM 优雅停止；杀 agent 重启后节点恢复在线，任务状态与事实一致。
7. 断网 5 分钟恢复：缓冲内事件补传，溢出部分有 gap 标记。
8. 日志统一抽查：agent 自身日志、训练 stdout、server 日志均为同一 JSONL
   schema（ts/level/source/node_id/job_id/msg），`jobs/<job_id>/stdout.jsonl`
   可被 Filebeat 直接 tail 解析。
9. 资产拉取：`file://` 与 `s3://`（MinIO 兼容端点）各跑通一次，
   sha256 不匹配时任务失败并上报 asset.failed，不启动训练。
   依赖链：引用未下载数据集的任务 → WAITING → maintenance lane 自动下载
   （不占 GPU）→ 就绪自动 RUNNING；下载失败转 FAILED，不悬挂。
10. 运维安装：admin 在 web 给节点安装 sd-scripts 环境 → agent 建 venv +
    冒烟审计 → 回报 ready，节点环境列表可见；重复下发同指纹指令幂等跳过。
11. 计量抽查：任务完成后可从事件表算出该任务在每块 GPU 上的
    起止时间与时长（user/pool/job/node/gpu 五元组齐全）。
12. `go build ./... && go vet ./... && go test ./...` 全绿（两端）。

## 8. 风险

| 风险 | 缓解 |
|---|---|
| 计量数据洞（有界缓冲溢出即丢） | 接受 gap 并标记；Phase 4 对账按 gap 区间处理，私有部署可信环境可接受 |
| nvml cgo 在 aarch64 GB10 编译失败 | go-bootstrap 步骤先验证；失败退回 `nvidia-smi` 解析实现（接口不变） |
| accelerate 多卡子进程组回收不完整 | e2e 用真多卡任务验证（验收 6）；核对该卡进程列表确认显存释放 |
| 日志高频时 JSONL 归一化成瓶颈 | 批量帧 + 有界缓冲；e2e 用真实 sd-scripts 压测落盘/上报/Filebeat tail 三路 |
| session_token 落盘泄漏 | 0600 权限；access token 不持久化 |

## 9. 已关闭的开放问题（2026-10-12 全部拍板）

- ~~mock server~~ → 不做，真实 server 联调（决策 2）
- ~~web 鉴权底线~~ → 完整用户系统提前到 Phase 2（决策 16）
- ~~artifact 回传通路~~ → 复用 server 存储层（决策 11）
- ~~引擎包分发~~ → admin 运维操作 + asset 通路（决策 14）
- ~~agent 自升级~~ → 永不做，运维手动替换（决策 9）
- ~~agent TUI~~ → 暂时不做，日志 + web 视图覆盖排障
- ~~节点时钟校验~~ → 不做，调度只看事实；计量用 server 接收时间 +
  elapsed 相对时长（决策 13）

## 10. 工具链待回填清单（go-bootstrap 时确认）

- [ ] Go 版本与来源（`go version`）
- [ ] web 包管理器（社区仓选型：查 lora-scripts-next/frontend）
- [ ] lint/test 命令写入工作区 AGENTS.md「本机概况」节
