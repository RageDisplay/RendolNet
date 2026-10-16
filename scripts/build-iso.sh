#!/usr/bin/env bash
# build-iso.sh <версия> [--bundle FILE.rnu] [--out DIR]
#
# Пакет (или готовый --bundle), k3s с образами, затем ISO в контейнере
# iso/Dockerfile.builder. Зеркала берутся из mirrors.local.env.
#   K3S_VERSION  версия k3s (по умолчанию ниже)
#   K3S_URL      откуда качать релизы k3s, например raw-прокси Nexus на GitHub
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck source=scripts/lib-mirrors.sh
. scripts/lib-mirrors.sh

VERSION=""; BUNDLE=""; COPY_TO=""
while [ $# -gt 0 ]; do
  case "$1" in
    --bundle) BUNDLE="$2"; shift ;;
    --out) COPY_TO="$2"; shift ;;
    -*) echo "неизвестный флаг $1" >&2; exit 2 ;;
    *) VERSION="$1" ;;
  esac
  shift
done
VERSION="${VERSION:-dev}"
export K3S_VERSION="${K3S_VERSION:-v1.36.5+k3s1}"
K3S_URL="${K3S_URL:-https://github.com/k3s-io/k3s/releases/download}"
OUT=".tmp/iso"
mkdir -p "$OUT/k3s" "$OUT/cache"

if [ -z "$BUNDLE" ]; then
  bash scripts/build-bundle.sh "$VERSION" --out "$OUT/dist"
  BUNDLE="$OUT/dist/rendolnet-$VERSION.rnu"
fi
echo "==> пакет $BUNDLE"
rm -rf "$OUT/bundle" && mkdir -p "$OUT/bundle"
tar -C "$OUT/bundle" -xf "$BUNDLE"
( cd "$OUT/bundle" && sha256sum -c --quiet SHA256SUMS )

echo "==> k3s $K3S_VERSION"
enc="${K3S_VERSION//+/%2B}"
stamp="$OUT/k3s/.version"
if [ "$(cat "$stamp" 2>/dev/null)" != "$K3S_VERSION" ]; then
  for f in k3s k3s-airgap-images-amd64.tar.zst sha256sum-amd64.txt; do
    curl -fsSL --retry 3 -o "$OUT/k3s/$f" "$K3S_URL/$enc/$f"
  done
  ( cd "$OUT/k3s" && grep -E ' (k3s|k3s-airgap-images-amd64.tar.zst)$' sha256sum-amd64.txt | sha256sum -c --quiet )
  chmod +x "$OUT/k3s/k3s"
  echo "$K3S_VERSION" > "$stamp"
fi

echo "==> образ сборщика"
docker build -q "${MIRROR_BUILD_ARGS[@]}" -f iso/Dockerfile.builder -t rendolnet-iso-builder iso >/dev/null

echo "==> ISO (10–30 минут)"
MSYS_NO_PATHCONV=1 docker run --rm --cap-add SYS_ADMIN \
  --security-opt seccomp=unconfined --security-opt apparmor=unconfined \
  "${MIRROR_ENV[@]}" \
  -v "$PWD:/src:ro" -v "$PWD/$OUT:/out" -v rn-iso-aptcache:/cache \
  rendolnet-iso-builder "$VERSION"

ISO="rendolnet-$VERSION-amd64.iso"
if [ -n "$COPY_TO" ]; then
  mkdir -p "$COPY_TO"
  cp "$OUT/$ISO" "$OUT/$ISO.sha256" "$COPY_TO/"
fi
echo "==> $OUT/$ISO"
