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
                        hostname, gpus: [...], cpu, mem, labels }
server → agent: Welcome { node_id, session_token, server_time,
                         heartbeat_interval, config }
```

- 首次注册用 access token（管理员在主控创建，可撤销、可设有效期）。
- 握手成功后下发 **session_token**（运行期凭证），与注册 token 分离；
  session_token 支持轮换，access token 泄漏不波及已注册节点。
- `node_id` 由 server 分配，agent 持久化到本地状态文件，重启后复用。

## 消息分类

| 方向 | 类型 | 说明 |
|---|---|---|
| s→a | `provision` | 环境安装指令（引擎、版本、依赖约束） |
| s→a | `job.start` | 下发任务（engine、config 文件、资源需求：GPU 数/型号） |
| s→a | `job.stop` / `job.kill` | 优雅停止 / 强杀 |
| a→s | `event` | 状态事件流：任务开始/进度/结束/失败/抢占 |
| a→s | `metrics` | nvml 采样批量上报（见 metering 设计） |
| a→s | `log` | 训练日志流（结构化转发引擎 stdout） |
| 双向 | `file` | 配置下发 / artifact 回传的分块传输 |

## 幂等与可靠性（硬规则）

- 所有 s→a 指令携带唯一 `command_id`；agent 重启或重发后**重复执行不得破坏状态**
  （已装好的环境跳过、已跑的任务不重复拉起）。
- agent 本地维护状态机 + WAL：event/metrics 断网期间落盘，重连后补传，
  server 按 `command_id` / 事件序号去重。
- 消息 schema 带 `protocol_version`，首版即预留字段扩展规则（新增字段 optional，
  不删不改已有字段语义）。

## 安全

- 传输：wss（TLS）必选；私有部署允许自签，agent 侧支持 pinned CA / fingerprint。
- 鉴权：注册用 access token，运行期用 session_token；消息层不做用户级鉴权
  （用户概念只存在于 server）。
- 不采用：agent 暴露 HTTP 端口被动接收指令（违背 agent-pull 初衷）。

## 待定（动工前需拍板）

- 单连接多路复用 vs 日志单独通道（首版单连接，日志批量帧）。
- 大文件（模型/数据集）走哪条路：建议首版不经协议，由 agent 从
  客户内部存储（对象存储/NAS）拉取，协议只传 URI + 校验。
