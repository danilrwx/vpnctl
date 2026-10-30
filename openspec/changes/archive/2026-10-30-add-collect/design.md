# Design

## Context
Есть SSH-слой (`ssh::Ssh`), параллельный опрос в `plan::plan` (достижимость, маркер, хэши → дрейф), рендер с `experimental.v2ray_api` sing-box на `127.0.0.1:8080` (`render::STATS_API`) и xray `api` на `127.0.0.1:10085`, теги outbound'ов `<exit>-<proto>`, `link-<exit>`, `exit-auto`, socks `probe` на 1080, `render::cert_paths`. На машине оператора нет sing-box; тесты не ходят по SSH. См. proposal.md — Why.

## Goals / Non-Goals
**Goals:** одна команда снимает всё с хоста за один SSH-вызов; все парсеры и расчёт дельт — чистые функции с тестами; `status --json` работает без сети.
**Non-Goals:** матрица как «настоящий клиент каждого протокола» с временными конфигами в tmpfs (отложено: нужен движок на ноутбуке); пробник внутри РФ; графики; лимиты трафика.

## Decisions
- **Stats API читается `curl`'ом как gRPC поверх HTTP/2, ответ разбирается вручную.** `sing-box tools` не умеет опрашивать `v2ray_api`; `grpcurl` — лишний бинарник плюс `.proto` на хосте (сервер без reflection); `clash_api` даёт только живые соединения без накопленных счётчиков по пользователям; xray как клиент — 30 МБ ради одного запроса. gRPC-вызов `QueryStats` с пустым паттерном — это POST на `/v2ray.core.app.stats.command.StatsService/QueryStats` с телом из пяти нулевых байт (кадр: флаг сжатия + длина 0 + пустой `QueryStatsRequest`); `curl --http2-prior-knowledge -H 'content-type: application/grpc' --data-binary @-` отдаёт тело ответа, хост кодирует его `base64 -w0`. Ответ — `QueryStatsResponse { repeated Stat { string name = 1; int64 value = 2 } }`: varint и length-delimited поля, ~30 строк декодера без зависимостей. Один и тот же путь для sing-box (8080) и xray (10085, тот же сервис). Имена: `user>>>brat-split-reality>>>traffic>>>uplink`, `outbound>>>fi-hel-1-awg>>>traffic>>>downlink`.
- **Один скрипт на хост, вывод `ключ=значение` построчно**: load из `/proc/loadavg`, память из `/proc/meminfo`, диск `df -kP /`, соединения `ss -Htn state established | wc -l`, uptime из `/proc/uptime`, обновления `apt-get -s upgrade | grep -c '^Inst '`, `reboot-required` по файлу, срок сертификата `openssl x509 -enddate` → `date -d … +%s` на хосте (дата в epoch, парсер дат не нужен), проба `curl -w '%{http_code} %{time_total}'`. Парсер — чистая функция `parse_host`.
- **Дельта** `delta(prev, cur) = cur - prev` если `cur >= prev`, иначе `cur`; первое снятие — `cur`. Итоги «всего» — отдельная таблица `totals`, в которую дельты прибавляются; ретеншн чистит только снятия.
- **Достижимость, маркер и дрейф** — из `plan::plan` (уже параллельный), чтобы не дублировать логику; `collect` вызывает его и затем свой скрипт по хостам (ControlMaster делает второе подключение дешёвым).
- **Схема SQLite** — четыре плоские таблицы: `samples` (хост, время, метрики, ошибка), `traffic` (хост, время, вид user/outbound, имя, дельты, показания), `totals`, `edges` (матрица). `rusqlite` с `bundled`, чтобы не зависеть от libsqlite3 на ноутбуке.
- **Матрица с ноутбука** — `TcpStream::connect_timeout` на `ip:port` TCP-протоколов (reality, mtg; ws через домен:443) и время `ssh true`; UDP-протоколы (hy2, awg) с ноутбука не проверяются — прочерк «udp». **С входа**: `timeout 5 bash -c 'exec 3<>/dev/tcp/…'` (на Debian `sh` — dash без `/dev/tcp`), для awg — возраст последнего рукопожатия `awg show awgN latest-handshakes` и RTT `ping` до второго адреса туннеля.
- **`status --json`** — чистая функция от состояния и содержимого базы; печать через `serde_json`.

## Risks / Trade-offs
- [gRPC через curl не проверен на живом sing-box — на машине его нет] → формат протокола стандартный, парсер покрыт тестами на собранных вручную байтах; первый `collect` на стенде — проверка; при отказе хост печатает пометку «stats недоступны», остальное собирается.
- [tcp-коннект к порту hy2 подтверждает хост, а не протокол (на 443 слушает и reality)] → в выводе так и названо; настоящая проверка hy2 — через urltest движка, отложено вместе с «настоящим клиентом».
- [Первое снятие приписывает весь трафик с запуска движка одному интервалу] → приемлемо для одного оператора; «всего» корректно.
- [`apt-get -s upgrade` на хосте занимает секунды] → хосты опрашиваются параллельно.
