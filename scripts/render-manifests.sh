#!/usr/bin/env bash
# render-manifests.sh <версия> [префикс-реестра] > rendolnet.yaml
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${1:?версия}"
IMAGES="${2:-}"
[ -z "$IMAGES" ] || IMAGES="${IMAGES%/}/"
sed -e "s#\${VERSION}#${VERSION}#g" -e "s#\${IMAGES}#${IMAGES}#g" deploy/k8s/rendolnet.yaml
