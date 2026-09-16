#!/usr/bin/env bash
# 构建单个插件的全部可构建制品到 dist/<id>/（供 CI 与本地使用）。
#
# 用法: scripts/build-plugin.sh <plugin-id>
#   OS/ARCH 可覆盖（默认 linux/amd64）；ui 与架构无关，多架构时用 SKIP_UI=1 只构建一次。
#   打包使用确定性 tar（固定 mtime/属主/排序），保证同输入产出同 sha256。
set -euo pipefail

ID="${1:?plugin id 必填}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/dist/$ID"
OS="${OS:-linux}"
ARCH="${ARCH:-amd64}"
mkdir -p "$OUT"

# 确定性打包：固定时间/属主/排序，产出可复现。
tar_det() { # $1=输出 $2=工作目录 $3..=条目
  local out="$1" dir="$2"; shift 2
  tar --sort=name --mtime='UTC 2020-01-01' --owner=0 --group=0 --numeric-owner \
    -czf "$out" -C "$dir" "$@"
}

# sidecar（Go；交叉编译需 CGO_ENABLED=0）
if [ -f "$ROOT/plugins/$ID/backend/go.mod" ]; then
  echo "== 构建 sidecar: $ID ($OS/$ARCH) =="
  (cd "$ROOT/plugins/$ID/backend" && CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" go build -o "$OUT/$ID-sidecar" ./)
  tar_det "$OUT/$ID-sidecar-$OS-$ARCH.tar.gz" "$OUT" "$ID-sidecar"
  rm -f "$OUT/$ID-sidecar"
fi

# ui（Vite 工程 → dist；或纯静态目录直接打包）
if [ "${SKIP_UI:-0}" = "1" ]; then
  echo "== 跳过 ui（SKIP_UI=1）=="
elif [ -f "$ROOT/plugins/$ID/ui/package.json" ]; then
  echo "== 构建 ui: $ID =="
  (cd "$ROOT/plugins/$ID/ui" && pnpm install --frozen-lockfile && pnpm build)
  tar_det "$OUT/$ID-ui.tar.gz" "$ROOT/plugins/$ID/ui/dist"
elif [ -d "$ROOT/plugins/$ID/ui" ]; then
  echo "== 打包静态 ui: $ID =="
  tar_det "$OUT/$ID-ui.tar.gz" "$ROOT/plugins/$ID/ui"
fi

# assets（静态资源）
if [ -d "$ROOT/plugins/$ID/assets" ]; then
  echo "== 打包 assets: $ID =="
  tar_det "$OUT/$ID-assets.tar.gz" "$ROOT/plugins/$ID/assets"
fi

echo "== $ID 制品 =="
ls -l "$OUT"
