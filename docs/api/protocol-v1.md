# Server ↔ Agent 协议 v1（protocol-v1）

> 本文是协议的唯一字段级权威定义，与 `pkg/protocol` 实现对齐维护。
> 语义与设计理由见 `docs/design/server-agent-protocol.md`；
> 任务状态语义见 `docs/design/job-lifecycle.md`。

- protocol_version: `1`
- 传输：WebSocket（wss），消息为 JSON 文本帧
- 编码：UTF-8；时间戳一律 epoch 秒（int）；`server_ts` 由 server 落库时补

## 1. 信封（所有消息共用）

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| type | string | ✅ | 消息类型，见下表 |
| protocol_version | int | ✅ | 首版恒为 1 |
| command_id | string | 条件 | s→a 指令必填（幂等键）；a→s 对指令的应答回显 |
| seq | int | ✅(a→s) | agent 侧单调递增序号，断线重连续传；server 按 (node_id, seq) 去重 |
| payload | object | ✅ | 消息体，逐类型定义见下 |

消息类型一览：`hello` / `welcome` / `heartbeat` / `provision` / `env.gc` /
`job.start` / `job.stop` / `job.kill` / `event` / `metrics` / `log` /
`asset.ready` / `asset.failed` / `file`。

## 2. 注册与连接

### hello（a→s，连接建立后第一条）

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| node_id | string | ❌ | 已有身份文件时携带，用于会话恢复 |
| credential | string | 条件 | 已注册节点重连时必填（身份文件里的凭证） |
| access_token | string | 条件 | 首次注册：token 模式必填；注册窗口期可空 |
| agent_version | string | ✅ | |
| hostname | string | ✅ | |
| description | string | ❌ | 初始建议，server 元数据为准 |
| pool | string | ❌ | 初始归属建议；缺省 `default`；新池自动创建 |
| gpus | array | ✅ | `[{index, uuid, model, vendor, mem_total_mb}]`，vendor 中性 |
| cpu / mem_mb | string / int | ✅ | 概要信息 |
| labels | object | ❌ | 自由键值对 |

### welcome（s→a，握手应答）

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| node_id | string | ✅ | server 分配/确认 |
| credential | string | 条件 | 首次注册下发；agent 写身份文件（0600），之后重连凭它 |
| session_token | string | ✅ | 运行期凭证，可轮换（server 任何消息可带新值） |
| heartbeat_interval_s | int | ✅ | |
| config | object | ❌ | 缓冲上限等运行参数覆盖 |

握手失败：server 直接关闭连接，关闭帧带 reason
（`bad_token` / `revoked` / `window_closed` / `version_incompatible`）。

### heartbeat（a→s）

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| jobs | array | ✅ | `[{job_id, status}]` 运行中任务事实 |
| free_gpus | array | ✅ | 当前空闲 gpu_index 列表（事实，调度依据） |

## 3. 指令（s→a，均含 command_id）

### provision

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| engine | string | ✅ | |
| version | string | ✅ | |
| package | object | ✅ | 引擎包资产 `{uri, sha256, size?}` |
| constraints | string | ❌ | 依赖约束（pip constraints 内容或引用） |

应答：`event{type:"provision.ready"|"provision.broken"}`，broken 附审计日志
`file` 帧或日志段引用。同指纹已存在直接 ready（幂等）。

### env.gc

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| older_than_days | int | ❌ | 默认 30 |

回收零引用且超龄的 venv，应答 `event{type:"env.gc.done", removed:[...]}`。

### job.start

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| job_id | string | ✅ | |
| engine / engine_version | string | ✅ | |
| config_toml | string | ✅ | server 渲染的完整任务配置 |
| assets | array | ❌ | `[{uri, dest, sha256, size?}]`，缺省无外部资产 |
| gpus | array | ✅ | `[0,1]` 物理 gpu_index；agent 设 CUDA_VISIBLE_DEVICES |
| lane | string | ✅ | `compute` / `maintenance` |

### job.stop / job.kill

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| job_id | string | ✅ | |
| preempt | bool | ❌(stop) | true = admin 手动抢占，计量口径不变 |
| grace_s | int | ❌(stop) | SIGTERM 宽限，默认 60 |

## 4. 上报（a→s）

### event

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| event_type | string | ✅ | 见下 |
| job_id | string | ❌ | 任务事件必填 |
| elapsed_s | int | ❌ | 相对时长（计量校验用，不依赖节点时钟） |
| data | object | ❌ | 事件附加（missing[]、exit_code、reason 等） |

event_type 枚举：
`job.created` / `job.waiting{reason,missing[]}` / `job.starting{gpus[]}` /
`job.progress{step,total,loss?,lr?}` / `job.finished{exit_code:0}` /
`job.failed{code,msg}` / `job.terminated{reason:"stop"|"kill"|"preempted"}` /
`asset.*`（见 §5）/ `provision.*` / `env.gc.done` / `node.envs`（环境清单事实上报）。

断网补传：重连后先发 `event{event_type:"gap", data:{dropped_seq_from,
dropped_seq_to}}`，再按 seq 顺序补传缓冲内容。

### metrics

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| samples | array | ✅ | `[{gpu_index, ts_offset_s, util, mem_used_mb, power_w?, temp_c?}]`，逐卡 |

### log

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| lines | array | ✅ | JSONL 日志行数组，每行 schema 见附录 A——**与节点落盘文件逐字节一致** |

### file（双向，仅小文件）

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| path | string | ✅ | 相对 job/节点目录 |
| chunk / offset / last | string(b64) / int / bool | ✅ | 分块传输，单文件上限 1MB |

## 5. 资产

`asset.ready` / `asset.failed`（a→s）：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| uri | string | ✅ | |
| job_id | string | ❌ | 由任务依赖触发时携带 |
| sha256_actual | string | ✅(ready) | 实测值 |
| error | string | ✅(failed) | |

URI scheme 白名单：`file://`、`s3://`、`https://`（仅 server 自身地址）。
凭证（S3 key 等）只存在于节点本地配置，不出现在协议消息里。

## 6. 版本演进与兼容规则

- `protocol_version` 整数单调递增；破坏性变更（删字段/改语义/改类型）
  才升版本。
- 新增字段一律 optional，接收方必须忽略未知字段。
- 握手时双方交换版本：不一致且无法协商 → `version_incompatible`。
- 兼容矩阵：server v1 接受 agent v1；agent 不得高于 server 主版本。
  agent 随交付包升级（运维手动），无自动协商升级。
- vendor 引擎快照升级走"从社区仓重新导出 + 验证"流程，
  commit message 记录来源与上游 commit，与协议版本正交。

## 附录 A：统一日志 JSONL schema

协议 `log` 帧内每行、节点落盘 `stdout.jsonl` / `agent.jsonl`、server 日志
文件，三者共用一份格式：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| ts | string | ✅ | RFC3339 UTC（展示用；计量不靠它） |
| level | string | ✅ | `debug` / `info` / `warn` / `error` |
| source | string | ✅ | `agent` / `engine` / `server` / `provision` / `fetch` |
| node_id | string | ✅ | |
| job_id | string | ❌ | 任务相关行必填 |
| msg | string | ✅ | 单行文本（多行堆栈拆多行） |
| extra | object | ❌ | 结构化附加（step/loss 等也可走 event） |

敏感信息规则：匹配 TOKEN|KEY|SECRET|PASSWORD 的环境变量值不进入日志；
路径一律以 `<agent_root>` 相对路径书写。
