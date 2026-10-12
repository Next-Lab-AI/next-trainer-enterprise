# Server ↔ Agent 协议

## 问题

主控与被控端之间的协议是整个系统的第一公民：**客户一旦部署就改不动**。
必须在 agent 动工前定版骨架（连接、注册、消息分类、幂等规则），
否则后期每个字段变更都是兼容性问题。

## 连接层

- 节点侧主动建立长连接：`agent --server wss://ctrl.example.com --token <access-token>`。
- 选型：**WebSocket + JSON 消息**起步（简单、跨语言调试方便、代理/网闸穿透性好）；
  预留升级到 gRPC stream 的可能（消息层 schema 与传输解耦）。
- 心跳：agent → server，默认 15s；连续 N 次未达 server 标记节点离线。
- 断线重连：指数退避；重连后重新走注册握手（携带 node_id 恢复会话）。

## 注册握手

```
agent → server: Hello { node_id?, agent_version, protocol_version,
                        hostname, description, pool, gpus: [...],
                        cpu, mem, labels }
server → agent: Welcome { node_id, session_token, server_time,
                         heartbeat_interval, config }
```

- 注册有两种模式：
  1. **token 模式（默认）**：管理员在主控 web 创建 access token
     （可撤销、可设有效期），agent 凭 `--token` 注册——白名单制。
  2. **注册窗口模式**：admin 在 web 打开注册窗口（toggle），窗口期内
     Hello 免 token，方便云算力与小规模场景批量接入；用完由 admin
     手动关掉，关闭后恢复仅 token。
- **免 token 仅限首次握手那一瞬间**：无论哪种模式，注册成功即由 server
  为该节点生成凭证并随 Welcome 下发，agent 存入身份文件；此后该节点的
  一切连接（含窗口关闭后）都凭此凭证认证。顶替已注册节点 = 需要拿到
  节点本地的身份文件，伪造难度与 token 模式注册的节点一致。
- 握手成功后下发 **session_token**（运行期凭证），与注册 token 分离；
  session_token 支持轮换，access token 泄漏不波及已注册节点。
- `node_id` 由 server 分配，agent 只持久化单个身份文件
  （node_id + session_token，0600），重启后复用。
- `description` / `pool` 是节点归属的**初始建议**：新池名 server 自动创建，
  缺省进 `default` 池；之后节点↔池归属以 server 侧元数据为准
  （admin 可改划，多对多），agent 上报值不再影响已有归属。
- 不校验节点时钟：跨时区部署（中/美 GPU 共存）是合法场景，
  调度只依据上报事实。

## 消息分类

| 方向 | 类型 | 说明 |
|---|---|---|
| s→a | `provision` | 环境安装指令（admin 运维操作触发：引擎、版本、依赖约束） |
| s→a | `env.gc` | 环境回收指令（零引用且超龄的 venv） |
| s→a | `job.start` | 下发任务（engine、渲染后 TOML 配置、`assets[]` 资产清单、资源需求：GPU 数/型号） |
| s→a | `job.stop` / `job.kill` | 优雅停止 / 强杀；`job.stop` 带 `preempt` 标记时为抢占（admin 手动触发，语义见 job-lifecycle 设计） |
| a→s | `event` | 状态事件流：任务等待（依赖未就绪）/开始/进度/结束/失败/抢占；携带 GPU 分配与生命周期信息，用量流水由此派生 |
| a→s | `metrics` | GPU 逐卡采样批量上报（gpu_index 维度，字段厂商中性，见 metering 设计） |
| a→s | `log` | 训练日志流（帧格式 = 统一 JSONL 日志 schema，与节点落盘文件同一份格式，Filebeat/Logstash 可直接采集） |
| a→s | `asset.ready` / `asset.failed` | 资产拉取结果（含 sha256 实测值） |
| 双向 | `file` | 仅限小文件（配置下发、artifact 元数据）；大文件不走协议 |

## 幂等与可靠性（硬规则）

- 所有 s→a 指令携带唯一 `command_id`；agent 重启或重发后**重复执行不得破坏状态**
  （已装好的环境跳过、已跑的任务不重复拉起）。
- **agent 无状态**：不引入本地数据库，计算节点不保存可能过期的元数据，
  一切以实时观测事实为准（进程存活、GPU 占用、磁盘上的 envs//datasets/）。
  event/metrics/log 断网期间进**有界内存缓冲**，溢出丢弃最老并记 gap，
  重连后先发 gap 事件再补传；server 按 `command_id` / 事件序号去重。
- 资产（数据集/底座模型/引擎包）以 `{uri, dest, sha256}` 清单随指令下发，
  agent 主动拉取：scheme 白名单 `file://`、`s3://`（MinIO 兼容）、
  `https://`（仅 server 自身数据集分发端点，session_token 鉴权）；
  S3 凭证配置在节点本地，不经 server 流转。
- 消息 schema 带 `protocol_version`，首版即预留字段扩展规则（新增字段 optional，
  不删不改已有字段语义）。**字段级定义与版本兼容矩阵见
  `docs/api/protocol-v1.md`**（协议的唯一权威，与 `pkg/protocol` 对齐维护）。

## 安全

- 传输：wss（TLS）必选；私有部署允许自签，agent 侧支持 pinned CA / fingerprint。
- 鉴权：注册用 access token，运行期用 session_token；消息层不做用户级鉴权
  （用户概念只存在于 server）。
- 不采用：agent 暴露 HTTP 端口被动接收指令（违背 agent-pull 初衷）。

## 待定（动工前需拍板）

- 单连接多路复用 vs 日志单独通道（首版单连接，日志批量帧）。
- ~~大文件走哪条路~~ → 已定：不经协议，agent 按 URI 主动拉取
  （file/s3/https-server，凭证节点本地），协议只传 URI + sha256。
