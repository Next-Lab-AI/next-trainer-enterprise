# Next Trainer Enterprise

**Next Trainer Enterprise**（Next Trainer 企业版）— 面向多机多卡与专业调度的训练工作台。

- Organization: [Next-Lab-AI](https://github.com/Next-Lab-AI)
- Related community edition: [wochenlong/lora-scripts-next](https://github.com/wochenlong/lora-scripts-next)（Next Trainer）

> Private repository. Internal development only for now.

## 架构

```
next-trainer-enterprise/
├── server/        # Go 主控端：用户系统、计费、任务调度、展示层
│   └── web/       # Vue 前端，构建产物 embed 进 server 二进制
├── agent/         # Go 被控端（算力节点）：单 exe，最多带简单 TUI
└── vendor/        # 训练引擎源码（patched 验证版快照，见下表）
```

### 部署模型（agent-pull）

- 每台算力机器运行一个 `agent` exe，启动时传入主控地址 + access token，
  **由节点侧主动向主控建立长连接完成注册**（主控可放在公网/上层网络，
  节点无需开放入站端口）。
- 注册后环境安装、任务下发均由主控决定；agent 负责拉起/监控/回收
  本机的训练进程（Python 进程隔离，venv 照旧，按引擎/任务独立环境）。
- 计量在 agent 侧完成（nvml 采样 + 任务事件流），主控侧汇总，
  计价策略可替换，支持企业内部门间结算（showback/chargeback）。

### 交付形态

客户可选：仅引擎源码（自行改造）、仅被控端 agent、或整套（server + agent + 引擎）。

## Vendored 引擎快照

`vendor/` 下为**已 patch、已验证的本土可用源码**，直接交付，
不做"拉上游 + 运行时打 patch"。标准化/瘦身改造后续另行处理。

| 目录 | 上游 | Pin（快照基准 commit） | 说明 |
|---|---|---|---|
| `vendor/sd-scripts` | kohya-ss/sd-scripts | （深度 patched 内置树，无单一上游 pin） | 主训练引擎，SD1.5/SDXL/SD3/Flux/Anima 等 |
| `vendor/ai-toolkit` | ostris/ai-toolkit | `5497a001cb8752c665f93907a0393fc612116fd5` | 本机安装快照基准（清单默认 pin 为 `ecee894e`） |
| `vendor/anima-lora` | sorryhyun/anima_lora | `b43928b5e4b82b907bfca1a322383a33088d0bdd` | Anima LoRA 引擎 |
| `vendor/diffsynth-studio` | modelscope/DiffSynth-Studio | `7686e54d41d25c0e8ed5f1318acc23b6bb832654` | 视频/图像训练套件 |
| `vendor/musubi-tuner` | musubi-tuner 上游 | `e0cbd8f3dfe38365b10f8bc790b980f8894e8ba1` | 视频 LoRA 引擎 |
| `vendor/next-integrated` | — | — | Next 集成引擎（自研，待开发） |

快照导出约定：从 lora-scripts-next 的 `extensions/<engine>/source`
（或 `vendor/sd-scripts` 的 tracked 树）拷贝，排除 `__pycache__`、`*.egg-info`、
`build/`、`jobs/`、`.git` 等运行期产物。
