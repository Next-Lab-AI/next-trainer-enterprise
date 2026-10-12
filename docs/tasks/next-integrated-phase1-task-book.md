# next-integrated Phase 1 任务书（骨架 + SDXL 打样）

## 计划元数据

- Plan ID: EE-NEXTINT-P1-20261012
- Version: v1
- Last updated: 2026-10-12 Asia/Shanghai
- Canonical progress file: `docs/tasks/next-integrated-phase1-task-book.md`
- Current branch: `feat/next-integrated-bootstrap`
- Current active step: Step 4 已完成，Step 5 待开工
- Design docs:
  - `docs/design/next-integrated.md`（总设计，钉版基线在其中）
  - `docs/design/engine-process-contract.md`（契约权威定义）
- 调研依据：工作区外层 `docs/research-2026-10-12-next-integrated.md`

## 目标

以远古 sd-scripts 源码为种子起 next-integrated 骨架，移植 automagic +
LoRA 网络，**SDXL LoRA 单卡 smoke 跑通**——验证"统一核心库 + adapter +
契约原生输出"三件事，给后续逐模型吸收打样。

## 范围与约束

- In scope:
  - `vendor/next-integrated/` 骨架：pyproject（钉版基线）、engine.toml、
    engine.d hooks、统一入口 train.py（契约原生 JSONL，零 wrapper）；
  - 核心库第一期切片：config（TOML 直解）、contract（JSONL/SIGTERM/退出码）、
    network（LoRA）、optimizers 注册表 + automagic 移植、checkpoint
    （diffusers 风格 safetensors，无 kohya 兼容）；
  - SDXL adapter（加载/前向/loss 三件套）；
  - 钉版验证项实测（见下）。
- Out of scope（本期不做）:
  - SDXL 以外的模型 adapter（Anima 是下一个，见设计文档吸收顺序）；
  - full-ft / ControlNet / TI / 多卡；
  - dataset/bucketing 的完整拆解（本期借用种子代码最小可用集，
    完整模块化随第二个模型吸收时做）；
  - 老引擎 deprecation 标记（第一个非种子模型吸收完成才开始）。
- Constraints:
  - 钉版基线：Python 3.13 / torch 2.12.x+cu132 / transformers 5.5.x
    （diffusers 本期 smoke 时定版并回填设计文档）；
  - 不改训练核心算法逻辑，只做模块化与收敛；
  - anima-lora 不进依赖树、不作钉版依据；
  - 推送前过 `go build/vet`（如涉及 Go 侧）与 Python 侧 smoke。

## 执行阶段

### Step 1: 骨架与契约 ✅（2026-10-12 完成）

- Outputs: pyproject（依赖组参考 anima-lora 的多平台写法但版本自定）、
  engine.toml（capabilities 先写 `["sdxl"]`）、engine.d/、train.py、
  `core/contract` + `core/config`。
- Validation: agent 按引擎契约拉起 `train.py`，假训练循环的
  progress/log/artifact/error 帧完整到达 server；SIGTERM 有 checkpoint。
- 完成情况：
  - 远古种子已导入 vendor/next-integrated（kohya upstream @206adb6，
    143d5ac，排除 .venv/train_data 等本机产物）；
  - engine.toml + engine.d/linux/nvidia/{install,audit,preflight}.sh，
    是首个按契约落地的引擎（e0f454f）；
  - 冒烟回路本机直跑验证通过：JSONL 帧格式正确，SIGTERM 后
    checkpoint 落盘、exit 0；agent 端到端联调待排期。

### Step 2: automagic + LoRA ✅（2026-10-12 完成）

- Outputs: `optimizers/`（automagic 1/2/3 + optimizer_utils）、
  `core/network` LoRA、`core/checkpoint`。
- Validation: quanto 8bit 在 torch 2.12 下实测通过（不通过则 automagic
  先走 32bit 路径并记录）；LoRA save/load roundtrip 与种子实现一致。
- 完成情况：
  - automagic 1/2/3 + optimizer_utils 移植自 vendor/ai-toolkit（cd59ede），
    quanto 改可选导入；`core/optimizers` 注册表落地；CPU 冒烟全过；
  - **quanto 0.2.7 在 torch 2.12.1+cu132 实测通过**：int8 量化/反量化、
    QBytesTensor 写回、automagic v1 直接吃 quanto 参数均正常，
    无需退 32bit（钉版验证清单关闭一项）；
  - core/network LoRA 移植（f8b48e1）：LoRAModule 与种子前向 bitwise
    一致；core/checkpoint 用 diffusers/PEFT 命名（lora_A/lora_B），
    alpha/rank 缩放烘进 lora_B（diffusers 原生加载不读 alpha），
    无 kohya 兼容；roundtrip 测试 4 项全过；
  - 环境修正：flash-attn 预编译 wheel 移出 pyproject（GitHub 直连 URL
    会阻塞 uv lock），改由 engine.d/linux/nvidia/install.sh 按架构可选
    安装（已并入骨架 commit 54863f5）；vendor venv 已建
    （torch 2.12.1+cu132 / py3.13 / transformers 5.5.4 / diffusers 0.35.2）。
- 本机测试资产：工作区外层 `docs/test-assets-2026-10-12.md`
  （ComfyUI 模型库 + lora-scripts-next-test 数据集/历史任务）。

### Step 3: SDXL adapter + smoke ✅（2026-10-12 完成，loss 对照后补）

- Outputs: `adapters/sdxl/` 三件套；diffusers 版本定版回填设计文档。
- Validation: 单卡 SDXL LoRA smoke 跑通，loss 曲线与 vendor/sd-scripts
  同配置对照无异常；产出 checkpoint 可被 diffusers 直接加载。
- 完成情况：
  - `adapters/sdxl/`：load（种子 library 加载 + transformers 5.5 shim）/
    forward（eps 预测 + DDPM scaled_linear + VAE 0.13025，与种子主回路
    逐步对应）；`core/dataset` 最小图片+caption 数据集（中心裁剪，完整
    bucketing 拆解仍随第二个模型做）；train.py 接入真实训练回路
    （契约帧原生输出）；
  - smoke 实测（GB10，tIllunai3_v8 + 6 图数据集，bf16，dim=8，adamw
    1e-4，20 步）：loss 0.25→0.03 正常下降区间，同种子重跑逐位一致；
  - **checkpoint diffusers 直接加载验证通过**：diffusers 0.35.2 + peft，
    722 层全部注入 UNet2DConditionModel（种子 input_blocks 路径经
    kohya→legacy→PEFT 官方转换链映射；proj_in/out 的 Linear↔Conv1x1
    维度转换）；
  - SIGTERM 中途停止：checkpoint 落盘 + exit 0（真实训练回路）；
  - diffusers 定版 0.35.x（0.35.2），设计文档已回填；
  - 坑位记录：transformers 5.5 的 _LazyModule 会替换
    sys.modules["transformers"]，shim 别名必须在首次符号解析后打在
    新对象上（见 adapters/sdxl/_compat.py）；HF 在线探测在网络不可达时
    挂数分钟，tokenizer 改本地缓存优先。
- 遗留：loss 曲线与 vendor/sd-scripts 同配置对照**另起任务排期**
  （vendor 无现成环境；且数据集预处理不同，对照只能看趋势）。

### Step 4: SD1.5 adapter ✅（2026-10-12 完成）

- Outputs: `adapters/sd15/`（与 SDXL 同 pipeline 收敛，顺手做）。
- Validation: 单卡 SD1.5 LoRA smoke 跑通。
- 说明：Anima-DiT 是当前口径第三个模型，但吸收涉及 transformers 5.5
  兼容排查，独立排期在本期之后（见设计文档吸收顺序）。
- 完成情况：
  - `adapters/sd15/` 落地，接口与 sdxl 一致（Bundle/forward 同签名，
    Bundle.text_encoders 统一为 list）；train.py 训练回路收敛为
    模型无关 `run_lora(section, adapter)`，dispatch [sdxl]/[sd15]；
  - 数学与种子 train_network.py SD1.5 主回路逐步对应（eps 预测 /
    DDPM scaled_linear / VAE 0.18215 / padding 到 max_length 后
    text_encoder(input_ids)[0]）；
  - 基座模型从 ModelScope 拉取（AI-ModelScope/stable-diffusion-v1-5
    v1-5-pruned-emaonly.safetensors 4.27GB，直连可达；HF 不通时的
    备用源）；
  - smoke 实测（GB10，SD1.5 + 6 图数据集，bf16，dim=8，adamw 1e-4，
    512px，20 步）：loss 0.56→0.02 正常，0.43s/step；checkpoint
    384 keys 经 diffusers UNet2DConditionModel + PEFT 全量注入验证；
  - 顺手修两个确定性坑：① dataset flip 用全局 RNG，shuffle 下
    flip 决策随 batch 顺序漂移 → 改按样本 idx 独立 Generator；
    ② SDPA mem_efficient 后端 backward 走 atomicAdd 不可复现 →
    SD1.5 关 SDPA 走原生 bmm attention。修后同种子两次重跑
    loss 逐位一致。

## 钉版验证清单（本期顺带关闭）

- [ ] sd-scripts `anima_train*` 在 transformers 5.5 下的 API 兼容排查
      （T5/LLM tokenizer 加载层，为 Step 后的 Anima 吸收铺路）
- [x] ~~optimum.quanto 在 torch 2.12 下的实测（Step 2）~~ ✅ 通过（0.2.7 +
      torch 2.12.1+cu132，2026-10-12）
- [ ] torch 2.12/cu132 的 Windows wheel 覆盖确认（ToB 交付前提）

## 风险

| 风险 | 缓解 |
|---|---|
| transformers 4→5 升级在种子代码里炸点比预想多 | smoke 先行暴露；炸点集中在 TE/tokenizer 加载，不碰训练核心 |
| quanto 不兼容 | automagic 退 32bit，核心不阻塞 |
| GB10 GPU 被训推服务占用，smoke 排不上 | 与用户协商窗口；静态开发与单测不占 GPU |

## 验收

- agent → server 全链路契约帧正确；
- SDXL LoRA smoke 指标正常，checkpoint diffusers 可加载；
- 设计文档 diffusers 定版回填；
- 老引擎（vendor/sd-scripts 等）零改动。
