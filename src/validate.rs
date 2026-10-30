//! Статическая проверка состояния. Возвращает список ошибок; пустой — состояние валидно.
use crate::state::*;
use std::collections::{BTreeMap, BTreeSet, HashSet};

fn ident_ok(s: &str) -> bool {
    !s.is_empty()
        && s.chars()
            .all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '-' || c == '_')
}

fn date_ok(s: &str) -> bool {
    let b = s.as_bytes();
    b.len() == 10
        && b[4] == b'-'
        && b[7] == b'-'
        && s.chars()
            .enumerate()
            .all(|(i, c)| i == 4 || i == 7 || c.is_ascii_digit())
}

pub fn validate(st: &State) -> Vec<String> {
    let mut e = Vec::new();
    let mut err = |s: String| e.push(s);

    // --- хосты ---
    let mut domains: BTreeMap<&str, Vec<&str>> = BTreeMap::new();
    for (name, h) in &st.hosts {
        if !ident_ok(name) {
            err(format!("хост «{name}»: имя — только [a-z0-9-_]"));
        }
        if let Some(v) = &h.via {
            if !st.hosts.contains_key(v) {
                err(format!("хост «{name}»: via «{v}» не описан"));
            }
            if v == name {
                err(format!("хост «{name}»: via указывает на самого себя"));
            }
        }
        if let Some(d) = &h.domain {
            domains.entry(d).or_default().push(name);
        }
        let inb = st.inbounds.get(name).cloned().unwrap_or_default();
        if h.engine == Engine::Xray && inb.contains_key(&Proto::Hy2) {
            err(format!(
                "хост «{name}»: engine xray не умеет hysteria2 — уберите inbound hy2 или переключите engine"
            ));
        }
        if h.engine == Engine::Xray && inb.contains_key(&Proto::Awg) && h.role == Role::Entry {
            err(format!(
                "хост «{name}»: engine xray не умеет DNS-сервер для L3-клиентов — awg на входе требует sing-box"
            ));
        }
        let mut tcp = BTreeMap::new();
        let mut udp = BTreeMap::new();
        for (p, i) in &inb {
            let table = if matches!(p, Proto::Hy2 | Proto::Awg) {
                &mut udp
            } else {
                &mut tcp
            };
            if let Some(other) = table.insert(i.port, *p) {
                err(format!(
                    "хост «{name}»: порт {} занят и {} и {}",
                    i.port,
                    other.name(),
                    p.name()
                ));
            }
            if *p == Proto::Awg && h.role == Role::Entry {
                match &i.subnets {
                    None => err(format!(
                        "хост «{name}»: awg на входе требует subnets (split/full)"
                    )),
                    Some(s)
                        if s.split.contains(&s.full.network())
                            || s.full.contains(&s.split.network()) =>
                    {
                        err(format!(
                            "хост «{name}»: подсети awg split {} и full {} пересекаются",
                            s.split, s.full
                        ))
                    }
                    _ => {}
                }
            }
        }
        let hs = st.secrets.hosts.get(name);
        if inb.contains_key(&Proto::Reality) && hs.and_then(|s| s.reality.as_ref()).is_none() {
            err(format!("хост «{name}»: нет ключей reality в секретах"));
        }
        if inb.contains_key(&Proto::Awg) && hs.and_then(|s| s.awg.as_ref()).is_none() {
            err(format!("хост «{name}»: нет ключей awg в секретах"));
        }
        if inb.contains_key(&Proto::Mtg) && hs.and_then(|s| s.mtg_secret.as_ref()).is_none() {
            err(format!("хост «{name}»: нет секрета mtg в секретах"));
        }
        if h.role == Role::Entry && h.domain.is_none() {
            err(format!("хост «{name}»: входу нужен домен"));
        }
    }
    for (d, hs) in &domains {
        if hs.len() > 1 && !hs.iter().any(|h| st.hosts[*h].retiring) {
            err(format!(
                "домен {d} у хостов {}: допустимо только на время переезда, пометьте старый retiring: true",
                hs.join(", ")
            ));
        }
    }
    // цикл в графе via
    for start in st.hosts.keys() {
        let mut seen = vec![start.as_str()];
        let mut cur = st.hosts[start].via.as_deref();
        while let Some(v) = cur {
            if seen.contains(&v) {
                seen.push(v);
                err(format!("цикл в графе via: {}", seen.join(" → ")));
                break;
            }
            seen.push(v);
            cur = st.hosts.get(v).and_then(|h| h.via.as_deref());
        }
    }

    // --- линки ---
    let mut keys = HashSet::new();
    for l in &st.links {
        let k = l.key();
        if !keys.insert(k.clone()) {
            err(format!("линк {k} описан дважды"));
        }
        if l.from == l.to {
            err(format!("линк {k}: from и to совпадают"));
        }
        for side in [&l.from, &l.to] {
            if !st.hosts.contains_key(side) {
                err(format!("линк {k}: хост «{side}» не описан"));
            }
        }
        if l.protocols.is_empty() {
            err(format!("линк {k}: нет протоколов"));
        }
        let to_inb = st.inbounds.get(&l.to).cloned().unwrap_or_default();
        for p in &l.protocols {
            if !p.is_link() {
                err(format!(
                    "линк {k}: {} не годится как транспорт линка",
                    p.name()
                ));
            }
            if !to_inb.get(p).is_some_and(|i| i.enabled) {
                err(format!(
                    "линк {k}: на приёмнике «{}» нет включённого inbound {}",
                    l.to,
                    p.name()
                ));
            }
        }
        if let Some(a) = l.active {
            if !l.protocols.contains(&a) {
                err(format!(
                    "линк {k}: active {} нет среди протоколов",
                    a.name()
                ));
            }
        } else if l.policy == Policy::Fixed {
            err(format!("линк {k}: policy fixed требует active"));
        }
        if l.protocols.contains(&Proto::Awg) && l.tunnel.is_none() {
            err(format!("линк {k}: awg требует tunnel (подсеть /30)"));
        }
        let ls = st.secrets.links.get(&k);
        if l.protocols.contains(&Proto::Awg) && ls.and_then(|s| s.awg.as_ref()).is_none() {
            err(format!(
                "линк {k}: нет ключей awg для обеих сторон в секретах"
            ));
        }
        if l.protocols.contains(&Proto::Reality) && ls.and_then(|s| s.reality.as_ref()).is_none() {
            err(format!("линк {k}: нет ключей reality в секретах"));
        }
        if l.protocols.contains(&Proto::Hy2) && ls.and_then(|s| s.hy2_password.as_ref()).is_none() {
            err(format!("линк {k}: нет пароля hy2 в секретах"));
        }
    }
    for (a, b) in st
        .links
        .iter()
        .enumerate()
        .flat_map(|(i, a)| st.links.iter().skip(i + 1).map(move |b| (a, b)))
    {
        if let (Some(x), Some(y)) = (a.tunnel, b.tunnel)
            && (x.contains(&y.network()) || y.contains(&x.network()))
        {
            err(format!(
                "линки {} и {}: подсети туннелей {x} и {y} пересекаются",
                a.key(),
                b.key()
            ));
        }
    }

    // --- пользователи ---
    let mut peer_ips: BTreeMap<std::net::Ipv4Addr, &str> = BTreeMap::new();
    for (name, u) in &st.users {
        if !ident_ok(name) {
            err(format!("пользователь «{name}»: имя — только [a-z0-9-_]"));
        }
        if u.modes.is_empty() {
            err(format!("пользователь «{name}»: нет режимов"));
        }
        if u.protocols.is_empty() {
            err(format!("пользователь «{name}»: нет протоколов"));
        }
        if let Some(d) = &u.expires
            && !date_ok(d)
        {
            err(format!(
                "пользователь «{name}»: expires «{d}» не YYYY-MM-DD"
            ));
        }
        let Some(entry) = st.entry_of(u) else {
            err(format!(
                "пользователь «{name}»: не указан вход (entry), а входов не ровно один"
            ));
            continue;
        };
        if !st.hosts.get(&entry).is_some_and(|h| h.role == Role::Entry) {
            err(format!("пользователь «{name}»: entry «{entry}» не вход"));
            continue;
        }
        let inb = st.inbounds.get(&entry).cloned().unwrap_or_default();
        for p in &u.protocols {
            if !inb.contains_key(p) {
                err(format!(
                    "пользователь «{name}»: на входе «{entry}» нет inbound {}",
                    p.name()
                ));
            }
        }
        let has_awg = u.protocols.contains(&Proto::Awg);
        match (&u.awg, has_awg) {
            (Some(peer), true) => {
                if let Some(s) = inb.get(&Proto::Awg).and_then(|i| i.subnets.as_ref()) {
                    let net = s.for_mode(peer.mode);
                    if !net.contains(&peer.ip) {
                        err(format!(
                            "пользователь «{name}»: адрес awg {} вне подсети режима {} ({net})",
                            peer.ip,
                            peer.mode.name()
                        ));
                    }
                    if peer.ip == net.network()
                        || peer.ip == net.hosts().next().unwrap_or(net.network())
                    {
                        err(format!(
                            "пользователь «{name}»: адрес awg {} зарезервирован за шлюзом",
                            peer.ip
                        ));
                    }
                }
                if !u.modes.contains(&peer.mode) {
                    err(format!(
                        "пользователь «{name}»: режим пира awg {} не входит в режимы пользователя",
                        peer.mode.name()
                    ));
                }
                if let Some(other) = peer_ips.insert(peer.ip, name) {
                    err(format!("адрес awg {} и у «{other}», и у «{name}»", peer.ip));
                }
                if st
                    .secrets
                    .users
                    .get(name)
                    .and_then(|s| s.awg.as_ref())
                    .is_none()
                {
                    err(format!("пользователь «{name}»: нет ключей awg в секретах"));
                }
            }
            (None, true) => err(format!(
                "пользователь «{name}»: протокол awg без описания пира (awg: {{mode, ip}})"
            )),
            (Some(_), false) => err(format!(
                "пользователь «{name}»: пир awg описан, но протокола awg нет"
            )),
            (None, false) => {}
        }
        if !st.secrets.users.contains_key(name) {
            err(format!(
                "пользователь «{name}»: нет секретов (uuid, hy2_password, sub_id)"
            ));
        }
    }

    // --- маршрутизация и наборы ---
    for (name, s) in &st.sets {
        if !ident_ok(name) {
            err(format!("набор «{name}»: имя — только [a-z0-9-_]"));
        }
        for d in &s.domains {
            let body = d
                .split_once(':')
                .map(|(p, b)| {
                    if matches!(p, "full" | "keyword" | "regexp" | "domain") {
                        b
                    } else {
                        d.as_str()
                    }
                })
                .unwrap_or(d);
            if body.is_empty() || body.contains(char::is_whitespace) {
                err(format!("набор «{name}»: домен «{d}» выглядит неверно"));
            }
        }
    }
    for (host, r) in &st.routing {
        let Some(h) = st.hosts.get(host) else {
            err(format!("routing/{host}.yaml: хост не описан"));
            continue;
        };
        if let Some(p) = &r.exit.prefer
            && st.link(host, p).is_none()
        {
            err(format!(
                "routing/{host}.yaml: exit.prefer «{p}», но линка {host}->{p} нет"
            ));
        }
        if r.exit.policy == ExitPolicy::Fixed && r.exit.prefer.is_none() {
            err(format!(
                "routing/{host}.yaml: exit.policy fixed требует prefer"
            ));
        }
        let inbound_names: BTreeSet<&str> = st
            .inbounds
            .get(host)
            .map(|m| m.keys().map(|p| p.name()).collect())
            .unwrap_or_default();
        for (g, rules) in [
            ("all", &r.groups.all),
            ("split", &r.groups.split),
            ("full", &r.groups.full),
        ] {
            if h.role != Role::Entry && !rules.is_empty() {
                err(format!(
                    "routing/{host}.yaml: правила маршрутизации только у входов"
                ));
            }
            for (i, rule) in rules.iter().enumerate() {
                let at = format!("routing/{host}.yaml: {g} #{}", i + 1);
                for s in &rule.match_.sets {
                    if !st.sets.contains_key(s) {
                        err(format!("{at}: набор «{s}» не описан"));
                    }
                }
                for ib in &rule.match_.inbound {
                    if ib != "probe" && !inbound_names.contains(ib.as_str()) {
                        err(format!("{at}: inbound «{ib}» не описан на хосте"));
                    }
                }
                if let Action::Link { link } = &rule.action
                    && link != "auto"
                    && st.link(host, link).is_none()
                {
                    err(format!("{at}: линка {host}->{link} нет"));
                }
                if rule.match_.is_empty() && i + 1 != rules.len() {
                    err(format!(
                        "{at}: правило без условия ловит всё, правила ниже недостижимы"
                    ));
                }
            }
        }
    }
    e
}

/// Предупреждения: состояние применимо, но что-то стоит доделать.
pub fn warnings(st: &State) -> Vec<String> {
    let mut w = Vec::new();
    for (name, h) in &st.hosts {
        if h.role == Role::Entry && !h.retiring && st.links_from(name).next().is_none() {
            w.push(format!(
                "хост «{name}»: у входа нет ни одного линка к выходу — весь трафик пойдёт напрямую"
            ));
        }
        if h.host_key.is_none() {
            w.push(format!(
                "хост «{name}»: host key ещё не записан (появится после bootstrap)"
            ));
        }
    }
    if !st.secrets_encrypted && !st.secrets.users.is_empty() {
        w.push("секреты лежат открытым текстом: нет .sops.yaml (init --age <получатель>)".into());
    }
    w
}
