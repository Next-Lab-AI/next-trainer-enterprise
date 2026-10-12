"""优化器注册表：按配置名构建优化器。

automagic 家族移植自 ai-toolkit（见 next_integrated/optimizers/）；
Prodigy / D-Adaptation / schedulefree 等自适应优化器走 pytorch-optimizer
三方包按需补，不预置（见 docs/design/next-integrated.md §automagic 移植清单）。
"""

from __future__ import annotations

import torch


def build_optimizer(name: str, params, **kwargs) -> torch.optim.Optimizer:
    key = name.lower()
    if key == "adamw":
        return torch.optim.AdamW(params, **kwargs)
    if key == "adamw8bit":
        # quanto 8bit 路径待 Step 2 实测后接；先用 torch 原生 AdamW 占位会
        # 静默改变语义，这里直接报未实现，避免误用。
        raise NotImplementedError("adamw8bit: quanto path pending validation")
    if key == "automagic":
        from next_integrated.optimizers import Automagic

        return Automagic(params, **kwargs)
    if key == "automagic2":
        from next_integrated.optimizers import Automagic2

        return Automagic2(params, **kwargs)
    if key == "automagic3":
        from next_integrated.optimizers import Automagic3

        return Automagic3(params, **kwargs)
    raise ValueError(f"unknown optimizer: {name}")


def available_optimizers() -> list[str]:
    return ["adamw", "automagic", "automagic2", "automagic3"]
