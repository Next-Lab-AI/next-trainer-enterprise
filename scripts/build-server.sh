#!/usr/bin/env bash
# 交叉编译 server 三平台裸二进制（CGO_ENABLED=0，modernc.org/sqlite 纯 Go）。
# 用法: scripts/build-server.sh [版本号]   默认 git describe --tags --always --dirty
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VER="${1:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="$ROOT/dist/release"
mkdir -p "$OUT"

if [[ ! -d "$ROOT/server/web/dist" ]]; then
  echo "server/web/dist 不存在，先跑 scripts/build-web.sh" >&2
  exit 1
fi

cd "$ROOT/server"
for target in windows-x86_64 linux-x86_64 linux-arm64; do
  goos="${target%%-*}"
  case "${target##*-}" in
    x86_64) goarch=amd64 ;;
    arm64)  goarch=arm64 ;;
  esac
  ext=""
  [[ "$goos" == windows ]] && ext=".exe"
  out="$OUT/next-trainer-server-$VER-$target$ext"
  echo "build server $target"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags "-s -w -X main.version=$VER" -o "$out" ./cmd/server
done
