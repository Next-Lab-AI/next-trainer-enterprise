"""TOML 直解配置加载：无 argparse 转换层（契约：配置文件进）。"""

from __future__ import annotations

import tomllib
from pathlib import Path
from typing import Any


def load_config(path: str | Path) -> dict[str, Any]:
    with open(path, "rb") as f:
        cfg = tomllib.load(f)
    if not isinstance(cfg, dict):
        raise ValueError(f"config root must be a table: {path}")
    return cfg


def section(cfg: dict[str, Any], name: str) -> dict[str, Any]:
    sec = cfg.get(name, {})
    if not isinstance(sec, dict):
        raise ValueError(f"config section [{name}] must be a table")
    return sec
