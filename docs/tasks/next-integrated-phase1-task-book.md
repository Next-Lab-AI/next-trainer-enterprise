# next-integrated Phase 1 任务书（骨架 + SDXL 打样）

## 计划元数据

- Plan ID: EE-NEXTINT-P1-20261012
- Version: v1
- Last updated: 2026-10-12 Asia/Shanghai
- Canonical progress file: `docs/tasks/next-integrated-phase1-task-book.md`
- Current branch: 独立 feature 分支（`feat/next-integrated-*`，不阻塞主线）
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

### Step 1: 骨架与契约

- Outputs: pyproject（依赖组参考 anima-lora 的多平台写法但版本自定）、
  engine.toml（capabilities 先写 `["sdxl"]`）、engine.d/、train.py、
  `core/contract` + `core/config`。
- Validation: agent 按引擎契约拉起 `train.py`，假训练循环的
  progress/log/artifact/error 帧完整到达 server；SIGTERM 有 checkpoint。

### Step 2: automagic + LoRA

- Outputs: `optimizers/`（automagic 1/2/3 + optimizer_utils）、
  `core/network` LoRA、`core/checkpoint`。
- Validation: quanto 8bit 在 torch 2.12 下实测通过（不通过则 automagic
  先走 32bit 路径并记录）；LoRA save/load roundtrip 与种子实现一致。

### Step 3: SDXL adapter + smoke

- Outputs: `adapters/sdxl/` 三件套；diffusers 版本定版回填设计文档。
- Validation: 单卡 SDXL LoRA smoke 跑通，loss 曲线与 vendor/sd-scripts
  同配置对照无异常；产出 checkpoint 可被 diffusers 直接加载。

### Step 4: SD1.5 adapter

- Outputs: `adapters/sd15/`（与 SDXL 同 pipeline 收敛，顺手做）。
- Validation: 单卡 SD1.5 LoRA smoke 跑通。
- 说明：Anima-DiT 是当前口径第三个模型，但吸收涉及 transformers 5.5
  兼容排查，独立排期在本期之后（见设计文档吸收顺序）。

## 钉版验证清单（本期顺带关闭）

- [ ] sd-scripts `anima_train*` 在 transformers 5.5 下的 API 兼容排查
      （T5/LLM tokenizer 加载层，为 Step 后的 Anima 吸收铺路）
- [ ] optimum.quanto 在 torch 2.12 下的实测（Step 2）
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
