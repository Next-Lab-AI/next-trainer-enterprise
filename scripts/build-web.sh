#!/usr/bin/env bash
# 构建前端，产出 server/web/dist（server 通过 go:embed 打包进二进制）。
set -euo pipefail

cd "$(dirname "$0")/../server/web"
npm ci
npm run build
