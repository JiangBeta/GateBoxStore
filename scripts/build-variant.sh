#!/usr/bin/env bash
# 构建一个核心组件配方变体（ADR-038）。
#
# 用法: scripts/build-variant.sh <version> <os> <arch> <feature1,feature2,...>
# 例:   scripts/build-variant.sh 2.10.0 linux amd64 github.com/mholt/caddy-l4
#
# 依赖: xcaddy（`go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest`）、tar、sha256sum。
# 产物: dist/caddy-<version>-<key>.tar.gz，并打印可回填 variants.yaml 的 url/sha256/key。
set -euo pipefail

VERSION="${1:?version 必填}"
OS="${2:?os 必填}"
ARCH="${3:?arch 必填}"
FEATURES="${4:-}"

cd "$(dirname "$0")/.."
mkdir -p dist

# 组装 xcaddy 的 --with 参数（逗号分隔）
WITH_ARGS=()
if [ -n "$FEATURES" ]; then
  IFS=',' read -r -a arr <<< "$FEATURES"
  for f in "${arr[@]}"; do
    [ -n "$f" ] && WITH_ARGS+=(--with "$f")
  done
fi

echo "构建 caddy v$VERSION ($OS/$ARCH) 特征=[$FEATURES]"
GOOS="$OS" GOARCH="$ARCH" xcaddy build "v$VERSION" "${WITH_ARGS[@]}" --output dist/caddy

KEY=$(go run ./cmd/gbx-store variant-key -component caddy -version "$VERSION" -os "$OS" -arch "$ARCH" -features "$FEATURES")
ARCHIVE="dist/caddy-${VERSION}-${KEY#sha256:}-${OS}-${ARCH}.tar.gz"
tar -czf "$ARCHIVE" -C dist caddy
SHA=$(sha256sum "$ARCHIVE" | awk '{print $1}')
SIZE=$(stat -c%s "$ARCHIVE")

cat <<EOF

构建完成。回填 variants.yaml：
  - component: caddy
    version: "$VERSION"
    features: [${FEATURES}]
    os: $OS
    arch: $ARCH
    url: <GitHub Release 制品 URL>
    sha256: "$SHA"
    size: $SIZE
key: $KEY
制品: $ARCHIVE
EOF
