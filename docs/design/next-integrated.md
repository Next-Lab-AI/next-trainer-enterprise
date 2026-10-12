# next-integrated 自研集成引擎设计

> 2026-10-12 定稿。调研依据见工作区外层
> `docs/research-2026-10-12-next-integrated.md`（不随仓库提交），
> 本文是该调研结论的固化，自包含。

## 定位与动机

- **第一诉求：消灭四套 Python 环境**。ToB 交付一个引擎包一个 venv，
  替代 sd-scripts / ai-toolkit / anima-lora / diffsynth 图片侧四套并存。
- **方法是硬抄不是研发**：模型适配器从各引擎现成实现抄起再收敛，
  不重写 legacy、不追上游、不求最优架构；抄完定型，模型按需加。
- **慢工程，独立分支，不阻塞主线**（bootstrap 任务书已定性"卖点级"）。
  vendor 老引擎在对应模型被吸收完之前照常维护，是交付兜底。

## 范围

- In scope：图片生成型训练，LoRA 先行（所有模型），
  full-ft / ControlNet / TI 随需求排期。
- Out of scope：视频（musubi-tuner 全族、diffsynth 视频/音频 pipeline）、
  推理类（tagger / tag-translation），维持独立引擎不动。

## 环境钉版基线

硬件下限 = **Ampere（sm_80）**：训练以 bf16 为主，bf16 原生支持自
Ampere 起；Turing（2080 Ti 等）在 cu128 wheel 里能点亮但无原生 bf16，
CUDA 13 已将其列入废弃——亮不了就给客户发老 vendor 引擎镜像，不回头兼容。

| 项 | 钉法 | 依据 |
|---|---|---|
| Python | 3.13 | 下方组合在本机 GB10（aarch64）已验证 |
| torch | 2.12.x + cu132 单一 index | 一套 wheel 覆盖 sm_80→sm_120；不追 2.13/2.14（quanto/flash-attn 预编译覆盖未跟上，无必升特性） |
| transformers | 5.5.x | 可信源最高钉版（ai-toolkit ==5.5.3）；anima-lora 的 ≥5.10 是激进货，不采信 |
| diffusers | 对齐 ai-toolkit / sd-scripts 取交集，第一期 smoke 时定版 | — |

flash-attn 用 mjun0812 预编译 wheel（aarch64/x86_64 均有
`cu132torch2.12-cp313`），装不上就退 SDPA，不阻塞。

## 总体结构

```
vendor/next-integrated/
├── pyproject.toml          # 钉版基线 + 依赖组（cuda-windows / rocm 等）
├── engine.toml             # 引擎契约 manifest，自研引擎即一等公民
├── engine.d/               # 契约 hooks（JSONL 输出原生实现，零 wrapper）
├── next_integrated/
│   ├── core/               # 统一训练核心库（见下）
│   ├── adapters/<model>/   # 模型薄适配器（见下）
│   └── optimizers/         # automagic 家族等
└── train.py                # 统一入口：配置文件进、JSONL 出、退出码收尾
```

种子 = **原来单独维护的远古 sd-scripts 源码**（kohya upstream @206adb6，
2025-09；不是 vendor/sd-scripts 这个 patched fork——fork 里的现代模型
覆盖是吸收目标，不是起点）。

参考源三个，分工明确：
- 种子（@206adb6）：骨架与 SD1.5/SDXL adapter 的起点；
- 新上游快照（lora-scripts-next-test/sd-scripts @37a1cbb，2026-07）：
  升级对照——transformers/diffusers 新老 API 差异、bug 修复照抄；
- vendor/sd-scripts（patched fork）：Anima/Flux 等现代模型训练实现的
  吸收源。

### 核心库模块划分

| 模块 | 内容 | 来源 |
|---|---|---|
| `core/dataset` | bucketing、caption 读取、latent 缓存 | 拆远古 sd-scripts `train_util.py`（6942 行单文件，机械拆解，靠 smoke 兜底） |
| `core/network` | LoRA / LoHa / DoRA 实现与 apply/save | sd-scripts networks + ai-toolkit 对照 |
| `core/optimizers` | 优化器注册表；automagic 家族移植 | ai-toolkit `toolkit/optimizers/` |
| `core/checkpoint` | checkpoint 读写（格式见下节） | 新写 |
| `core/contract` | JSONL 帧输出、SIGTERM 优雅停止、退出码 | 新写（契约原生，agent 零适配） |
| `core/config` | TOML 配置直接解析，**无 argparse 转换层** | 新写 |

### 模型 adapter 接口

每个模型一个 `adapters/<model>/` 包，三件套：

```python
class ModelAdapter(Protocol):
    def load(self, cfg) -> Bundle: ...      # 加载 base 模型/TE/VAE，返回权重包
    def forward(self, bundle, batch) -> LossOut: ...   # 前向 + loss
    def sample(self, bundle, prompt) -> Image: ...     # 训练中采样预览（可选）
```

adapter 负责模型特有的一切（TE 组合、timestep 采样策略、v-pred/eps、
flow-matching 参数化），核心库只管通用训练循环。新增模型 = 抄一份
现成实现收敛成 adapter，不动核心。

## checkpoint 格式

**不做 kohya 格式兼容**（2026-10-12 定）：kohya 的 `ss_*` metadata 与
key 命名约定已过时，ai-toolkit / diffsynth 均不依赖它。统一采用
**diffusers/PEFT 风格的标准 safetensors LoRA**（`lora_unet_*` /
TE 命名按 diffusers 约定），metadata 只写通用字段
（base model、rank、alpha、训练配置摘要）。

存量 kohya 格式资产由 vendor/sd-scripts 继续兼容（它的存续价值之一）；
如未来确有转换需求，另起独立转换工具，不污染训练核心。

## automagic 移植清单

从 `vendor/ai-toolkit/toolkit/optimizers/` 移植：

- `automagic.py` / `automagic2.py` / `automagic3.py`
  （`automagicEXPERIMENT.py` 不移植，实验货）；
- 依赖项：`optimizer_utils`（同目录，一并抄）、
  `optimum.quanto` 8bit tensor（外部依赖，钉版后实测兼容性）；
- sd-scripts 侧的自适应优化器（Prodigy / D-Adaptation / schedulefree）
  走 `pytorch-optimizer` 三方包，按需补，不预置。

落点为 `next_integrated/optimizers/`，注册进 `core/optimizers` 注册表，
与 adamw 等并列可选。

## 模型吸收顺序与 deprecation 策略

总量口径：主线 13 个模型族 + diffsynth 长尾 7+（约 20+），**不追新、
客户点到才加**。当前需求口径收敛为三个先行：SD1.5、SDXL、Anima-DiT。

| 序 | 模型 | 抄自 | 说明 |
|---|---|---|---|
| 1 | SDXL | 远古 sd-scripts 种子自带 | 第一期打样，验证核心库 + 契约 |
| 2 | SD1.5 | 种子自带 | 与 SDXL 同 pipeline，顺手收敛，工程量小 |
| 3 | Anima-DiT | **sd-scripts 的 anima_train***（保本实现） | 自研模型差异化；anima-lora 仅作算法参考（专用网络/校准思路），不进依赖树 |
| 4+ | 积压：Flux / Qwen-Image / SD3 / Lumina / Hunyuan-Image / Chroma / AuraFlow / CogView4 / HiDream / diffsynth 长尾 | 各对应引擎 | 按客户优先级从积压里挑，不预排序 |

deprecation 口径：老引擎对应模型的 capabilities 标签在 engine.toml 里
打 `deprecated` 注记，web 侧引导到 next-integrated；老引擎不再加新模型。

## 风险

| 风险 | 缓解 |
|---|---|
| transformers 4→5 API 断裂消化不全（sd-scripts 种子钉 4.54） | 第一期 SDXL smoke 顺带验证；Anima 吸收前先过 tokenizer/TE 加载层 |
| quanto 在 torch 2.12 下不兼容 | 钉版后第一时间实测；不行则 automagic 退 32bit 路径，不阻塞核心 |
| train_util.py 拆解引入隐性回归 | 纯机械拆 + SDXL LoRA smoke 对比老引擎 loss 曲线 |
| 慢工程被主线挤占 | 独立分支；vendor 老引擎是兜底，钉版错了也可回退 |

## 不采用

- 一步到位全新架构（路线 B）：架构赌博，违背"硬抄"定位。
- kohya checkpoint 兼容层：格式已过时，见上节。
- 追上游 / 追新模型：抄完定型，模型按需加。
- anima-lora 作为代码源或依赖源：激进第三方实现，ToB 安全性存疑。
