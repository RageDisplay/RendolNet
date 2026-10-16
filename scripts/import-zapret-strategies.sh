#!/usr/bin/env bash
# import-zapret-strategies.sh [тег]
# Скачивает релиз Flowseal/zapret-discord-youtube и пересобирает
# internal/zapret/strategies.json. Тег по умолчанию — тот же, что в Dockerfile.
set -euo pipefail
cd "$(dirname "$0")/.."
REF="${1:-$(sed -n 's/^ARG FLOWSEAL_REF=//p' deploy/Dockerfile)}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
curl -fsSL -o "$TMP/src.zip" "https://github.com/Flowseal/zapret-discord-youtube/archive/refs/tags/$REF.zip"
(cd "$TMP" && unzip -q src.zip)
go run ./tools/zapret-import "$TMP"/zapret-discord-youtube-*/ internal/zapret/strategies.json
echo "==> internal/zapret/strategies.json ($REF)"
