# Server 数据模型（Data Model）

## 问题

store provider 抽象（SQLite 首版，PG/MySQL 预留）需要一份与实现无关的
实体定义：字段、关系、保留策略。接口设计对照 SQL 语义，不发明 KV 式 API。

约定：所有时间戳存 server 侧 UTC（`server_ts`）；agent 上报的相对时长
（`elapsed_s`）另列。ID 一律 server 分配的文本 ULID/UUID。

## 实体

### users

| 字段 | 类型 | 说明 |
|---|---|---|
| id | text PK | |
| username | text unique | 登录名 |
| password_hash | text | argon2id |
| role | text | `admin` / `user`（可扩展枚举） |
| disabled | bool | 禁用即拒绝登录与活跃会话 |
| created_at / updated_at | int | epoch s |

### access_tokens（agent 注册令牌）

| 字段 | 类型 | 说明 |
|---|---|---|
| id | text PK | |
| token_hash | text unique | 只存哈希，明文签发时展示一次 |
| label | text | 用途备注 |
| created_by | text FK users | |
| expires_at | int null | 可空 = 永久 |
| revoked_at | int null | 撤销时间；不影响已注册节点 |

### nodes

| 字段 | 类型 | 说明 |
|---|---|---|
| node_id | text PK | 握手分配 |
| credential_hash | text | 节点凭证哈希（Welcome 下发的那枚） |
| hostname / description | text | description 可被 admin 改 |
| agent_version / protocol_version | text | |
| labels | json | 自由标签 |
| gpus | json | 拓扑快照（型号/显存/UUID，以最近上报事实为准） |
| status | text | `online` / `offline` / `revoked`（派生+显式吊销） |
| first_seen / last_seen | int | |

在线状态是派生值（心跳超时即 offline），不单独存"最后心跳"之外的
元数据；节点环境清单不落库——由 agent 事实扫描上报，内存态展示。

### pools

| 字段 | 类型 | 说明 |
|---|---|---|
| pool_id | text PK | |
| name | text unique | |
| auto_created | bool | agent `--pool` 带出来的新池（默认无授权） |
| created_at | int | |

关联表（多对多）：`node_pools(node_id, pool_id)`、
`user_pools(user_id, pool_id)`、`dataset_pools(dataset_id, pool_id)`。

### datasets

| 字段 | 类型 | 说明 |
|---|---|---|
| dataset_id | text PK | |
| name / version | text | |
| kind | text | `dataset` / `artifact`（产物）/ `engine`（引擎包） |
| backend | text | `local` / `s3` |
| uri | text | backend 内定位（local 相对路径或 s3://…） |
| sha256 / size_bytes | text / int | 上传时计算 |
| created_by / created_at | | artifact 另记 `job_id` 来源 |
| deleted_at | int null | 软删（引用它的历史任务还要对账） |

### jobs

| 字段 | 类型 | 说明 |
|---|---|---|
| job_id | text PK | |
| user_id / pool_id / node_id | text FK | node_id 在派发后回填 |
| engine / engine_version | text | |
| config_toml | text | server 渲染的完整配置（审计依据） |
| assets | json | `[{uri, dest, sha256, size?}]` 清单快照 |
| gpus | json | 分配的 gpu_index 列表 |
| status | text | 见 job-lifecycle 六态 + QUEUED |
| exit_code | int null | |
| created_at / started_at / ended_at | int | started/ended 以 server 收到事件时间为准 |

### events（事件流水，计量与审计的源头）

| 字段 | 类型 | 说明 |
|---|---|---|
| seq | int PK autoincrement | server 侧单调序号（去重依据） |
| node_id / job_id | text | job_id 可空（节点级事件） |
| type | text | `job.*` / `asset.*` / `provision.*` / `node.*` |
| payload | json | 事件体（含 GPU 分配、退出码等） |
| server_ts | int | 收到时间（权威） |
| agent_seq / elapsed_s | int null | agent 序号与相对时长（补传去重、校验） |
| gap | bool | 断网溢出后补传时的洞标记 |

用量流水（哪张卡跑了多久）是本表按 `job.starting/job.finished/...`
与 `jobs.gpus` 的派生查询，不另建表；Phase 4 聚合层直接基于它。

### commands（指令审计）

| 字段 | 类型 | 说明 |
|---|---|---|
| command_id | text PK | 幂等键 |
| node_id / type / payload | | provision / job.start / env.gc / … |
| issued_by | text FK users | 发起人（运维操作留痕） |
| issued_at / acked_at | int | |

## 保留策略

- `events` 与日志文件是最大膨胀源：保留期配置项（默认 90 天事件、
  30 天完整日志），过期归档/清除由 server 后台任务执行；
  用量派生查询在清除前先做月度聚合快照（metering Phase 4 细化）。
- `jobs` / `datasets` 永久保留（软删），用量对账的锚点。
- SQLite 首版开 WAL；备份 = 停写快照 + datasets 目录，运维文档 Phase 6 细化。

## 不采用

- 用量另建流水表（派生物，建了必然对不上）。
- 事件表存 agent 绝对时间戳（跨时区节点合法，server_ts 才是权威）。
