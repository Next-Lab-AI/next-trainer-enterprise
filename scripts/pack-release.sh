#!/usr/bin/env bash
# 汇总发布产物到 dist/release/：
#   build-web（dist 缺失时）-> build-server -> build-agent -> pack-vendor
#   -> SHA256SUMS.txt + RELEASE-NOTES.md（首次生成模板，之后手动维护不覆盖）
# 用法: scripts/pack-release.sh <版本号>   例: scripts/pack-release.sh 0.1.0
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VER="${1:?用法: pack-release.sh <版本号>}"
OUT="$ROOT/dist/release"

if [[ ! -d "$ROOT/server/web/dist" ]]; then
  "$ROOT/scripts/build-web.sh"
fi
"$ROOT/scripts/build-server.sh" "$VER"
"$ROOT/scripts/build-agent.sh" "$VER"
"$ROOT/scripts/pack-vendor.sh" "$VER"

cd "$OUT"
rm -f SHA256SUMS.txt
sha256sum next-trainer-server-"$VER"-* next-trainer-agent-"$VER"-* \
  vendor-engines-"$VER".zip > SHA256SUMS.txt 2>/dev/null || {
  echo "SHA256SUMS 生成失败：$VER 的产物不全？" >&2
  exit 1
}

if [[ ! -f RELEASE-NOTES.md ]]; then
  cat > RELEASE-NOTES.md <<EOF
# next-trainer $VER

## 发布物

| 文件 | 用途 |
|---|---|
| next-trainer-server-$VER-windows-x86_64.exe | 控制面（Windows） |
| next-trainer-server-$VER-linux-x86_64 | 控制面（Linux x86） |
| next-trainer-server-$VER-linux-arm64 | 控制面（Linux ARM） |
| next-trainer-agent-$VER-linux-x86_64 | GPU 节点（Linux x86） |
| next-trainer-agent-$VER-linux-arm64 | GPU 节点（Linux ARM） |
| vendor-engines-$VER.zip | 训练引擎源码全家桶，放 agent 旁 \`engines/\` 目录 |

agent 的 Windows 版本暂不支持（internal/exec 平台拆分中）。

## agent 节点依赖

- **uv**：官方安装见 https://docs.astral.sh/uv/getting-started/installation/
  （也可 \`pip install uv\`），下载后放到 agent 旁 \`tools/\` 目录。
- 离线/网闸环境：把 uv 的 pip index、pytorch index、HF endpoint
  全部指向内网镜像。

## 校验

\`\`\`sh
sha256sum -c SHA256SUMS.txt
\`\`\`
EOF
fi

echo "--- $OUT"
ls -lh "$OUT"
