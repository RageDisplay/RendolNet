# API

Всё, что делает веб-интерфейс, доступно через `https://<узел>/v1/…`.

## Доступ

- **Токен**: раздел «Пользователи» → «Мои токены API». Заголовок
  `Authorization: Bearer rn_…`. Токен действует с правами владельца, в базе
  хранится только его хеш.
- **Сессия браузера**: cookie `rn_session` после `POST /v1/auth/login`. Для
  изменяющих запросов с cookie нужен заголовок `X-RN-Request: 1` — браузер не
  отправит его с чужого сайта без preflight.

Роли: `viewer` — только чтение; `operator` — правка и применение конфига,
сброс сессий; `admin` — ещё пользователи, настройки, обновления, перезагрузка.

После 10 неудачных входов с одного адреса за 5 минут вход для него закрывается
до конца окна.

Ошибки возвращаются как `{"error": "текст"}`. Ошибки проверки конфига — как
`{"errors": [{"path": "rules[2].action", "message": "…"}]}` с кодом 422.

## Методы

| Метод | Роль | Что делает |
|---|---|---|
| `GET /v1/config/candidate` | viewer | черновик |
| `PUT /v1/config/candidate` | operator | заменить черновика: `{"body": {...}, "if_unmodified": "<updated_at>"}` |
| `GET /v1/config/diff[?from=N&to=M]` | viewer | построчный diff; по умолчанию действующая ревизия → черновик |
| `POST /v1/config/commit` | operator | `{"comment": "...", "confirm_seconds": 120}` |
| `POST /v1/config/confirm` | operator | подтвердить ждущую ревизию |
| `GET /v1/config/revisions` | viewer | история |
| `POST /v1/config/revisions/{id}/load` | operator | ревизию в черновик |
| `GET /v1/config/export[?revision=N]` | viewer | JSON для скачивания |
| `GET /v1/dashboard` | viewer | всё для главной страницы |
| `GET /v1/links` | viewer | интерфейсы узла со счётчиками |
| `GET /v1/sessions?limit=N` | viewer | активные сессии |
| `DELETE /v1/sessions` | operator | сбросить все сессии |
| `GET /v1/counters` | viewer | срабатывания правил по id |
| `GET /v1/events?type=&action=&addr=&port=&rule=&since=&before=&limit=` | viewer | журнал |
| `GET /v1/traffic?hours=N` | viewer | минутные скорости по интерфейсам |
| `GET /v1/system` | viewer | узел, агент, обновление |
| `POST /v1/system/reboot`, `/poweroff` | admin | |
| `POST /v1/update/upload` | admin | тело запроса — файл .rnu |
| `POST /v1/update/apply` | admin | установить загруженный пакет |
| `GET /v1/zapret/strategies`, `/status` | viewer | |
| `GET /v1/users`, `POST`, `PUT /{id}`, `DELETE /{id}` | admin | пользователи |
| `GET /v1/audit` | admin | аудит |
| `GET /healthz` | — | версия, без авторизации |

## rendolctl

```
export RN_URL=https://10.0.0.10 RN_TOKEN=rn_... RN_INSECURE=1

rendolctl status
rendolctl config get > fw.json
# правка fw.json
rendolctl config set fw.json
rendolctl config diff
rendolctl commit -m "открыл 8443 для мониторинга" -confirm 120
rendolctl confirm
rendolctl revisions
rendolctl events -n 100
```

`RN_INSECURE=1` (или `-k`) отключает проверку сертификата. По умолчанию узел
выпускает самоподписанный сертификат. Свой сертификат кладётся в
`/var/lib/rendolnet/tls/tls.crt` и `tls.key`, после этого перезапустите под API.

## Агент

У агента свой API на unix-сокете `/run/rendolnet/agent.sock` (`internal/agent/server.go`).
Им пользуются rendol-api, консоль и zapret. Авторизации там нет, доступ ограничен
правами на файл сокета (0660).
