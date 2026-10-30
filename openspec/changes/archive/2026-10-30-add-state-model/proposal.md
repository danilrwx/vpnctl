# Change: Модель состояния в git

## Why
Вся инфраструктура должна выводиться из файлов в репозитории: хосты, линки, протоколы, пользователи, наборы доменов, версии, секреты. Без этого невозможны детерминированный рендер, plan/apply и переезд ноды.

## What Changes
- Схема файлов состояния (хост: `engine`, `retiring`; выбор выхода в `routing/<host>.yaml`): `hosts.yaml`, `links.yaml`, `protocols.yaml`, `users.yaml`, `routing/<host>.yaml`, `sets/*.yaml`, `versions.yaml`, `secrets.sops.yaml`
- Загрузка, валидация (ссылочная целостность, граф `via` без циклов, подсети не пересекаются, ключи линков с обеих сторон), типизированные структуры в `vpnctl-core`
- Команды `init`, `host add`, `user add`, `link add`, `set edit`, `rule add` как операции над состоянием с генерацией секретов в sops и коммитом
- Команда `validate`

## Impact
- Новый крейт `vpnctl-core`, модуль `state`
- Все остальные change'и зависят от этого
