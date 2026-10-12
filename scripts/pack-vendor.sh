#!/usr/bin/env bash
# vendor/ 全家桶 -> dist/release/vendor-engines-<ver>.zip（纯源码，平台无关）。
# 包结构：
#   <engine>/...        引擎源码快照（剔除 __pycache__/.git/.venv 等）
#   manifest.json       {"version","engines":{name:{"sha256","dependency_files"}}}
# manifest 的树哈希算法（agent 侧用 Go 复算校验，必须保持一致）：
#   对引擎目录下所有文件按相对路径排序，依次向 sha256 写入
#   "<相对路径>\0" + 文件内容。
# 用法: scripts/pack-vendor.sh <版本号>
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VER="${1:?用法: pack-vendor.sh <版本号>}"
OUT="$ROOT/dist/release"
mkdir -p "$OUT"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
STAGE="$WORK/vendor-engines"
mkdir -p "$STAGE"

cd "$ROOT/vendor"
for d in */; do
  name="${d%/}"
  tar --exclude='__pycache__' --exclude='*.pyc' --exclude='.git' \
      --exclude='.venv' --exclude='venv' --exclude='node_modules' \
      --exclude='.mypy_cache' --exclude='.pytest_cache' \
      -cf - "$name" | tar -xf - -C "$STAGE"
done

python3 - "$STAGE" "$VER" <<'PY'
import hashlib, json, os, sys

root, ver = sys.argv[1], sys.argv[2]

def tree_hash(d):
    h = hashlib.sha256()
    paths = []
    for dirpath, dirnames, filenames in os.walk(d):
        dirnames.sort()
        for f in filenames:
            paths.append(os.path.relpath(os.path.join(dirpath, f), d))
    for rel in sorted(paths):
        h.update(rel.encode())
        h.update(b"\0")
        with open(os.path.join(d, rel), "rb") as fp:
            while True:
                chunk = fp.read(1 << 20)
                if not chunk:
                    break
                h.update(chunk)
    return h.hexdigest()

engines = {}
for name in sorted(os.listdir(root)):
    d = os.path.join(root, name)
    if not os.path.isdir(d):
        continue
    entry = {"sha256": tree_hash(d)}
    dep = [c for c in ("requirements.txt", "requirements_base.txt",
                       "constraints.txt", "pyproject.toml", "setup.py")
           if os.path.exists(os.path.join(d, c))]
    if dep:
        entry["dependency_files"] = dep
    engines[name] = entry

with open(os.path.join(root, "manifest.json"), "w", encoding="utf-8") as fp:
    json.dump({"version": ver, "engines": engines}, fp,
              indent=2, ensure_ascii=False)
    fp.write("\n")
PY

cd "$STAGE"
rm -f "$OUT/vendor-engines-$VER.zip"
zip -q -r -X "$OUT/vendor-engines-$VER.zip" .
echo "OK $OUT/vendor-engines-$VER.zip"
