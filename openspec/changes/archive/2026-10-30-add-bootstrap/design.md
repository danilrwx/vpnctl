# Design

## Context
Есть SSH-слой (`ssh::Ssh`, known_hosts из `host_key`), рендер (`render::render_host` даёт `config.json` с `route.rule_set`), шаблоны minijinja с переопределением из `templates/` состояния и `versions.yaml` с версиями и sha256. Целевая ОС — Debian 13, вход по root. См. proposal.md — Why.

## Goals / Non-Goals
**Goals:** один скрипт, который можно прочитать и запустить руками; каждая фаза сама проверяет «уже сделано»; всё, что меняется от хоста к хосту, приходит из состояния.
**Non-Goals:** управление sshd и пользователями; обновление самого bootstrap-скрипта на хосте в фоне; L3-протоколы кроме AWG.

## Decisions
- **Один shell-скрипт из шаблона `bootstrap.sh.j2`**, выполняется `ssh … sh -s` со stdin; stdout стримится оператору. Альтернатива — пошаговые ssh-вызовы из Rust: больше кода и round-trip'ов, а идемпотентность всё равно живёт в shell. Шаблон лежит в `templates/` состояния, оператор может править.
- **Источники бинарников — релизы GitHub**, URL-шаблоны с версией в коде (`sing-box`, `xray`, `caddy`, `mtg`), sha256 — из `versions.yaml`. Нет sha — предупреждение, не ошибка (первая установка, потом оператор закрепляет).
- **Rule-set'ы из релизов `SagerNet/sing-geosite` и `sing-geoip`**: `geodata.version` в `versions.yaml` — тег релиза (`YYYYMMDDhhmmss`). Список тегов — из `route.rule_set` отрендеренного `config.json`, чтобы не качать 1400 файлов. Отдельного sha на каждый файл нет: принимаем, релизы неизменяемы.
- **AWG — сборка из исходников Amnezia через DKMS** (`amneziawg-linux-kernel-module` и `amneziawg-tools` на теге `v<awg>` из `versions.yaml`), а не PPA: репозиторий Amnezia собран под Ubuntu и требует доверять ключу, которого нет в состоянии. DKMS пересобирает модуль после обновления ядра.
- **Окно перезагрузки** — `Unattended-Upgrade::Automatic-Reboot` + override таймера `apt-daily-upgrade.timer` на нечётные/чётные дни по роли.
- **Host key** через `ssh-keyscan -t ed25519` до первого подключения, коммит `hosts: <name>: host key`; потом `StrictHostKeyChecking=yes`.
- **Самоподписанный сертификат** на выходе через `openssl` в `/etc/vpnctl/self.{crt,key}`; клиент линка hy2 идёт с `insecure: true` (так уже рендерится).

## Risks / Trade-offs
- Сеть хоста: GitHub и apt Amnezia должны быть доступны с хоста; из РФ GitHub может быть медленным — скрипт качает через `curl --retry 3`.
- Тесты не ходят по SSH: покрывается генерация скрипта (идемпотентные проверки, sha-строки, список rule-set'ов, дни перезагрузки).
