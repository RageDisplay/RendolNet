# RendolNet

Межсетевой экран и маршрутизатор на Linux. Фильтрация и NAT сделаны на eBPF
(XDP и TC), управление — Go-сервисы в k3s, веб-интерфейс на React. Ставится с
ISO на голое железо или виртуальную машину: одна нода, готовая к работе после
перезагрузки.

Сейчас умеет:

- зоны, правила между зонами с объектами адресов и сервисов, stateful-трекинг
  TCP/UDP/ICMP, IPv4 и IPv6;
- маскарад и SNAT, проброс портов;
- блок-лист и ограничение SYN на XDP, до разбора пакета ядром;
- статические маршруты, VLAN, DHCP-клиент на WAN;
- кандидат конфигурации, diff, применение с подтверждением и автооткатом,
  история ревизий;
- журнал отбоев и сессий, таблица активных соединений, графики трафика;
- обновление пакетом через веб с откатом при неудаче;
- обход DPI через nfqws (zapret) со стратегиями из сборки Flowseal;
- API с токенами и `rendolctl` поверх него.

## Быстрый старт

| Что нужно | Команда | Где читать |
|---|---|---|
| Поставить на сервер | `make iso`, загрузиться с образа | [docs/install.md](docs/install.md) |
| Посмотреть интерфейс | `make dev`, затем https://localhost:8443, admin / admin | [docs/development.md](docs/development.md) |
| Поднять в minikube | `make minikube` | [docs/development.md](docs/development.md) |
| Обновить узел | собрать `make bundle`, загрузить файл `.rnu` в разделе «Обновление» | [docs/update.md](docs/update.md) |

## Документация

- [architecture.md](docs/architecture.md) — из чего состоит узел и как части говорят друг с другом
- [dataplane.md](docs/dataplane.md) — eBPF-программы, карты, путь пакета
- [config-model.md](docs/config-model.md) — формат конфигурации, кандидат, ревизии, подтверждение
- [install.md](docs/install.md) — ISO, требования, установка, автоустановка
- [update.md](docs/update.md) — пакет обновления и как он ставится
- [zapret.md](docs/zapret.md) — обход DPI
- [api.md](docs/api.md) — HTTP API и `rendolctl`
- [build.md](docs/build.md) — сборка, зеркала Nexus, CI
- [development.md](docs/development.md) — стенды и тесты

## Раскладка репозитория

```
bpf/            eBPF на C: XDP-фильтр, TC ingress/egress, NAT
cmd/            rendol-api, rendol-agent, rendol-updater, rendol-console,
                rendol-installer, rendolctl
internal/       netcfg (модель конфига), dataplane, agent, api, store,
                update, installer, zapret, platform/*
web/            веб-интерфейс
deploy/         Dockerfile, манифесты k8s, dev-стенд
iso/            сборка ISO: mmdebstrap, xorriso, файлы корневой системы
scripts/        сборка пакета и ISO, импорт стратегий zapret
docs/           документация
```

## Лицензия

GPLv3, см. [LICENSE](LICENSE). eBPF-программы — GPL-2.0, как того требует ядро.
Стратегии и списки zapret взяты из
[Flowseal/zapret-discord-youtube](https://github.com/Flowseal/zapret-discord-youtube)
(MIT), nfqws — из [bol-van/zapret](https://github.com/bol-van/zapret).
