# Change: Bootstrap хоста

## Why
`apply` умеет только раскладывать конфиги и пропускает хост без маркера bootstrap. Чтобы новый VPS стал нодой одной командой (и переезд ноды был bootstrap + apply + DNS), нужна идемпотентная подготовка хоста по шаблонам из репо: пакеты, бинарники по пинам из `versions.yaml`, фаервол, ядро AWG, сертификаты, юниты.

## What Changes
- Команда `host bootstrap <name> [--ssh user@ip]`: записывает host key в состояние, затем по SSH выполняет скрипт подготовки, печатая прогресс построчно
- Шаблон `bootstrap.sh.j2` рядом с остальными шаблонами (копируется в `templates/` состояния, правится оператором)
- Загрузка sing-box/xray, caddy, mtg по версиям и sha256 из `versions.yaml`; rule-set'ы sing-box (`geosite-*`, `geoip-*`) по релизу из `versions.yaml` → `geodata` — только те, что встречаются в рендере хоста
- Среда выполнения вне git: security-репозитории, `unattended-upgrades` с окном перезагрузки (вход — нечётные дни, выходы — чётные), nftables, sysctl, AmneziaWG через DKMS из исходников, самоподписанный сертификат на выходе, systemd-юниты включены, но не запущены до `apply`
- Маркер `/etc/vpnctl/bootstrap = <коммит>`

## Capabilities

### New Capabilities
- `bootstrap`: подготовка хоста к первому `apply` и повторная досборка среды выполнения

### Modified Capabilities
(нет: `apply` уже требует маркер bootstrap)

## Impact
- Модуль `bootstrap` в крейте, шаблон `src/templates/bootstrap.sh.j2`, команда в `main.rs`
- Зависит от `add-state-model`, `add-render` (список rule-set'ов берётся из рендера), `add-apply` (SSH-слой)
- Внешние источники: релизы GitHub sing-box/xray/caddy/mtg/sing-geosite/sing-geoip, исходники Amnezia на GitHub
