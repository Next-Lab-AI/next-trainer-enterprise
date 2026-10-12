"""checkpoint 读写：diffusers/PEFT 风格 safetensors LoRA（无 kohya 兼容）。

保存格式（docs/design/next-integrated.md §checkpoint 格式）：
- key: ``unet.<diffusers 模块路径>.lora_A.weight`` / ``.lora_B.weight``，
  TE 前缀 ``text_encoder.`` / ``text_encoder_2.``；
- alpha/rank 缩放（alpha/rank）在保存时烘进 lora_B——diffusers 原生加载
  PEFT 格式时不读 alpha（scale 恒为 1），烘焙后加载行为在任何配置下都正确；
- metadata 只写通用字段（rank/alpha/训练配置摘要，字符串值）。

命名转换：LoRA 模块内部沿用种子 UNet 的路径（input_blocks/output_blocks），
保存/加载时经 kohya key → diffusers legacy key → PEFT key 的转换链映射到
diffusers 模块路径（全部复用 diffusers 官方转换函数，不手写 block 映射表）。
TE 内部路径本身就是 diffusers 约定，直通。

加载（resume 训练）约定：烘焙后的权重对应 scale=1，因此用于加载的
LoRANetwork 必须以 alpha == rank 构建；本模块在加载时校验并拒绝
scale != 1 的网络，避免双重缩放。
"""

from __future__ import annotations

import json
from functools import lru_cache
from pathlib import Path
from typing import Optional

import torch
from safetensors.torch import load_file, save_file

from next_integrated.core.network.lora import LoRANetwork

METADATA_FORMAT = "next-integrated.lora"


@lru_cache(maxsize=None)
def _unet_path_to_diffusers(path: str) -> str:
    """种子 UNet 内部路径（input_blocks.4.1.transformer_blocks.0.attn1.to_q）
    → diffusers 模块路径（down_blocks.1.attentions.0.transformer_blocks.0.attn1.to_q）。

    完整复刻 diffusers 加载 kohya LoRA 的转换链（sgm 索引映射 → kohya key
    改名 → PEFT 格式），映射表以 diffusers 为准。layers_per_block 固定 2
    （SD1.5/SDXL 相同）。"""
    from types import SimpleNamespace

    from diffusers.loaders.lora_conversion_utils import (
        _convert_unet_lora_key,
        _maybe_map_sgm_blocks_to_diffusers,
    )
    from diffusers.utils.state_dict_utils import convert_unet_state_dict_to_peft

    dummy = torch.zeros(1)
    kohya_key = "lora_unet_" + path.replace(".", "_") + ".lora_down.weight"
    mapped = _maybe_map_sgm_blocks_to_diffusers(
        {kohya_key: dummy}, SimpleNamespace(layers_per_block=2), delimiter="_"
    )
    (mapped_key,) = mapped.keys()
    legacy_key = _convert_unet_lora_key(mapped_key)
    modern = convert_unet_state_dict_to_peft({legacy_key: dummy})
    (modern_key,) = modern.keys()
    # "...attn1.to_q.lora_A.weight" → 模块路径
    return modern_key.removesuffix(".lora_A.weight")


def _module_save_key(lora_name: str) -> str:
    """内部模块名（unet.xxx / text_encoder.xxx）→ checkpoint 内 lora_A key。"""
    prefix, _, path = lora_name.partition(".")
    if prefix == LoRANetwork.PREFIX_UNET:
        return f"unet.{_unet_path_to_diffusers(path)}.lora_A.weight"
    # TE 路径已是 diffusers 约定，直通
    return f"{lora_name}.lora_A.weight"


def _is_unet_1x1_conv_target(lora_name: str) -> bool:
    """种子把 proj_in/proj_out 实现为 Linear，diffusers 侧是 Conv2d 1x1，
    保存/加载时要在 [out, in] 与 [out, in, 1, 1] 之间转换。"""
    prefix, _, path = lora_name.partition(".")
    return prefix == LoRANetwork.PREFIX_UNET and (path.endswith(".proj_in") or path.endswith(".proj_out"))


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
        key_a = _module_save_key(name)
        key_b = key_a.replace(".lora_A.weight", ".lora_B.weight")
        down = lora.lora_down.weight.detach()
        up = lora.lora_up.weight.detach()
        # 缩放烘进 up（lora_B）：文件在 diffusers scale=1 约定下即正确
        up = up.to(torch.float32) * lora.scale
        if _is_unet_1x1_conv_target(name) and down.ndim == 2:
            down = down.unsqueeze(-1).unsqueeze(-1)
            up = up.unsqueeze(-1).unsqueeze(-1)
        if dtype is not None:
            down = down.to(dtype)
            up = up.to(dtype)
        state[key_a] = down.contiguous()
        state[key_b] = up.contiguous()
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

    missing = []
    seen = set()
    for name, lora in network.named_lora_modules():
        key_a = _module_save_key(name)
        key_b = key_a.replace(".lora_A.weight", ".lora_B.weight")
        if key_a not in state or key_b not in state:
            missing.append(name)
            continue
        down, up = state[key_a], state[key_b]
        if dtype is not None:
            down, up = down.to(dtype), up.to(dtype)
        if _is_unet_1x1_conv_target(name) and down.ndim == 4:
            down = down.squeeze(-1).squeeze(-1)
            up = up.squeeze(-1).squeeze(-1)
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
