#!/usr/bin/env bash
set -euo pipefail

log() { printf '{"type":"log","level":"%s","msg":"%s"}\n' "$1" "$2"; }
err() { printf '{"type":"error","code":"%s","msg":"%s"}\n' "$1" "$2"; }

PY="$VENV_PATH/bin/python"
[ -x "$PY" ] || { err preflight "venv python missing: $PY"; exit 1; }

if [ -n "${JOB_DIR:-}" ]; then
    [ -d "$JOB_DIR" ] && [ -w "$JOB_DIR" ] || { err preflight "JOB_DIR not writable: $JOB_DIR"; exit 1; }
fi

"$PY" -c "import torch; assert torch.cuda.is_available(), 'cuda unavailable'" \
    || { err preflight "torch.cuda unavailable"; exit 1; }

log info "preflight ok"
