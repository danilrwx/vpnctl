# Change: Детерминированный рендер конфигов

## Why
Конфиги движка (xray/sing-box), веб-сервера, AWG, mtg и файлы подписок должны выводиться из состояния побайтно воспроизводимо, иначе plan не сможет отличить изменение от шума, а CI — проверить коммит.

## What Changes
- Шаблоны minijinja в `templates/` для xray и sing-box (inbound'ы, routing по группам, DNS, outbound'ы линков, балансер), Caddyfile, `awg*.conf`, mtg, nftables, systemd-юниты
- Рендер правил маршрутизации: группы «для всех / split / full» → плоский список правил движка с подстановкой email'ов и подсетей
- Рендер подписок: base64-список ссылок + sing-box JSON + Clash YAML, заголовки через веб-сервер
- Команда `render [--host] [--out]`, проверка воспроизводимости (`render --check`)

## Impact
- Модуль `render` в `vpnctl-core`, каталог `templates/`
- Зависит от `add-state-model`
