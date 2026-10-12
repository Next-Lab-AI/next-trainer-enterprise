"""SD1.5 adapter：加载/前向（+loss）/采样 三件套。

与 SDXL 同 pipeline 收敛：接口与 adapters/sdxl 一致（Bundle/forward 签名
相同，train.py 共用同一训练回路）。数学与种子 train_network.py 主回路
逐步对应：eps 预测、DDPM scaled_linear、VAE scale 0.18215、tokenizer
padding 到 max_length 后 text_encoder(input_ids)[0]（clip_skip 等随需
求补）。
"""

from __future__ import annotations

from dataclasses import dataclass

import torch
import torch.nn.functional as F
from diffusers import DDPMScheduler
from transformers import CLIPTokenizer

from next_integrated.adapters.sdxl import _compat  # noqa: F401  # 必须先于 library/ 导入

from library import custom_train_functions, model_util

VAE_SCALE_FACTOR = 0.18215

TOKENIZER_PATH = "openai/clip-vit-large-patch14"

_DTYPES = {"bf16": torch.bfloat16, "fp16": torch.float16, "fp32": torch.float32}


@dataclass
class Bundle:
    device: torch.device
    weight_dtype: torch.dtype
    tokenizer: CLIPTokenizer
    text_encoder: torch.nn.Module
    vae: torch.nn.Module
    unet: torch.nn.Module
    noise_scheduler: DDPMScheduler
    resolution: int

    @property
    def text_encoders(self) -> list[torch.nn.Module]:
        return [self.text_encoder]


def _load_tokenizer(path: str) -> CLIPTokenizer:
    # 本地缓存优先：网络不可达时 from_pretrained 的默认在线探测会挂数分钟
    try:
        return CLIPTokenizer.from_pretrained(path, local_files_only=True)
    except Exception:
        return CLIPTokenizer.from_pretrained(path)


def load(cfg: dict) -> Bundle:
    device = torch.device(cfg.get("device", "cuda"))
    weight_dtype = _DTYPES[cfg.get("weight_dtype", "bf16")]
    resolution = int(cfg.get("resolution", 512))

    ckpt = cfg["pretrained_model"]
    text_encoder, vae, unet = model_util.load_models_from_stable_diffusion_checkpoint(False, ckpt, "cpu")

    vae_path = cfg.get("vae")
    if vae_path:
        vae = model_util.load_vae(vae_path, torch.float32)

    tokenizer = _load_tokenizer(cfg.get("tokenizer", TOKENIZER_PATH))

    vae.to(device, dtype=torch.float32).eval()
    vae.requires_grad_(False)
    text_encoder.to(device, dtype=weight_dtype).eval()
    unet.to(device, dtype=weight_dtype).eval()
    for m in (text_encoder, unet):
        m.requires_grad_(False)

    if cfg.get("gradient_checkpointing", False):
        unet.enable_gradient_checkpointing()
    # SDPA 的 mem_efficient 后端 backward 走 atomicAdd，同种子重跑不可复现；
    # 关掉走 diffusers 原生 bmm attention（确定性）
    unet.set_use_sdpa(False)

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
        tokenizer=tokenizer,
        text_encoder=text_encoder,
        vae=vae,
        unet=unet,
        noise_scheduler=noise_scheduler,
        resolution=resolution,
    )


def encode_text(bundle: Bundle, captions: list[str], grad_enabled: bool = False) -> torch.Tensor:
    tok = bundle.tokenizer
    input_ids = tok(
        captions, padding="max_length", max_length=tok.model_max_length, truncation=True, return_tensors="pt"
    ).input_ids.to(bundle.device)

    with torch.set_grad_enabled(grad_enabled):
        hidden = bundle.text_encoder(input_ids)[0]
    return hidden


def forward(bundle: Bundle, batch: dict, train_text_encoder: bool = False) -> torch.Tensor:
    device, weight_dtype = bundle.device, bundle.weight_dtype

    with torch.no_grad():
        latents = bundle.vae.encode(batch["images"].to(device, dtype=torch.float32)).latent_dist.sample()
        if torch.any(torch.isnan(latents)):
            latents = torch.nan_to_num(latents, 0)
        latents = (latents * VAE_SCALE_FACTOR).to(weight_dtype)

    encoder_hidden_states = encode_text(bundle, batch["captions"], grad_enabled=train_text_encoder)
    encoder_hidden_states = encoder_hidden_states.to(weight_dtype)

    noise = torch.randn_like(latents)
    timesteps = torch.randint(
        0, bundle.noise_scheduler.config.num_train_timesteps, (latents.shape[0],), device=device
    )
    noisy_latents = bundle.noise_scheduler.add_noise(latents, noise, timesteps).to(weight_dtype)

    noise_pred = bundle.unet(noisy_latents, timesteps, encoder_hidden_states).sample

    loss = F.mse_loss(noise_pred.float(), noise.float(), reduction="mean")
    return loss
