# LoRA network module, ported from the seed sd-scripts networks/lora.py
# (kohya-ss/sd-scripts @206adb6). Training-time math is unchanged; regional /
# sub-prompt inference paths, block_dims and LyCORIS plumbing are out of scope
# for Phase 1.
#
# Naming: modules keep their diffusers-style dotted path (e.g.
# "down_blocks.0.attentions.0.transformer_blocks.0.attn1.to_q") plus a prefix
# ("unet" / "text_encoder" / "text_encoder_2"), which core/checkpoint turns
# into diffusers/PEFT save keys. The seed's kohya-style flattened names are
# not used anywhere in the new core.

import math
from typing import Dict, List, Optional, Union

import torch


class LoRAModule(torch.nn.Module):
    """
    replaces forward method of the original Linear, instead of replacing the original Linear module.
    """

    def __init__(
        self,
        lora_name,
        org_module: torch.nn.Module,
        multiplier=1.0,
        lora_dim=4,
        alpha=1,
        dropout=None,
        rank_dropout=None,
        module_dropout=None,
    ):
        """if alpha == 0 or None, alpha is rank (no scaling)."""
        super().__init__()
        self.lora_name = lora_name

        if org_module.__class__.__name__ == "Conv2d":
            in_dim = org_module.in_channels
            out_dim = org_module.out_channels
        else:
            in_dim = org_module.in_features
            out_dim = org_module.out_features

        self.lora_dim = lora_dim

        if org_module.__class__.__name__ == "Conv2d":
            kernel_size = org_module.kernel_size
            stride = org_module.stride
            padding = org_module.padding
            self.lora_down = torch.nn.Conv2d(in_dim, self.lora_dim, kernel_size, stride, padding, bias=False)
            self.lora_up = torch.nn.Conv2d(self.lora_dim, out_dim, (1, 1), (1, 1), bias=False)
        else:
            self.lora_down = torch.nn.Linear(in_dim, self.lora_dim, bias=False)
            self.lora_up = torch.nn.Linear(self.lora_dim, out_dim, bias=False)

        if type(alpha) == torch.Tensor:
            alpha = alpha.detach().float().numpy()  # without casting, bf16 causes error
        alpha = self.lora_dim if alpha is None or alpha == 0 else alpha
        self.scale = alpha / self.lora_dim
        self.register_buffer("alpha", torch.tensor(alpha))

        # same as microsoft's
        torch.nn.init.kaiming_uniform_(self.lora_down.weight, a=math.sqrt(5))
        torch.nn.init.zeros_(self.lora_up.weight)

        self.multiplier = multiplier
        self.org_module = org_module  # remove in applying
        self.dropout = dropout
        self.rank_dropout = rank_dropout
        self.module_dropout = module_dropout

    def apply_to(self):
        self.org_forward = self.org_module.forward
        self.org_module.forward = self.forward
        del self.org_module

    def forward(self, x):
        org_forwarded = self.org_forward(x)

        # module dropout
        if self.module_dropout is not None and self.training:
            if torch.rand(1) < self.module_dropout:
                return org_forwarded

        lx = self.lora_down(x)

        # normal dropout
        if self.dropout is not None and self.training:
            lx = torch.nn.functional.dropout(lx, p=self.dropout)

        # rank dropout
        if self.rank_dropout is not None and self.training:
            mask = torch.rand((lx.size(0), self.lora_dim), device=lx.device) > self.rank_dropout
            if len(lx.size()) == 3:
                mask = mask.unsqueeze(1)  # for Text Encoder
            elif len(lx.size()) == 4:
                mask = mask.unsqueeze(-1).unsqueeze(-1)  # for Conv2d
            lx = lx * mask

            # scaling for rank dropout: treat as if the rank is changed
            scale = self.scale * (1.0 / (1.0 - self.rank_dropout))  # redundant for readability
        else:
            scale = self.scale

        lx = self.lora_up(lx)

        return org_forwarded + lx * self.multiplier * scale

    @torch.no_grad()
    def get_delta_weight(self, multiplier=None) -> torch.Tensor:
        """Effective weight delta (up @ down * scale), for merge/verification."""
        if multiplier is None:
            multiplier = self.multiplier
        up_weight = self.lora_up.weight.to(torch.float)
        down_weight = self.lora_down.weight.to(torch.float)
        if len(down_weight.size()) == 2:
            return multiplier * (up_weight @ down_weight) * self.scale
        if down_weight.size()[2:4] == (1, 1):
            return (
                multiplier
                * (up_weight.squeeze(3).squeeze(2) @ down_weight.squeeze(3).squeeze(2)).unsqueeze(2).unsqueeze(3)
                * self.scale
            )
        conved = torch.nn.functional.conv2d(down_weight.permute(1, 0, 2, 3), up_weight).permute(1, 0, 2, 3)
        return multiplier * conved * self.scale


class LoRANetwork(torch.nn.Module):
    UNET_TARGET_REPLACE_MODULE = ["Transformer2DModel"]
    UNET_TARGET_REPLACE_MODULE_CONV2D_3X3 = ["ResnetBlock2D", "Downsample2D", "Upsample2D"]
    TEXT_ENCODER_TARGET_REPLACE_MODULE = ["CLIPAttention", "CLIPSdpaAttention", "CLIPMLP"]

    # diffusers 侧的根名（checkpoint key 前缀）
    PREFIX_UNET = "unet"
    PREFIX_TEXT_ENCODER = "text_encoder"
    PREFIX_TEXT_ENCODER_2 = "text_encoder_2"

    def __init__(
        self,
        text_encoder: Union[List[torch.nn.Module], torch.nn.Module, None],
        unet: torch.nn.Module,
        multiplier: float = 1.0,
        lora_dim: int = 4,
        alpha: float = 1,
        dropout: Optional[float] = None,
        rank_dropout: Optional[float] = None,
        module_dropout: Optional[float] = None,
        conv_lora_dim: Optional[int] = None,
        conv_alpha: Optional[float] = None,
        train_text_encoder: bool = True,
    ) -> None:
        super().__init__()
        self.multiplier = multiplier
        self.lora_dim = lora_dim
        self.alpha = alpha
        self.conv_lora_dim = conv_lora_dim
        self.conv_alpha = conv_alpha
        self.dropout = dropout
        self.rank_dropout = rank_dropout
        self.module_dropout = module_dropout

        def create_modules(
            prefix: str,
            root_module: torch.nn.Module,
            target_replace_modules: List[str],
        ) -> List[LoRAModule]:
            loras = []
            for name, module in root_module.named_modules():
                if module.__class__.__name__ in target_replace_modules:
                    for child_name, child_module in module.named_modules():
                        is_linear = child_module.__class__.__name__ == "Linear"
                        is_conv2d = child_module.__class__.__name__ == "Conv2d"
                        is_conv2d_1x1 = is_conv2d and child_module.kernel_size == (1, 1)

                        if not (is_linear or is_conv2d):
                            continue

                        if is_linear or is_conv2d_1x1:
                            dim, alpha = self.lora_dim, self.alpha
                        elif self.conv_lora_dim is not None:
                            dim, alpha = self.conv_lora_dim, self.conv_alpha
                        else:
                            continue

                        lora_path = f"{name}.{child_name}"
                        lora_name = f"{prefix}.{lora_path}"
                        lora = LoRAModule(
                            lora_name,
                            child_module,
                            self.multiplier,
                            dim,
                            alpha,
                            dropout=dropout,
                            rank_dropout=rank_dropout,
                            module_dropout=module_dropout,
                        )
                        loras.append(lora)
            return loras

        self.text_encoder_loras: List[LoRAModule] = []
        if train_text_encoder and text_encoder is not None:
            text_encoders = text_encoder if type(text_encoder) == list else [text_encoder]
            for i, te in enumerate(text_encoders):
                if len(text_encoders) > 1:
                    prefix = self.PREFIX_TEXT_ENCODER if i == 0 else self.PREFIX_TEXT_ENCODER_2
                else:
                    prefix = self.PREFIX_TEXT_ENCODER
                self.text_encoder_loras.extend(
                    create_modules(prefix, te, LoRANetwork.TEXT_ENCODER_TARGET_REPLACE_MODULE)
                )

        target_modules = LoRANetwork.UNET_TARGET_REPLACE_MODULE
        if self.conv_lora_dim is not None:
            target_modules = target_modules + LoRANetwork.UNET_TARGET_REPLACE_MODULE_CONV2D_3X3
        self.unet_loras: List[LoRAModule] = create_modules(self.PREFIX_UNET, unet, target_modules)

        names = set()
        for lora in self.text_encoder_loras + self.unet_loras:
            assert lora.lora_name not in names, f"duplicated lora name: {lora.lora_name}"
            names.add(lora.lora_name)

    def set_multiplier(self, multiplier):
        self.multiplier = multiplier
        for lora in self.text_encoder_loras + self.unet_loras:
            lora.multiplier = self.multiplier

    def apply_to(self, apply_text_encoder=True, apply_unet=True):
        if not apply_text_encoder:
            self.text_encoder_loras = []
        if not apply_unet:
            self.unet_loras = []

        for lora in self.text_encoder_loras + self.unet_loras:
            lora.apply_to()
            # nn.Module 注册名不允许含 "."，注册名仅用于 state_dict 组织，与保存格式无关
            self.add_module(lora.lora_name.replace(".", "_"), lora)

    def prepare_optimizer_params(self, text_encoder_lr=None, unet_lr=None, default_lr=None):
        params = []
        if self.text_encoder_loras:
            params.append(
                {
                    "params": [p for lora in self.text_encoder_loras for p in lora.parameters()],
                    "lr": text_encoder_lr if text_encoder_lr is not None else default_lr,
                }
            )
        if self.unet_loras:
            params.append(
                {
                    "params": [p for lora in self.unet_loras for p in lora.parameters()],
                    "lr": unet_lr if unet_lr is not None else default_lr,
                }
            )
        return params

    def named_lora_modules(self):
        for lora in self.text_encoder_loras + self.unet_loras:
            yield lora.lora_name, lora
