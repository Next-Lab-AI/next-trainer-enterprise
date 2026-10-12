#!/usr/bin/env bash
set -euo pipefail

log() { printf '{"type":"log","level":"%s","msg":"%s"}\n' "$1" "$2"; }
progress() { printf '{"type":"progress","stage":"%s","pct":%d}\n' "$1" "$2"; }
err() { printf '{"type":"error","code":"%s","msg":"%s"}\n' "$1" "$2"; }

UV="${UV_PATH:-uv}"
PYVER="${PYTHON_VERSION:-3.13}"

progress "venv" 5
log info "creating venv at $VENV_PATH (python $PYVER)"
"$UV" venv "$VENV_PATH" --python "$PYVER" || { err install "uv venv failed"; exit 1; }

progress "deps" 40
log info "installing dependencies via uv sync (pyproject.toml)"
cd "$ENGINE_ROOT"
if UV_PROJECT_ENVIRONMENT="$VENV_PATH" "$UV" sync --frozen --python "$VENV_PATH/bin/python" 2>/dev/null; then
    :
else
    log info "no usable lockfile, falling back to unlocked uv sync"
    UV_PROJECT_ENVIRONMENT="$VENV_PATH" "$UV" sync --python "$VENV_PATH/bin/python" || { err install "uv sync failed"; exit 1; }
fi

progress "flash-attn" 80
# flash-attn 预编译 wheel（mjun0812，GitHub releases）按架构可选安装；
# GitHub 直连 URL 不进 pyproject（会阻塞 uv lock），装不上退 SDPA，不阻塞。
FA_BASE="https://github.com/mjun0812/flash-attention-prebuild-wheels/releases/download"
case "$(uname -m)" in
    x86_64)  FA_URL="$FA_BASE/v0.9.17/flash_attn-2.8.3+cu132torch2.12-cp313-cp313-linux_x86_64.whl" ;;
    aarch64) FA_URL="$FA_BASE/v0.9.22/flash_attn-2.8.3+cu132torch2.12-cp313-cp313-linux_aarch64.whl" ;;
    *)       FA_URL="" ;;
esac
if [ -n "$FA_URL" ]; then
    log info "installing flash-attn prebuilt wheel ($(uname -m))"
    if ! "$UV" pip install --no-deps --python "$VENV_PATH/bin/python" "$FA_URL"; then
        log warn "flash-attn install failed, training will fall back to SDPA"
    fi
else
    log warn "no prebuilt flash-attn wheel for $(uname -m), training will fall back to SDPA"
fi

progress "done" 100
log info "install complete"
