"""最小可用数据集（Phase 1 smoke 级）：图片目录 + 同名 .txt caption。

预处理对齐 kohya 语义的中心裁剪路径：最大方形中心裁剪 → LANCZOS 缩放
到固定分辨率 → 随机水平翻转 → [-1, 1]。bucketing/latent 缓存等完整拆解
随第二个模型吸收时做（见 docs/design/next-integrated.md）。
"""

from __future__ import annotations

from pathlib import Path

import numpy as np
import torch
from PIL import Image

IMAGE_EXTS = {".png", ".jpg", ".jpeg", ".webp", ".bmp"}


class ImageCaptionDataset(torch.utils.data.Dataset):
    def __init__(self, data_dir: str | Path, resolution: int = 1024, flip_aug: bool = True):
        self.data_dir = Path(data_dir)
        self.resolution = resolution
        self.flip_aug = flip_aug

        self.samples = []
        for p in sorted(self.data_dir.rglob("*")):
            if p.suffix.lower() not in IMAGE_EXTS:
                continue
            caption_file = p.with_suffix(".txt")
            if not caption_file.exists():
                continue
            self.samples.append((p, caption_file))
        if not self.samples:
            raise ValueError(f"no image/.txt caption pairs found under {self.data_dir}")

    def __len__(self):
        return len(self.samples)

    def __getitem__(self, idx):
        img_path, caption_file = self.samples[idx]
        caption = caption_file.read_text(encoding="utf-8").strip()

        img = Image.open(img_path).convert("RGB")
        w, h = img.size
        side = min(w, h)
        left = (w - side) // 2
        top = (h - side) // 2
        img = img.crop((left, top, left + side, top + side))
        img = img.resize((self.resolution, self.resolution), Image.LANCZOS)

        arr = np.array(img, dtype=np.float32) / 127.5 - 1.0
        tensor = torch.from_numpy(arr).permute(2, 0, 1)
        if self.flip_aug and torch.rand(()) < 0.5:
            tensor = torch.flip(tensor, dims=[2])

        return {
            "images": tensor,
            "captions": caption,
            # SDXL size conditioning：与种子 collate 语义一致（h, w）/（y, x）
            "original_sizes_hw": (h, w),
            "crop_top_lefts": (top, left),
            "target_sizes_hw": (self.resolution, self.resolution),
        }


def collate_fn(examples):
    return {
        "images": torch.stack([e["images"] for e in examples]),
        "captions": [e["captions"] for e in examples],
        "original_sizes_hw": torch.tensor([e["original_sizes_hw"] for e in examples]),
        "crop_top_lefts": torch.tensor([e["crop_top_lefts"] for e in examples]),
        "target_sizes_hw": torch.tensor([e["target_sizes_hw"] for e in examples]),
    }
