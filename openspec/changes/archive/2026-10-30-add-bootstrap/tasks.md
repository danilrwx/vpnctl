## 1. Скрипт
- [x] 1.1 Шаблон `bootstrap.sh.j2`: apt + security, `unattended-upgrades` и окно перезагрузки по роли, nftables и sysctl из рендера, каталоги `/etc/vpnctl`
- [x] 1.2 Загрузка движка (по `engine`), caddy и mtg (только вход) по `versions.yaml` с проверкой sha256; предупреждение без sha
- [x] 1.3 Rule-set'ы sing-box из релиза `geodata` по тегам из рендера хоста
- [x] 1.4 `amneziawg-dkms`/`amneziawg-tools` из репозитория Amnezia; самоподписанный сертификат на выходе; юниты включены, не запущены; маркер bootstrap
- [x] 1.5 `bootstrap::script(st, host)` — чистая функция, контекст шаблона из состояния и рендера

## 2. Команда
- [x] 2.1 `host bootstrap <name> [--ssh user@ip]`: host key через `ssh-keyscan` + коммит, выполнение скрипта со стримом вывода
- [x] 2.2 Тесты `tests/bootstrap.rs`: вход и выход, xray, отсутствие sha, список rule-set'ов, дни перезагрузки
- [x] 2.3 README: строка про `host bootstrap`; встроенный шаблон в `render::TEMPLATES`
