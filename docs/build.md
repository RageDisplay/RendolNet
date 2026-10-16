# Сборка

## Зеркала

Всё, что качается при сборке (модули Go, пакеты npm, пакеты Debian), может
идти через Nexus. Скопируйте `mirrors.example.env` в `mirrors.local.env`
(в git не попадает):

```
RN_GOPROXY=http://nexus.lan/repository/Golang-Registry/
RN_NPM_REGISTRY=http://nexus.lan/repository/npm-proxy/
RN_APT_MIRROR=http://nexus.lan/repository/debian-proxy
RN_APT_SECURITY_MIRROR=http://nexus.lan/repository/debian-security-proxy
```

`scripts/lib-mirrors.sh` читает файл и передаёт значения в `docker build` и
сборщик ISO. `RN_NO_MIRRORS=1` — собрать без зеркал, не трогая файл.

От зеркала не зависит целостность: модули Go проверяются по `go.sum`, npm — по
`package-lock.json`, пакеты Debian — по подписям Debian. На установленной
системе в apt всегда прописаны публичные источники Debian, каким бы ни было
зеркало сборки.

k3s и релиз Flowseal качаются с GitHub. Чтобы брать их через Nexus, заведите
raw-прокси на `https://github.com` и задайте `K3S_URL` для `build-iso.sh`.

## Образы

Один `deploy/Dockerfile`, цель выбирается `--target`:

| Цель | Основа | Содержимое |
|---|---|---|
| `api` | distroless static | rendol-api и собранный веб-интерфейс |
| `agent` | distroless static | rendol-agent |
| `zapret` | debian:trixie-slim | nfqws, nftables, файлы Flowseal, rendol-agent |
| `host` | scratch | бинари для узла, забираются через `--output` |
| `agent-dev` | debian:trixie-slim | агент с iproute2 для dev-стенда |

```
make images VERSION=0.2.0
```

## Пакет и ISO

```
make bundle VERSION=0.2.0     # dist/rendolnet-0.2.0.rnu
make iso VERSION=0.2.0        # .tmp/iso/rendolnet-0.2.0-amd64.iso
```

`build-iso.sh` собирает пакет (или берёт готовый через `--bundle`), качает k3s
и его образы для офлайн-установки (`K3S_VERSION`, по умолчанию v1.36.5+k3s1;
суммы сверяются с `sha256sum-amd64.txt` релиза), затем запускает
`iso/Dockerfile.builder`.

Сборщик без `--privileged` и без устройств. CAP_SYS_ADMIN нужен mmdebstrap,
чтобы смонтировать /proc и /sys в chroot. mmdebstrap кладёт Debian 13 сразу в
squashfs, `iso/mkiso.sh` собирает гибридный образ через xorriso. Пакеты .deb
кешируются в томе `rn-iso-aptcache`, повторная сборка их не качает.

ISO собирается 10–30 минут и весит около 700 МБ: примерно треть — образы k3s
и RendolNet, остальное — Debian с ядром и прошивками сетевых карт.

## Номер версии

В CI версия берётся из ближайшего тега `vX.Y.Z`:

- на коммите с тегом — `1.2.0`;
- через пять коммитов после тега — `1.2.0-5-gabc1234`;
- без тегов — `0.0.0-N-g<hash>`.

Версия попадает в бинари через `-X main.version`, в образы — тегом, в пакет —
в `manifest.json`.

## CI

`.gitlab-ci.yml` устроен так же, как в VectrixCodeKeeper: исходники с GitHub,
сборка в GitLab, проверка vck, публикация в реестр VectrixCloud.

| Стадия | Задания |
|---|---|
| fetch | `fetch:source` — клон, вычисление версии |
| test | `test:go` (gofmt, tidy, `go test -race`), `test:api` (с Postgres), `test:bpf` (eBPF в ядре раннера, сверка сгенерированной обвязки), `test:lint`, `test:web`, `test:shell`, `test:manifests` |
| build | `build:api`, `build:agent`, `build:zapret` — образы в артефакты |
| scan | `scan:sast`, `scan:sca` — vck с Quality Gate |
| publish | `publish:images` — skopeo в `cloud.vectrix.sec:5000/rendolnet` |
| package | `package:bundle` — пакет обновления из проверенных образов |

Переменные проекта: `GITHUB_TOKEN`, `VCK_URL`, `VCK_TOKEN`, `VC_REGISTRY_USER`,
`VC_REGISTRY_PASSWORD`, `VC_REGISTRY_CA` (File), `RN_GOPROXY`,
`RN_NPM_REGISTRY`, `RN_APT_MIRROR`.

`test:bpf` и задания сборки используют docker:dind, им нужен раннер с
`privileged = true`. Для `test:bpf` у ядра раннера должна быть версия 6.6 и новее.
