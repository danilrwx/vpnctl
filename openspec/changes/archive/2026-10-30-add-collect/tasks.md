# Tasks

## 1. База и чистые функции
- [x] 1.1 `rusqlite` (bundled) в Cargo.toml; `metrics::Db::open` создаёт схему `samples`/`traffic`/`totals`/`edges`; `metrics.sqlite` в `.gitignore` состояния при `init` — проверка: тест открывает базу во временном каталоге, `ops::init` пишет `.gitignore` с этой строкой
- [x] 1.2 Декодер gRPC-кадра и protobuf `QueryStatsResponse` → карта имя→значение; `delta(prev, cur)` — проверка: тесты на собранных вручную байтах и на трёх сценариях дельт
- [x] 1.3 Скрипт хоста `host_script(engine, cert, role)` и `parse_host` → структура метрик; `sh -n` скрипта проходит — проверка: тест парсит образец вывода, включая отсутствующие поля

## 2. collect и probe
- [x] 2.1 `collect`: `plan::plan` + параллельный скрипт по хостам, запись снятий, дельт, итогов, ретеншн 60 дней, печать строки на хост — проверка: тест записи/чтения дельт и ретеншна в temp-базе без SSH; `cargo run -- collect` на фикстуре падает только на SSH
- [x] 2.2 `probe`: проба цепочки с входов с печатью кода и мс; `probe --matrix`: статическая проверка, рёбра с ноутбука и с входов, скрипт `edge_script`/`parse_edges`, таблица как в `screens/05-topology.html`, запись в `edges` — проверка: тесты парсера рёбер (ok/fail/не описан/старое рукопожатие) и форматирования ячеек

## 3. status --json
- [x] 3.1 `metrics::status(st, db)` → JSON: хосты, пользователи (сутки/неделя/всего, суммирование учёток), outbound'ы, пробы, матрица; команда `status --json` — проверка: тест на temp-базе с двумя учётками одного пользователя и на пустой базе
- [x] 3.2 README: строка про `collect`/`probe --matrix`/`status --json`; `docs/design.md` раздел 8: решение про чтение stats API через curl — проверка: текст на месте, `cargo fmt --check`, `cargo clippy --all-targets -- -D warnings`, `cargo test` зелёные
