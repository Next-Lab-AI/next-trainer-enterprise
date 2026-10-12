"""统一入口：配置文件进、JSONL 出、退出码收尾（engine-process-contract）。

agent 拉起形态：python train.py --config <job_dir>/config.toml
本期（Phase 1 Step 1）只有契约冒烟回路；真实训练随 SDXL adapter 落地。
"""

from __future__ import annotations

import argparse
import os
import sys
import time
from pathlib import Path

from next_integrated.core import config as cfgmod
from next_integrated.core.contract import EXIT_ERROR, EXIT_OK, Clock, Emitter, Shutdown


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

    emitter.error("not_implemented", "no runnable training section in config (SDXL adapter lands in Phase 1 Step 3)")
    return EXIT_ERROR


if __name__ == "__main__":
    sys.exit(main())
