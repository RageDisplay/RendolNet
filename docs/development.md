# Разработка

Нужны Go 1.26, Node 22 и Docker. Всё, что требует Linux (eBPF, netlink),
запускается в контейнерах, поэтому разрабатывать можно и на Windows.

## Тесты

| Команда | Что проверяет | Где |
|---|---|---|
| `make test` | gofmt, vet, модульные тесты | на хосте |
| `make test-bpf` | eBPF-программы через BPF_PROG_TEST_RUN | привилегированный контейнер |
| `make test-api` | API, хранилище, агент на настоящем Postgres | контейнеры |
| `make lint` | golangci-lint с `build/golangci.yml` | на хосте |
| `cd web && npm run build` | типы TypeScript, сборка интерфейса | на хосте |

Docker Desktop с WSL2 даёт ядро 6.6, этого хватает для `test-bpf`.

## Стенд в Docker

```
make dev        # https://localhost:8443, admin / admin
make dev-down
```

`deploy/dev/compose.yaml`: Postgres, агент в привилегированном контейнере и
API. У агента два dummy-интерфейса, `wan0` и `lan0`; `eth0` контейнера служит
управлением. Стартовый конфиг задаётся через `RN_SEED`. Трафик по такому стенду
не ходит, он нужен для интерфейса и API.

Интерфейс с горячей перезагрузкой: поднимите стенд, затем

```
cd web
RN_API=https://localhost:8443 npm run dev
```

## Стенд в minikube

```
make minikube VERSION=0.1.0-dev
kubectl -n rendolnet port-forward deploy/api 9443:443
```

Ставятся те же манифесты, что на узел, плюс `deploy/k8s/minikube.yaml` —
пароль базы и стартовый конфиг на dummy-интерфейсах ноды. minikube с драйвером
docker работает на ядре хоста, eBPF там настоящий.

minikube кеширует образы по тегу. При пересборке с тем же тегом выполните
`minikube image rm` или поменяйте `VERSION`.

## eBPF

После правки `bpf/rendol.c` или `bpf/rendol.h`:

```
make bpf        # bpf2go в контейнере с clang, обновит internal/dataplane/ebpf/*
make test-bpf
```

CI перегенерирует обвязку и упадёт, если она не совпала с той, что в репозитории.

Новые поля в структурах карт должны совпадать по выравниванию с Go:
bpf2go генерирует типы из BTF, вручную их писать не нужно.

## Стиль

- Комментарии — по-русски и только там, где неочевидно, почему код такой.
- Ошибки оборачиваются с префиксом пакета: `fmt.Errorf("dataplane: …: %w", err)`.
- Строки интерфейса — в `web/src/i18n/ru.ts` и `en.ts`. Английский словарь
  типизирован ключами русского, поэтому пропущенный перевод не скомпилируется.
- HTTP — стандартный `net/http` с шаблонами методов, без роутеров.
