#!/usr/bin/env bash
# 编译 agent 裸二进制。
# agent 依赖 go-nvml（cgo，运行时 dlopen libnvidia-ml），不能 CGO_ENABLED=0：
#   - linux-arm64：在 aarch64 主机原生构建
#   - linux-x86_64：需要交叉 C 工具链（apt install gcc-x86-64-linux-gnu，
#     或设 X86_CC 指向 zig cc 等）
#   - windows-x86_64：暂不支持——internal/exec 使用 unix-only syscall
#     （Setpgid/syscall.Kill/killTree），待平台拆分后开放
# 用法: scripts/build-agent.sh [版本号] [目标...]   目标默认两个 linux
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VER="dev"
targets=()
for arg in "$@"; do
  case "$arg" in
    windows-*|linux-*) targets+=("$arg") ;;
    *) VER="$arg" ;;
  esac
done
if [[ "$VER" == dev ]]; then
  VER="$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)"
fi
if [[ ${#targets[@]} -eq 0 ]]; then
  targets=(linux-x86_64 linux-arm64)
fi
OUT="$ROOT/dist/release"
mkdir -p "$OUT"
HOST_ARCH="$(uname -m)"

cd "$ROOT/agent"
for target in "${targets[@]}"; do
  out="$OUT/next-trainer-agent-$VER-$target"
  case "$target" in
    linux-arm64)
      if [[ "$HOST_ARCH" != aarch64 && -z "${ARM64_CC:-}" ]]; then
        echo "跳过 $target：非 aarch64 主机，请设 ARM64_CC 交叉工具链" >&2
        continue
      fi
      echo "build agent $target"
      CC="${ARM64_CC:-gcc}" CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -trimpath \
        -ldflags "-s -w -X main.agentVersion=$VER" -o "$out" ./cmd/agent
      ;;
    linux-x86_64)
      if [[ "$HOST_ARCH" == x86_64 ]]; then
        echo "build agent $target (native)"
        CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -trimpath \
          -ldflags "-s -w -X main.agentVersion=$VER" -o "$out" ./cmd/agent
      elif command -v "${X86_CC:-x86_64-linux-gnu-gcc}" >/dev/null; then
        echo "build agent $target (cross)"
        CC="${X86_CC:-x86_64-linux-gnu-gcc}" CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -trimpath \
          -ldflags "-s -w -X main.agentVersion=$VER" -o "$out" ./cmd/agent
      else
        echo "跳过 $target：缺交叉工具链，apt install gcc-x86-64-linux-gnu 或设 X86_CC" >&2
      fi
      ;;
    windows-x86_64)
      echo "跳过 $target：agent windows 支持待 internal/exec 平台拆分（见发布计划缺口）" >&2
      ;;
    *)
      echo "未知目标 $target" >&2
      exit 1
      ;;
  esac
done
