## 1. Движок
- [x] 1.1 Шаблоны sing-box: inbound'ы по протоколам, пользователи, clash api для статистики, tproxy-inbound со sniffing, probe-socks, Hysteria2, rule-set, urltest, DNS-сервер
- [x] 1.2 Шаблоны xray по `engine: xray`: то же без Hysteria2 и DNS-сервера, stats/api
- [x] 1.3 Routing: группы → правила движка; DNS по наборам; балансер протоколов линка и выбор выхода по политике из routing/<host>.yaml
- [x] 1.4 Outbound'ы линков: AWG через интерфейс, Reality/Hy2 как outbound

## 2. Остальные службы
- [x] 2.1 Caddyfile: сайт-прикрытие, пути ws и подписок, заголовки подписок, выбор формата по User-Agent
- [x] 2.2 `awg*.conf` для входа и пиров выхода; `mtg.toml` с upstream socks
- [x] 2.3 nftables, sysctl, systemd-юниты

## 3. Подписки и профили
- [x] 3.1 Ссылки vless/hysteria2/trojan из состояния; base64; sing-box JSON; Clash YAML
- [x] 3.2 `awg.conf` и `tg://proxy` для пользователя; QR в терминале

## 4. Детерминированность
- [x] 4.1 Сортировка ключей, запрет времени/случайностей в шаблонах
- [x] 4.2 `render --check`: два прогона, сравнение хэшей; задание для CI без секретов (заглушки)
- [x] 4.3 Снапшот-тесты на фикстурах
