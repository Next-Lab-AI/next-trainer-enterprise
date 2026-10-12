# Next Trainer Enterprise（企业版）

[English](README.md) | **中文**

面向多机多卡的企业级训练平台：算力池化、队列调度、逐卡计量、
数据集集中管理——为在私有基础设施上运行的算法工程团队而设计。

- 组织：[Next-Lab-AI](https://github.com/Next-Lab-AI)
- 社区版：[wochenlong/lora-scripts-next](https://github.com/wochenlong/lora-scripts-next)（Next Trainer）

> 私有仓库。

## 总览

企业版将控制面与训练面分离：

- **server**（Go 单二进制，内嵌 Vue 3 前端）——用户系统与 RBAC、
  资源池、任务队列、数据集管理、计量与展示层。
- **agent**（Go 单二进制，每台算力节点一个）——通过主动外发的长连接
  注册，接收指令，管理训练进程的完整生命周期。
- **vendor/**——pinned、patched、验证过的引擎源码快照。引擎以源码交付、
  以进程运行：配置文件进、结构化 JSONL 出、退出码收尾。

```
┌─────────────────────────────────────────────┐
│ server（Go 主控，单 exe，embed Vue 前端）      │
│  用户/RBAC · 资源池 · 队列 · 数据集 · 计量      │
└──────────────▲──────────────────────────────┘
               │ 长连接（节点主动发起，agent-pull）
     ┌─────────┴─────────┐─────────────┐
     │ agent（算力节点）   │  agent      │  agent  ×N
     └─────────┬─────────┘
               │ 进程拉起
        ┌──────┴───────┐
        │ vendor 引擎    │  Python venv，按引擎隔离
        └──────────────┘
```

## 核心特性

- **agent-pull 部署**：算力节点只需出网，无需开放入站端口、无需改动
  客户防火墙。注册即 `agent --server <addr> --token <access-token>`。
- **零入站、无自升级**：agent 不监听任何端口，只执行 server 分配的任务；
  升级由运维手动替换二进制，二进制内不存在远程自举通路。
- **资源池**：节点↔池多对多，归属是 server 侧元数据，由 admin 划分；
  用户与数据集按池授权；队列按池独立 FIFO，只在节点有实际空闲 GPU 时
  下发任务。
- **完整用户系统**：登录、用户管理、RBAC 角色、可撤销的 agent access
  token，均在 web 界面管理。
- **数据集集中管理**：server 持有数据集注册表，存储 backend 可插拔
  （默认本地磁盘，可选 S3/MinIO）；agent 按 URI 拉取并做 SHA-256 校验
  （`file://`、`s3://` 或经 server 认证的 `https://`）；训练产物经同一
  存储层回传。
- **逐卡计量**：每条用量记录携带 `(user, pool, job, node, gpu_index,
  gpu_model, start, end)`——哪张卡跑了多久是一等数据；计价器是可替换
  策略层，支持内部 showback/chargeback；以 server 接收时间为权威，
  跨时区节点无影响。
- **日志统一**：全链路一份 JSONL schema（ts/level/source/node_id/job_id/
  msg），协议 log 帧与节点落盘文件逐字节一致，Filebeat/Logstash 可直接
  tail 采集。
- **计算/维护双 lane**：下载、引擎安装、环境回收走 maintenance lane
  （不占 GPU），训练走 compute lane；任务声明资产依赖，数据集未就绪时
  进入 WAITING，依赖齐了自动开跑。
- **厂商无关 GPU 抽象**：指标与拓扑走接口（Sampler/Topology/
  ProcessLister），NVML 是其中一个实现，预留 ROCm 及其他加速卡。

## 运行

主控（server）：

```sh
server --listen :8080 --db data/server.db --data-root data/
```

算力节点（agent）：

```sh
agent --server wss://ctrl.example.com --token <access-token> \
      --description "DGX Spark 开发机" --pool vision
```

`--pool` 只是初始归属建议；admin 可在 web 上重新划分节点所属池
（一个节点可属于多个池）。引擎环境由 admin 在节点详情页发起安装
（"安装引擎"运维操作），按依赖指纹命名、可复用的 Python venv。

## 仓库布局

```
server/        Go 主控
server/web/    Vue 3 前端（embed 进 server 二进制）
agent/         Go 被控端（算力节点）
vendor/        引擎源码快照（见下表）
docs/design/   设计文档
docs/tasks/    任务书与验收报告
docs/api/      协议规范（消息 schema）
```

## Vendored 引擎快照

`vendor/` 下为已 patch、已验证的本土可用源码，直接交付，
不做"拉上游 + 运行时打 patch"。

| 目录 | 上游 | Pin（快照基准 commit） | 说明 |
|---|---|---|---|
| `vendor/sd-scripts` | kohya-ss/sd-scripts | （深度 patched 内置树，无单一上游 pin） | 主训练引擎：SD1.5/SDXL/SD3/Flux/Anima |
| `vendor/ai-toolkit` | ostris/ai-toolkit | `5497a001cb8752c665f93907a0393fc612116fd5` | 安装快照基准（清单默认 pin `ecee894e`） |
| `vendor/anima-lora` | sorryhyun/anima_lora | `b43928b5e4b82b907bfca1a322383a33088d0bdd` | Anima LoRA 引擎 |
| `vendor/diffsynth-studio` | modelscope/DiffSynth-Studio | `7686e54d41d25c0e8ed5f1318acc23b6bb832654` | 视频/图像训练套件 |
| `vendor/musubi-tuner` | musubi-tuner | `e0cbd8f3dfe38365b10f8bc790b980f8894e8ba1` | 视频 LoRA 引擎 |
| `vendor/tagger` | lora-scripts-next `mikazuki/tagger` | `ab4acd67dc06e49c5e7b32d18428173a544a8140` | ONNX 图像打标（WD14/CL interrogator） |
| `vendor/tag-translation` | lora-scripts-next `mikazuki/tag_translation` | `ab4acd67dc06e49c5e7b32d18428173a544a8140` | llama.cpp 标签翻译（Qwen GGUF）+ 中文词典 |
| `vendor/next-integrated` | — | — | Next 集成引擎（自研） |

导出约定：从社区仓 `extensions/<engine>/source`（或 `vendor/sd-scripts`
的 tracked 树）拷贝，排除 `__pycache__`、`*.egg-info`、`build/`、`jobs/`、
`.git` 等运行期产物。

## 交付形态

客户可选三种交付：

1. **仅引擎源码**——vendor 源码树，客户自行集成改造。
2. **仅 agent**——接入既有 server 的算力节点二进制。
3. **全家桶**——server + agent + 引擎，含全离线介质选项
   （源码 + wheelhouse + 底座模型），支持网闸/气隙部署。

## 文档

- 设计文档：[docs/design/](docs/design/)——架构、server↔agent 协议、
  引擎进程契约、计量计费、环境安装
- 任务书与验收：[docs/tasks/](docs/tasks/)
- 协议规范：[docs/api/](docs/api/)
