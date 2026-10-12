"""统一入口：配置文件进、JSONL 出、退出码收尾（engine-process-contract）。

agent 拉起形态：python train.py --config <job_dir>/config.toml
支持 section：[smoke]（契约冒烟）/ [sdxl] / [sd15]（LoRA 训练回路，模型无关）。
"""

from __future__ import annotations

import argparse
import os
import sys
import time
from pathlib import Path

from next_integrated.core import config as cfgmod
from next_integrated.core.contract import EXIT_ERROR, EXIT_OK, Clock, Emitter, Shutdown


def run_lora(cfg: dict, emitter: Emitter, shutdown: Shutdown, job_dir: Path, section: str) -> int:
    import importlib

    import torch

    from next_integrated.core import checkpoint as ckptmod
    from next_integrated.core.dataset import ImageCaptionDataset
    from next_integrated.core.dataset.simple import collate_fn
    from next_integrated.core.network import LoRANetwork
    from next_integrated.core.optimizers import build_optimizer

    adapter = importlib.import_module(f"next_integrated.adapters.{section}")

    sec = cfgmod.section(cfg, section)
    seed = int(sec.get("seed", 42))
    torch.manual_seed(seed)

    resolution = int(sec.get("resolution", 1024 if section == "sdxl" else 512))
    batch_size = int(sec.get("batch_size", 1))
    max_steps = int(sec.get("max_steps", 100))
    lr = float(sec.get("learning_rate", 1e-4))
    optimizer_name = sec.get("optimizer", "adamw")
    network_dim = int(sec.get("network_dim", 8))
    network_alpha = float(sec.get("network_alpha", network_dim))  # 默认 alpha=rank（scale=1）
    train_te = bool(sec.get("train_text_encoder", False))
    max_grad_norm = float(sec.get("max_grad_norm", 1.0))
    output_name = sec.get("output_name", f"{section}-lora")

    out_dir = job_dir / "output"
    out_dir.mkdir(parents=True, exist_ok=True)

    emitter.log("info", f"loading {section} model: {sec['pretrained_model']}")
    bundle = adapter.load({**sec, "resolution": resolution})
    emitter.log("info", "model loaded")

    dataset = ImageCaptionDataset(sec["train_data_dir"], resolution=resolution)
    loader = torch.utils.data.DataLoader(
        dataset, batch_size=batch_size, shuffle=True, num_workers=0, collate_fn=collate_fn
    )
    emitter.log("info", f"dataset: {len(dataset)} samples, batch_size={batch_size}")

    text_encoders = bundle.text_encoders if train_te else None
    network = LoRANetwork(
        text_encoders, bundle.unet, lora_dim=network_dim, alpha=network_alpha, train_text_encoder=train_te
    )
    # 先 apply_to 注册子模块，再 .to()——否则 LoRA 参数留在 CPU
    network.apply_to(apply_text_encoder=train_te)
    network.to(bundle.device, dtype=bundle.weight_dtype)
    network.train()
    emitter.log(
        "info",
        f"LoRA network: {len(network.unet_loras)} unet + {len(network.text_encoder_loras)} te modules, "
        f"dim={network_dim} alpha={network_alpha}",
    )

    optimizer = build_optimizer(
        optimizer_name, network.prepare_optimizer_params(default_lr=lr), lr=lr
    )

    def save(tag: str) -> str:
        path = out_dir / f"{output_name}-{tag}.safetensors"
        ckptmod.save_lora(
            network,
            path,
            dtype=bundle.weight_dtype,
            metadata={"base_model": sec["pretrained_model"], "steps": tag},
        )
        emitter.artifact("checkpoint", f"output/{path.name}")
        return path.name

    def current_lr() -> float:
        if hasattr(optimizer, "get_avg_learning_rate"):
            return float(optimizer.get_avg_learning_rate())
        return float(optimizer.param_groups[0]["lr"])

    clock = Clock()
    step = 0
    interrupted = False
    while step < max_steps:
        for batch in loader:
            if step >= max_steps:
                break
            if shutdown.requested:
                interrupted = True
                break
            loss = adapter.forward(bundle, batch, train_text_encoder=train_te)
            loss.backward()
            if max_grad_norm > 0:
                torch.nn.utils.clip_grad_norm_(network.parameters(), max_grad_norm)
            optimizer.step()
            optimizer.zero_grad(set_to_none=True)
            step += 1
            emitter.progress(
                step, max_steps, loss=round(loss.item(), 6), lr=current_lr(), elapsed_s=clock.elapsed_s
            )
        if interrupted:
            break

    if interrupted:
        name = save(f"last-{step:06d}")
        emitter.log("info", f"SIGTERM received, checkpoint saved at step {step}: {name}, exiting")
        return EXIT_OK

    name = save(f"{max_steps:06d}")
    emitter.log("info", f"training finished: {name}")
    return EXIT_OK



def run_smoke(cfg: dict, emitter: Emitter, shutdown: Shutdown, job_dir: Path) -> int:
    smoke = cfgmod.section(cfg, "smoke")
    steps = int(smoke.get("steps", 100))
    interval = float(smoke.get("interval_s", 0.05))
    out_dir = job_dir / "output"
    out_dir.mkdir(parents=True, exist_ok=True)
    clock = Clock()

    emitter.log("info", f"smoke run: {steps} steps, interval {interval}s")
    loss = 1.0
    step = 0
    while step < steps:
        if shutdown.requested:
            ckpt = out_dir / f"last-{step:06d}.safetensors"
            ckpt.write_bytes(b"smoke-checkpoint\n")
            emitter.artifact("checkpoint", f"output/{ckpt.name}")
            emitter.log("info", f"SIGTERM received, checkpoint saved at step {step}, exiting")
            return EXIT_OK
        time.sleep(interval)
        step += 1
        loss *= 0.99
        emitter.progress(step, steps, loss=round(loss, 6), lr=1e-4, elapsed_s=clock.elapsed_s)

    final = out_dir / "smoke-final.safetensors"
    final.write_bytes(b"smoke-final\n")
    emitter.artifact("checkpoint", f"output/{final.name}")
    emitter.log("info", "smoke run finished")
    return EXIT_OK


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    args = parser.parse_args()

    emitter = Emitter()
    shutdown = Shutdown()
    job_dir = Path(os.environ.get("JOB_DIR", ".")).resolve()

    try:
        cfg = cfgmod.load_config(args.config)
    except Exception as e:
        emitter.error("config", f"failed to load config {args.config}: {e}")
        return EXIT_ERROR

    if "smoke" in cfg:
        return run_smoke(cfg, emitter, shutdown, job_dir)

    for section in ("sdxl", "sd15"):
        if section in cfg:
            try:
                return run_lora(cfg, emitter, shutdown, job_dir, section)
            except Exception as e:
                emitter.error("train", f"{section} run failed: {e}")
                return EXIT_ERROR

    emitter.error("not_implemented", "no runnable training section in config")
    return EXIT_ERROR


if __name__ == "__main__":
    sys.exit(main())
