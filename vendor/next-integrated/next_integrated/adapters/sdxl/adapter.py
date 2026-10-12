"""SDXL adapter：加载/前向（+loss）/采样 三件套。

薄适配器：加载与模型结构复用种子 library/（sdxl_model_util /
sdxl_original_unet / train_util 的 TE 编码与尺寸嵌入），训练步数学与
种子 sdxl_train.py 主回路逐步对应（eps 预测、DDPM scaled_linear、
VAE scale 0.13025）。本期 smoke 只走 eps-prediction + 均匀 timestep；
v-pred / min_snr / huber 等随需求补。
"""

from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Optional

import torch
import torch.nn.functional as F
from diffusers import DDPMScheduler
from transformers import CLIPTokenizer

from next_integrated.adapters.sdxl import _compat  # noqa: F401  # 必须先于 library/ 导入

from library import custom_train_functions, model_util, sdxl_model_util, sdxl_train_util, train_util

VAE_SCALE_FACTOR = sdxl_model_util.VAE_SCALE_FACTOR  # 0.13025

TOKENIZER1_PATH = "openai/clip-vit-large-patch14"
TOKENIZER2_PATH = "laion/CLIP-ViT-bigG-14-laion2B-39B-b160k"

_DTYPES = {"bf16": torch.bfloat16, "fp16": torch.float16, "fp32": torch.float32}


@dataclass
class Bundle:
    device: torch.device
    weight_dtype: torch.dtype
    tokenizer1: CLIPTokenizer
    tokenizer2: CLIPTokenizer
    text_encoder1: torch.nn.Module
    text_encoder2: torch.nn.Module
    vae: torch.nn.Module
    unet: torch.nn.Module
    noise_scheduler: DDPMScheduler
    resolution: int

    @property
    def text_encoders(self) -> list[torch.nn.Module]:
        return [self.text_encoder1, self.text_encoder2]


def _load_tokenizer(path: str) -> CLIPTokenizer:
    # 本地缓存优先：网络不可达时 from_pretrained 的默认在线探测会挂数分钟
    try:
        return CLIPTokenizer.from_pretrained(path, local_files_only=True)
    except Exception:
        return CLIPTokenizer.from_pretrained(path)


def load(cfg: dict) -> Bundle:
    device = torch.device(cfg.get("device", "cuda"))
    weight_dtype = _DTYPES[cfg.get("weight_dtype", "bf16")]
    resolution = int(cfg.get("resolution", 1024))

    ckpt = cfg["pretrained_model"]
    (
        text_encoder1,
        text_encoder2,
        vae,
        unet,
        _logit_scale,
        _ckpt_info,
    ) = sdxl_model_util.load_models_from_sdxl_checkpoint(
        sdxl_model_util.MODEL_VERSION_SDXL_BASE_V1_0, ckpt, "cpu"
    )

    vae_path = cfg.get("vae")
    if vae_path:
        vae = model_util.load_vae(vae_path, torch.float32)

    tokenizer1 = _load_tokenizer(cfg.get("tokenizer1", TOKENIZER1_PATH))
    tokenizer2 = _load_tokenizer(cfg.get("tokenizer2", TOKENIZER2_PATH))
    tokenizer2.pad_token_id = 0  # 与种子一致：对齐 open clip tokenizer

    vae.to(device, dtype=torch.float32).eval()
    vae.requires_grad_(False)
    text_encoder1.to(device, dtype=weight_dtype).eval()
    text_encoder2.to(device, dtype=weight_dtype).eval()
    unet.to(device, dtype=weight_dtype).eval()
    for m in (text_encoder1, text_encoder2, unet):
        m.requires_grad_(False)

    if cfg.get("gradient_checkpointing", False):
        unet.set_gradient_checkpointing(True)
    unet.set_use_sdpa(True)

    noise_scheduler = DDPMScheduler(
        beta_start=0.00085,
        beta_end=0.012,
        beta_schedule="scaled_linear",
        num_train_timesteps=1000,
        clip_sample=False,
    )
    custom_train_functions.prepare_scheduler_for_custom_training(noise_scheduler, device)

    return Bundle(
        device=device,
        weight_dtype=weight_dtype,
        tokenizer1=tokenizer1,
        tokenizer2=tokenizer2,
        text_encoder1=text_encoder1,
        text_encoder2=text_encoder2,
        vae=vae,
        unet=unet,
        noise_scheduler=noise_scheduler,
        resolution=resolution,
    )


def encode_text(bundle: Bundle, captions: list[str], grad_enabled: bool = False):
    tok1, tok2 = bundle.tokenizer1, bundle.tokenizer2
    input_ids1 = tok1(
        captions, padding="max_length", max_length=tok1.model_max_length, truncation=True, return_tensors="pt"
    ).input_ids.to(bundle.device)
    input_ids2 = tok2(
        captions, padding="max_length", max_length=tok2.model_max_length, truncation=True, return_tensors="pt"
    ).input_ids.to(bundle.device)

    with torch.set_grad_enabled(grad_enabled):
        hs1, hs2, pool2 = train_util.get_hidden_states_sdxl(
            None,
            input_ids1,
            input_ids2,
            tok1,
            tok2,
            bundle.text_encoder1,
            bundle.text_encoder2,
            bundle.weight_dtype,
        )
    return hs1, hs2, pool2


def forward(bundle: Bundle, batch: dict, train_text_encoder: bool = False) -> torch.Tensor:
    device, weight_dtype = bundle.device, bundle.weight_dtype

    with torch.no_grad():
        latents = bundle.vae.encode(batch["images"].to(device, dtype=torch.float32)).latent_dist.sample()
        if torch.any(torch.isnan(latents)):
            latents = torch.nan_to_num(latents, 0)
        latents = (latents * VAE_SCALE_FACTOR).to(weight_dtype)

    hs1, hs2, pool2 = encode_text(bundle, batch["captions"], grad_enabled=train_text_encoder)

    embs = sdxl_train_util.get_size_embeddings(
        batch["original_sizes_hw"], batch["crop_top_lefts"], batch["target_sizes_hw"], device
    ).to(weight_dtype)
    vector_embedding = torch.cat([pool2, embs], dim=1).to(weight_dtype)
    text_embedding = torch.cat([hs1, hs2], dim=2).to(weight_dtype)

    noise = torch.randn_like(latents)
    timesteps = torch.randint(
        0, bundle.noise_scheduler.config.num_train_timesteps, (latents.shape[0],), device=device
    )
    noisy_latents = bundle.noise_scheduler.add_noise(latents, noise, timesteps).to(weight_dtype)

    noise_pred = bundle.unet(noisy_latents, timesteps, text_embedding, vector_embedding)

    loss = F.mse_loss(noise_pred.float(), noise.float(), reduction="mean")
    return loss
