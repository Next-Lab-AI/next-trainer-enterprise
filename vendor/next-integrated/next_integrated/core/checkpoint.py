"""checkpoint 读写：diffusers/PEFT 风格 safetensors LoRA（无 kohya 兼容）。

保存格式（docs/design/next-integrated.md §checkpoint 格式）：
- key: ``unet.<dotted path>.lora_A.weight`` / ``.lora_B.weight``，
  TE 前缀 ``text_encoder.`` / ``text_encoder_2.``；
- alpha/rank 缩放（alpha/rank）在保存时烘进 lora_B——diffusers 原生加载
  PEFT 格式时不读 alpha（scale 恒为 1），烘焙后加载行为在任何配置下都正确；
- metadata 只写通用字段（rank/alpha/训练配置摘要，字符串值）。

加载（resume 训练）约定：烘焙后的权重对应 scale=1，因此用于加载的
LoRANetwork 必须以 alpha == rank 构建；本模块在加载时校验并拒绝
scale != 1 的网络，避免双重缩放。
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Optional

import torch
from safetensors.torch import load_file, save_file

from next_integrated.core.network.lora import LoRANetwork

METADATA_FORMAT = "next-integrated.lora"


def save_lora(
    network: LoRANetwork,
    path: str | Path,
    dtype: Optional[torch.dtype] = None,
    metadata: Optional[dict] = None,
) -> None:
    state = {}
    ranks = set()
    alphas = set()
    for name, lora in network.named_lora_modules():
        down = lora.lora_down.weight.detach()
        up = lora.lora_up.weight.detach()
        # 缩放烘进 up（lora_B）：文件在 diffusers scale=1 约定下即正确
        up = up.to(torch.float32) * lora.scale
        if dtype is not None:
            down = down.to(dtype)
            up = up.to(dtype)
        state[f"{name}.lora_A.weight"] = down.contiguous()
        state[f"{name}.lora_B.weight"] = up.contiguous()
        ranks.add(lora.lora_dim)
        alphas.add(float(lora.alpha))

    md = {"format": METADATA_FORMAT}
    if len(ranks) == 1:
        md["rank"] = str(ranks.pop())
    if len(alphas) == 1:
        md["alpha"] = str(alphas.pop())
    if metadata:
        md.update({k: (v if isinstance(v, str) else json.dumps(v, ensure_ascii=False)) for k, v in metadata.items()})

    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    save_file(state, str(path), metadata=md)


def load_lora(
    network: LoRANetwork,
    path: str | Path,
    dtype: Optional[torch.dtype] = None,
) -> dict:
    """把 checkpoint 读进已构建的 network（resume）。返回 metadata。"""
    for _, lora in network.named_lora_modules():
        if lora.scale != 1.0:
            raise ValueError(
                f"load_lora requires alpha == rank (scale == 1), got scale {lora.scale} "
                f"for {lora.lora_name}: saved LoRA has alpha/rank baked into lora_B"
            )

    state = load_file(str(path))
    metadata = load_metadata(path)

    missing, unexpected = [], []
    seen = set()
    for name, lora in network.named_lora_modules():
        key_a, key_b = f"{name}.lora_A.weight", f"{name}.lora_B.weight"
        if key_a not in state or key_b not in state:
            missing.append(name)
            continue
        down, up = state[key_a], state[key_b]
        if dtype is not None:
            down, up = down.to(dtype), up.to(dtype)
        with torch.no_grad():
            lora.lora_down.weight.copy_(down)
            lora.lora_up.weight.copy_(up)
        seen.update((key_a, key_b))

    unexpected = [k for k in state if k not in seen]
    if missing:
        raise ValueError(f"checkpoint missing {len(missing)} modules, e.g. {missing[:3]}")
    if unexpected:
        raise ValueError(f"network missing {len(unexpected)} checkpoint keys, e.g. {unexpected[:3]}")

    return metadata


def load_metadata(path: str | Path) -> dict:
    from safetensors import safe_open

    with safe_open(str(path), framework="pt") as f:
        return f.metadata() or {}
