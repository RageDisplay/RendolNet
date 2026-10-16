# Обход DPI (zapret)

Модуль запускает `nfqws` из [bol-van/zapret](https://github.com/bol-van/zapret)
со стратегиями из сборки
[Flowseal/zapret-discord-youtube](https://github.com/Flowseal/zapret-discord-youtube).
Выключен по умолчанию, включается в разделе «Обход DPI»: интерфейс WAN,
стратегия, свои домены и адреса. Как и всё остальное, это часть конфига: работают
кандидат, подтверждение и откат.

## Откуда стратегии

Сборка Flowseal сделана для Windows: `winws.exe` и `.bat`-файлы. `winws` — порт
`nfqws`, аргументы у них почти общие. `scripts/import-zapret-strategies.sh`
скачивает релиз Flowseal и переводит каждый `general*.bat`
(`internal/zapret/convert.go`):

| В .bat | В RendolNet |
|---|---|
| `--wf-tcp=…`, `--wf-udp=…` (фильтр WinDivert) | порты для правил nftables |
| `%BIN%`, `%LISTS%` | `/opt/zapret/bin`, рабочий каталог списков |
| `^!` | `!` (экранирование cmd) |
| профили с `%GameFilter…%` | отбрасываются: игровой фильтр у Flowseal выключен по умолчанию |
| `--wf-raw`, `--ssid-filter`, `--nlm-filter` | стратегия помечается непереносимой |

Результат — `internal/zapret/strategies.json`, он встраивается в бинари.
Файлы `bin/*.bin` и `lists/*` в репозиторий не кладутся: образ zapret берёт
их из того же релиза Flowseal при сборке (`FLOWSEAL_REF` в `deploy/Dockerfile`).

При сборке образа каждая стратегия прогоняется через `nfqws --dry-run`. Если
nfqws какой-то аргумент не понимает, образ не соберётся. Сейчас: Flowseal 1.10.3,
zapret v72.13, переносятся все 22 стратегии.

Обновить стратегии:

```
bash scripts/import-zapret-strategies.sh 1.10.4
# поднять FLOWSEAL_REF в deploy/Dockerfile до той же версии
```

## Как работает на узле

Под `zapret` раз в 5 секунд читает конфиг у агента. Когда обход включён:

1. Копирует списки в рабочий каталог и дописывает свои домены
   (`list-general-user.txt`) и адреса (`ipset-all.txt`).
2. Ставит таблицу nftables:

   ```
   table inet rendol_zapret {
     chain post {
       type filter hook postrouting priority mangle; policy accept;
       oifname "eth1" meta mark & 0x40000000 == 0 tcp dport { 80, 443, … } ct original packets 1-6 queue flags bypass to 200
       oifname "eth1" meta mark & 0x40000000 == 0 udp dport { 443, 19294-19344, 50000-50100 } ct original packets 1-6 queue flags bypass to 200
     }
   }
   ```

3. Запускает `nfqws --qnum=200 --dpi-desync-fwmark=0x40000000` с аргументами стратегии.

В очередь уходят только первые 6 пакетов исходящих соединений на нужные
порты, остальной трафик идёт мимо. `bypass`: если nfqws упал, пакеты проходят,
а не теряются. Пакеты, которые nfqws отправляет сам, помечены 0x40000000 и
в очередь повторно не попадают.

Выключение удаляет таблицу и останавливает nfqws. Состояние (работает ли,
сколько перезапусков, последняя ошибка) пишется в `/run/rendolnet/zapret.json`
и показывается в интерфейсе.

## Сочетание с eBPF и NAT

nftables postrouting стоит раньше TC egress, поэтому nfqws видит пакеты ещё до
маскарада, с внутренними адресами. Поддельные пакеты nfqws проходят через
`tc_egress` так же, как обычные: у сессии уже есть запись в `ct`, и они
получают тот же внешний порт NAT.

## Если не помогает

Стратегии рассчитаны на разное оборудование провайдеров. Перебирайте их по
очереди: применяйте с подтверждением и проверяйте сайты из LAN. Свои домены
добавляются к спискам любой стратегии.
