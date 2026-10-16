# shellcheck shell=bash
# Подключается сборочными скриптами. Читает mirrors.local.env, если он есть
# (RN_NO_MIRRORS=1 — не читать), и готовит два массива:
#   MIRROR_BUILD_ARGS  флаги --build-arg для docker build
#   MIRROR_ENV         флаги -e для docker run

MIRROR_BUILD_ARGS=()
MIRROR_ENV=()
_mirrors_file="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/mirrors.local.env"
if [ -f "$_mirrors_file" ] && [ -z "${RN_NO_MIRRORS:-}" ]; then
  # Файл могли править в Windows, отсюда \r.
  while IFS='=' read -r key value; do
    key="${key//[[:space:]]/}"
    value="${value%$'\r'}"
    case "$key" in
      RN_GOPROXY|RN_NPM_REGISTRY|RN_APT_MIRROR|RN_APT_SECURITY_MIRROR) export "$key=$value" ;;
    esac
  done < <(grep -E '^[A-Z_]+=' "$_mirrors_file")
  echo "==> зеркала из mirrors.local.env"
fi
[ -n "${RN_GOPROXY:-}" ] && MIRROR_BUILD_ARGS+=(--build-arg "GOPROXY=$RN_GOPROXY") && MIRROR_ENV+=(-e "GOPROXY=$RN_GOPROXY")
[ -n "${RN_NPM_REGISTRY:-}" ] && MIRROR_BUILD_ARGS+=(--build-arg "NPM_REGISTRY=$RN_NPM_REGISTRY") && MIRROR_ENV+=(-e "NPM_REGISTRY=$RN_NPM_REGISTRY")
[ -n "${RN_APT_MIRROR:-}" ] && MIRROR_BUILD_ARGS+=(--build-arg "APT_MIRROR=$RN_APT_MIRROR") && MIRROR_ENV+=(-e "RN_APT_MIRROR=$RN_APT_MIRROR")
[ -n "${RN_APT_SECURITY_MIRROR:-}" ] && MIRROR_BUILD_ARGS+=(--build-arg "APT_SECURITY_MIRROR=$RN_APT_SECURITY_MIRROR") && MIRROR_ENV+=(-e "RN_APT_SECURITY_MIRROR=$RN_APT_SECURITY_MIRROR")
true
