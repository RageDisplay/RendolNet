#!/usr/bin/env bash
# build-bundle.sh <версия> [--out DIR] [--use-loaded BASE]
#
# Собирает пакет обновления dist/rendolnet-<версия>.rnu: образы, манифесты,
# бинари узла, SHA256SUMS. Тот же пакет кладётся в ISO.
#   --use-loaded BASE  образы уже загружены (CI) как BASE/<компонент>:<версия>
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck source=scripts/lib-mirrors.sh
. scripts/lib-mirrors.sh

VERSION="${1:?версия}"; shift
OUT="dist"
LOADED=""
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT="$2"; shift ;;
    --use-loaded) LOADED="$2"; shift ;;
    *) echo "неизвестный флаг $1" >&2; exit 2 ;;
  esac
  shift
done

PG_IMAGE="postgres:17-alpine"
ALPINE_IMAGE="${ALPINE_IMAGE:-alpine:3.21}"
WORK="$OUT/$VERSION/bundle"
rm -rf "$WORK"
mkdir -p "$WORK/manifests" "$WORK/host/usr/local/sbin"

COMPONENTS="api agent zapret"
IMAGES=()
for c in $COMPONENTS; do
  if [ -n "$LOADED" ]; then
    docker tag "$LOADED/$c:$VERSION" "rendolnet/$c:$VERSION"
  else
    echo "==> образ $c"
    docker build -q "${MIRROR_BUILD_ARGS[@]}" --build-arg VERSION="$VERSION" \
      -f deploy/Dockerfile --target "$c" -t "rendolnet/$c:$VERSION" . >/dev/null
  fi
  IMAGES+=("rendolnet/$c:$VERSION")
done
docker image inspect "$PG_IMAGE" >/dev/null 2>&1 || docker pull -q "$PG_IMAGE" >/dev/null
IMAGES+=("$PG_IMAGE")

echo "==> бинари узла"
docker build -q "${MIRROR_BUILD_ARGS[@]}" --build-arg VERSION="$VERSION" \
  -f deploy/Dockerfile --target host --output "type=local,dest=$WORK/host/usr/local/sbin" . >/dev/null

echo "==> образы → images.tar.zst"
# zstd берётся из контейнера: на Windows-хосте его обычно нет
docker save "${IMAGES[@]}" | docker run -i --rm "$ALPINE_IMAGE" \
  sh -c 'apk add -q zstd >/dev/null && zstd -T0 -12 -q -c' > "$WORK/images.tar.zst"

bash scripts/render-manifests.sh "$VERSION" > "$WORK/manifests/rendolnet.yaml"

# юниты узла едут вместе с бинарями: обновление их тоже доставляет
mkdir -p "$WORK/host/etc/systemd/system"
for u in rendol-updater.path rendol-updater.service rendol-updater.timer rendol-console.service          getty@tty2.service.d autovt@tty2.service.d; do
  cp -r "iso/rootfs/etc/systemd/system/$u" "$WORK/host/etc/systemd/system/"
done

printf '{\n  "product": "rendolnet",\n  "version": "%s",\n  "created": "%s",\n  "k3s": "%s",\n  "images": [%s]\n}\n' \
  "$VERSION" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${K3S_VERSION:-}" \
  "$(printf '"%s",' "${IMAGES[@]}" | sed 's/,$//')" > "$WORK/manifest.json"

( cd "$WORK" && find . -type f | sed 's#^\./##' | sort | xargs sha256sum > ../SHA256SUMS && mv ../SHA256SUMS SHA256SUMS )
# На Windows у файлов нет бита исполнения, поэтому права задаются явно:
# бинари узла 0755, остальное 0644.
tar -C "$WORK" --mode=u=rwX,go=rX --exclude=./host/usr/local/sbin -cf "$OUT/rendolnet-$VERSION.rnu" .
tar -C "$WORK" --mode=0755 -rf "$OUT/rendolnet-$VERSION.rnu" ./host/usr/local/sbin
( cd "$OUT" && sha256sum "rendolnet-$VERSION.rnu" > "rendolnet-$VERSION.rnu.sha256" )
ls -lh "$OUT/rendolnet-$VERSION.rnu"
