//! Static state validation. Returns a list of errors; an empty list means the state is valid.
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

    // --- hosts ---
    let mut domains: BTreeMap<&str, Vec<&str>> = BTreeMap::new();
    for (name, h) in &st.hosts {
        if !ident_ok(name) {
            err(format!("host «{name}»: name must match [a-z0-9-_]"));
        }
        if let Some(v) = &h.via {
            if !st.hosts.contains_key(v) {
                err(format!("host «{name}»: via «{v}» is not described"));
            }
            if v == name {
                err(format!("host «{name}»: via points to the host itself"));
            }
        }
        if let Some(d) = &h.domain {
            domains.entry(d).or_default().push(name);
        }
        let inb = st.inbounds.get(name).cloned().unwrap_or_default();
        if h.engine == Engine::Xray && inb.contains_key(&Proto::Hy2) {
            err(format!(
                "host «{name}»: engine xray does not support hysteria2 — drop the hy2 inbound or switch the engine"
            ));
        }
        if h.engine == Engine::Xray && inb.contains_key(&Proto::Awg) && h.role == Role::Entry {
            err(format!(
                "host «{name}»: engine xray cannot serve DNS for L3 clients — awg on the entry requires sing-box"
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
                    "host «{name}»: port {} is used by both {} and {}",
                    i.port,
                    other.name(),
                    p.name()
                ));
            }
            if *p == Proto::Awg && h.role == Role::Entry {
                match &i.subnets {
                    None => err(format!(
                        "host «{name}»: awg on the entry requires subnets (split/full)"
                    )),
                    Some(s)
                        if s.split.contains(&s.full.network())
                            || s.full.contains(&s.split.network()) =>
                    {
                        err(format!(
                            "host «{name}»: awg subnets split {} and full {} overlap",
                            s.split, s.full
                        ))
                    }
                    _ => {}
                }
            }
        }
        let hs = st.secrets.hosts.get(name);
        if inb.contains_key(&Proto::Reality) && hs.and_then(|s| s.reality.as_ref()).is_none() {
            err(format!("host «{name}»: no reality keys in secrets"));
        }
        if inb.contains_key(&Proto::Awg) && hs.and_then(|s| s.awg.as_ref()).is_none() {
            err(format!("host «{name}»: no awg keys in secrets"));
        }
        if inb.contains_key(&Proto::Mtg) && hs.and_then(|s| s.mtg_secret.as_ref()).is_none() {
            err(format!("host «{name}»: no mtg secret in secrets"));
        }
        if h.role == Role::Entry && h.domain.is_none() {
            err(format!("host «{name}»: the entry requires a domain"));
        }
    }
    for (d, hs) in &domains {
        if hs.len() > 1 && !hs.iter().any(|h| st.hosts[*h].retiring) {
            err(format!(
                "domain {d} on hosts {}: allowed only during a migration, mark the old host retiring: true",
                hs.join(", ")
            ));
        }
    }
    // cycle in the via graph
    for start in st.hosts.keys() {
        let mut seen = vec![start.as_str()];
        let mut cur = st.hosts[start].via.as_deref();
        while let Some(v) = cur {
            if seen.contains(&v) {
                seen.push(v);
                err(format!("cycle in the via graph: {}", seen.join(" → ")));
                break;
            }
            seen.push(v);
            cur = st.hosts.get(v).and_then(|h| h.via.as_deref());
        }
    }

    // --- links ---
    let mut keys = HashSet::new();
    for l in &st.links {
        let k = l.key();
        if !keys.insert(k.clone()) {
            err(format!("link {k} is described twice"));
        }
        if l.from == l.to {
            err(format!("link {k}: from and to are the same"));
        }
        for side in [&l.from, &l.to] {
            if !st.hosts.contains_key(side) {
                err(format!("link {k}: host «{side}» is not described"));
            }
        }
        if l.protocols.is_empty() {
            err(format!("link {k}: no protocols"));
        }
        let to_inb = st.inbounds.get(&l.to).cloned().unwrap_or_default();
        for p in &l.protocols {
            if !p.is_link() {
                err(format!("link {k}: {} cannot carry a link", p.name()));
            }
            if !to_inb.get(p).is_some_and(|i| i.enabled) {
                err(format!(
                    "link {k}: receiver «{}» has no enabled inbound {}",
                    l.to,
                    p.name()
                ));
            }
        }
        if let Some(a) = l.active {
            if !l.protocols.contains(&a) {
                err(format!(
                    "link {k}: active {} is not among the protocols",
                    a.name()
                ));
            }
        } else if l.policy == Policy::Fixed {
            err(format!("link {k}: policy fixed requires active"));
        }
        if l.protocols.contains(&Proto::Awg) && l.tunnel.is_none() {
            err(format!("link {k}: awg requires a tunnel (/30 subnet)"));
        }
        let ls = st.secrets.links.get(&k);
        if l.protocols.contains(&Proto::Awg) && ls.and_then(|s| s.awg.as_ref()).is_none() {
            err(format!("link {k}: no awg keys for both sides in secrets"));
        }
        if l.protocols.contains(&Proto::Reality) && ls.and_then(|s| s.reality.as_ref()).is_none() {
            err(format!("link {k}: no reality keys in secrets"));
        }
        if l.protocols.contains(&Proto::Hy2) && ls.and_then(|s| s.hy2_password.as_ref()).is_none() {
            err(format!("link {k}: no hy2 password in secrets"));
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
                "links {} and {}: tunnel subnets {x} and {y} overlap",
                a.key(),
                b.key()
            ));
        }
    }

    // --- users ---
    let mut peer_ips: BTreeMap<std::net::Ipv4Addr, &str> = BTreeMap::new();
    for (name, u) in &st.users {
        if !ident_ok(name) {
            err(format!("user «{name}»: name must match [a-z0-9-_]"));
        }
        if u.modes.is_empty() {
            err(format!("user «{name}»: no modes"));
        }
        if u.protocols.is_empty() {
            err(format!("user «{name}»: no protocols"));
        }
        if let Some(d) = &u.expires
            && !date_ok(d)
        {
            err(format!("user «{name}»: expires «{d}» is not YYYY-MM-DD"));
        }
        let Some(entry) = st.entry_of(u) else {
            err(format!(
                "user «{name}»: entry is not set and there is not exactly one entry"
            ));
            continue;
        };
        if !st.hosts.get(&entry).is_some_and(|h| h.role == Role::Entry) {
            err(format!(
                "user «{name}»: entry «{entry}» is not an entry host"
            ));
            continue;
        }
        let inb = st.inbounds.get(&entry).cloned().unwrap_or_default();
        for p in &u.protocols {
            if !inb.contains_key(p) {
                err(format!(
                    "user «{name}»: entry «{entry}» has no inbound {}",
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
                            "user «{name}»: awg address {} is outside the {} mode subnet ({net})",
                            peer.ip,
                            peer.mode.name()
                        ));
                    }
                    if peer.ip == net.network()
                        || peer.ip == net.hosts().next().unwrap_or(net.network())
                    {
                        err(format!(
                            "user «{name}»: awg address {} is reserved for the gateway",
                            peer.ip
                        ));
                    }
                }
                if !u.modes.contains(&peer.mode) {
                    err(format!(
                        "user «{name}»: awg peer mode {} is not among the user's modes",
                        peer.mode.name()
                    ));
                }
                if let Some(other) = peer_ips.insert(peer.ip, name) {
                    err(format!(
                        "awg address {} is used by both «{other}» and «{name}»",
                        peer.ip
                    ));
                }
                if st
                    .secrets
                    .users
                    .get(name)
                    .and_then(|s| s.awg.as_ref())
                    .is_none()
                {
                    err(format!("user «{name}»: no awg keys in secrets"));
                }
            }
            (None, true) => err(format!(
                "user «{name}»: awg protocol without a peer description (awg: {{mode, ip}})"
            )),
            (Some(_), false) => err(format!(
                "user «{name}»: an awg peer is described but the awg protocol is not set"
            )),
            (None, false) => {}
        }
        if !st.secrets.users.contains_key(name) {
            err(format!(
                "user «{name}»: no secrets (uuid, hy2_password, sub_id)"
            ));
        }
    }

    // --- routing and sets ---
    for (name, s) in &st.sets {
        if !ident_ok(name) {
            err(format!("set «{name}»: name must match [a-z0-9-_]"));
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
                err(format!("set «{name}»: domain «{d}» does not look valid"));
            }
        }
    }
    for (host, r) in &st.routing {
        let Some(h) = st.hosts.get(host) else {
            err(format!("routing/{host}.yaml: host is not described"));
            continue;
        };
        if let Some(p) = &r.exit.prefer
            && st.link(host, p).is_none()
        {
            err(format!(
                "routing/{host}.yaml: exit.prefer «{p}» but there is no link {host}->{p}"
            ));
        }
        if r.exit.policy == ExitPolicy::Fixed && r.exit.prefer.is_none() {
            err(format!(
                "routing/{host}.yaml: exit.policy fixed requires prefer"
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
                    "routing/{host}.yaml: routing rules exist only on entries"
                ));
            }
            for (i, rule) in rules.iter().enumerate() {
                let at = format!("routing/{host}.yaml: {g} #{}", i + 1);
                for s in &rule.match_.sets {
                    if !st.sets.contains_key(s) {
                        err(format!("{at}: set «{s}» is not described"));
                    }
                }
                for ib in &rule.match_.inbound {
                    if ib != "probe" && !inbound_names.contains(ib.as_str()) {
                        err(format!("{at}: inbound «{ib}» is not described on the host"));
                    }
                }
                if let Action::Link { link } = &rule.action
                    && link != "auto"
                    && st.link(host, link).is_none()
                {
                    err(format!("{at}: there is no link {host}->{link}"));
                }
                if rule.match_.is_empty() && i + 1 != rules.len() {
                    err(format!(
                        "{at}: a rule without conditions matches everything; rules below are unreachable"
                    ));
                }
            }
        }
    }
    e
}

/// Warnings: the state is applicable, but something deserves attention.
pub fn warnings(st: &State) -> Vec<String> {
    let mut w = Vec::new();
    for (name, h) in &st.hosts {
        if h.role == Role::Entry && !h.retiring && st.links_from(name).next().is_none() {
            w.push(format!(
                "host «{name}»: the entry has no link to an exit — all traffic goes direct"
            ));
        }
        if h.host_key.is_none() {
            w.push(format!(
                "host «{name}»: host key is not recorded yet (appears after bootstrap)"
            ));
        }
    }
    if !st.secrets_encrypted && !st.secrets.users.is_empty() {
        w.push("secrets are stored as plaintext: no .sops.yaml (init --age <recipient>)".into());
    }
    w
}
