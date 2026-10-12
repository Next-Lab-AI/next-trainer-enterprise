# 引擎进程边界契约（Engine Process Contract）

## 问题

社区版里 Python 后端通过函数调用/模块导入直接驱动训练引擎；
企业版 agent 是 Go，引擎必须收敛为**进程边界**：

> 拉起就是 cmd——配置文件进、结构化日志出、退出码收尾。

vendor/ 下 5 套引擎源码（sd-scripts、ai-toolkit、anima-lora、
diffsynth-studio、musubi-tuner）的"标准化改造"，本质就是把每套引擎
包成满足本契约的可执行单元。

## 契约

### 1. Manifest（每引擎一份，静态描述）

```toml
# vendor/<engine>/engine.toml
name = "sd-scripts"
version = "..."
entry = "train_network.py"        # 或 wrapper
accelerator = "accelerate"         # none | accelerate | torchrun
min_gpus = 1
max_gpus = 8                     # 多卡能力声明
env_spec = "requirements.txt"     # provisioning 依据
capabilities = ["sdxl", "flux", "anima", ...]
```

### 2. 输入：配置文件

- **server 渲染**：任务 TOML 由 server 按引擎模板渲染完整，随 `job.start`
  下发；agent 只做路径白名单校验（禁止逃出 `<agent_root>`），
  不做模板渲染——审计集中在 server，agent 零模板逻辑。
- 引擎不得从环境变量/隐式路径读取任务级配置（可移植性 + 可审计）。

### 3. 输出：结构化 stdout（JSON Lines）

```json
{"type":"progress","step":120,"total":3000,"loss":0.0812,"lr":1e-4,"elapsed_s":95}
{"type":"artifact","kind":"checkpoint","path":"output/last-000120.safetensors"}
{"type":"log","level":"info","msg":"caching latents done"}
```

- 人类可读日志走 stderr 或 `type:"log"` 帧，agent 原样转发。
- 引擎侧加一个薄 wrapper（Python）负责把原有 print/progress 适配成 JSONL；
  **不改训练核心逻辑**，wrapper 是 vendor 改造的主要落点。

### 4. 生命周期

- 优雅停止：agent 发 SIGTERM（Windows: CTRL_BREAK/terminate），
  引擎/wrapper 应在当前 step 边界 checkpoint 后退出（尽力而为）。
- 强杀：SIGKILL 兜底；agent 负责整棵进程树回收（不能残留孤儿显存占用）。
- 退出码：0 成功；非 0 + 最后一行 JSONL `{"type":"error","code":...,"msg":...}`。

### 5. 资源分配（单卡 vs 多卡）

- agent 按 server 下发的 `gpus: [0,1]` 设置 `CUDA_VISIBLE_DEVICES` 与
  accelerator 启动参数（`accelerate launch --num_processes N` 或
  `torchrun --nproc_per_node N`），引擎内部不感知调度。
- 多机训练首版不支持（队列层预留字段）。

### 6. 任务目录布局

```
<workspace>/<job_id>/
├── config.toml          # 渲染后的任务配置（审计依据）
├── stdout.jsonl         # agent 落盘的结构化流
├── output/              # checkpoint / 产物
└── logs/
```

## 附录 A：engine.toml 字段定义

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string | ✅ | 引擎标识（与 vendor 目录名一致） |
| version | string | ✅ | 快照版本/pin 说明 |
| entry | string | ✅ | 入口脚本（相对引擎根），通常是 wrapper |
| accelerator | string | ✅ | `none` / `accelerate` / `torchrun` |
| min_gpus / max_gpus | int | ✅ | 卡数能力声明，调度匹配依据 |
| env_spec | string | ✅ | requirements/constraints 文件路径（provision 依据） |
| capabilities | array[string] | ❌ | 能力标签（sdxl/flux/anima/video/tagging…），web 筛选用 |
| kind | string | ✅ | `train` / `inference`（打标/翻译属 inference，同契约） |

## 附录 B：JSONL 输出帧定义

每帧一行 JSON，stdout 承载；`type` 枚举：

| type | 字段 | 说明 |
|---|---|---|
| `progress` | step, total, loss?, lr?, elapsed_s | 训练进度；inference 引擎用 processed/total |
| `artifact` | kind, path | 产物登记（checkpoint / tags / …），path 相对任务目录 |
| `log` | level, msg | 人类可读日志（stderr 也由 agent 归一化为此类） |
| `error` | code, msg | 致命错误，必须是退出前最后一帧 |

未知帧类型 agent 原样转发为 `log`（前向兼容）。

## 各引擎改造量预估

| 引擎 | 入口形态 | 预计工作 |
|---|---|---|
| sd-scripts | 已有 toml config + accelerate | 最小：wrapper 做 JSONL 进度 |
| musubi-tuner | 类似 sd-scripts | 小 |
| ai-toolkit | 自有 job 体系 | 中：要掰开它的 job 管理 |
| anima-lora | setup.py 安装形态 | 中 |
| diffsynth-studio | 脚本集合，入口分散 | 大：先圈定支持的功能子集 |

## 不采用

- 引擎以 library 形式被 Go 直接嵌入（cgo/Python 嵌入）——部署噩梦。
- 每引擎自定义一套输出格式——agent 侧解析器会爆炸。
