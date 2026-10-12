#!/usr/bin/env bash
set -euo pipefail

log() { printf '{"type":"log","level":"%s","msg":"%s"}\n' "$1" "$2"; }

PY="$VENV_PATH/bin/python"
if [ ! -x "$PY" ]; then
    printf '{"type":"audit","ok":false,"errors":["venv python missing: %s"]}\n' "$PY"
    exit 1
fi

"$PY" - <<'EOF'
import json
import sys

facts = {"python": sys.version.split()[0]}
errors = []

try:
    import torch

    facts["torch"] = torch.__version__
    facts["cuda_available"] = torch.cuda.is_available()
    facts["cuda_version"] = torch.version.cuda
    if torch.cuda.is_available():
        facts["gpu"] = torch.cuda.get_device_name(0)
        cap = torch.cuda.get_device_capability(0)
        facts["sm"] = f"{cap[0]}{cap[1]}"
        if cap < (8, 0):
            errors.append(f"GPU sm_{facts['sm']} below Ampere floor (bf16 无原生支持)")
    else:
        errors.append("torch.cuda not available")
except Exception as e:
    errors.append(f"import torch failed: {e}")

for mod in ("transformers", "diffusers", "accelerate", "safetensors"):
    try:
        m = __import__(mod)
        facts[mod] = getattr(m, "__version__", "?")
    except Exception as e:
        errors.append(f"import {mod} failed: {e}")

print(json.dumps({"type": "audit", "ok": not errors, "facts": facts, "errors": errors}, ensure_ascii=True))
sys.exit(0 if not errors else 1)
EOF
