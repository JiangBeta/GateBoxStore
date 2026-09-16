#!/usr/bin/env bash
# 构建单个插件的全部可构建制品到 dist/<id>/（供 CI 与本地使用）。
#
# 用法: scripts/build-plugin.sh <plugin-id>
#   OS/ARCH 可覆盖（默认 linux/amd64）；ui 构建需 pnpm（网络或本地 store）。
set -euo pipefail

ID="${1:?plugin id 必填}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/dist/$ID"
OS="${OS:-linux}"
ARCH="${ARCH:-amd64}"
mkdir -p "$OUT"

# sidecar（Go）
if [ -f "$ROOT/plugins/$ID/backend/go.mod" ]; then
  echo "== 构建 sidecar: $ID =="
  (cd "$ROOT/plugins/$ID/backend" && go build -o "$OUT/$ID-sidecar" ./)
  tar -czf "$OUT/$ID-sidecar-$OS-$ARCH.tar.gz" -C "$OUT" "$ID-sidecar"
  rm -f "$OUT/$ID-sidecar"
fi

# ui（Vite 工程 → dist；或纯静态目录直接打包）
if [ -f "$ROOT/plugins/$ID/ui/package.json" ]; then
  echo "== 构建 ui: $ID =="
  (cd "$ROOT/plugins/$ID/ui" && pnpm install --frozen-lockfile && pnpm build)
  tar -czf "$OUT/$ID-ui.tar.gz" -C "$ROOT/plugins/$ID/ui/dist" .
elif [ -d "$ROOT/plugins/$ID/ui" ]; then
  echo "== 打包静态 ui: $ID =="
  tar -czf "$OUT/$ID-ui.tar.gz" -C "$ROOT/plugins/$ID/ui" .
fi

# assets（静态资源）
if [ -d "$ROOT/plugins/$ID/assets" ]; then
  echo "== 打包 assets: $ID =="
  tar -czf "$OUT/$ID-assets.tar.gz" -C "$ROOT/plugins/$ID/assets" .
fi

echo "== $ID 制品 =="
ls -l "$OUT"
