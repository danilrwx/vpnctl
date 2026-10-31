//! xray config for hosts with `engine: xray`: no Hysteria2 and no DNS server.
use super::singbox::domain_parts;
use super::*;
use serde_json::{Value, json};

fn xray_domains(d: &[String]) -> Vec<String> {
    let (s, f, k, r) = domain_parts(d);
    let mut v: Vec<String> = s.iter().map(|x| format!("domain:{x}")).collect();
    v.extend(f.iter().map(|x| format!("full:{x}")));
    v.extend(k.iter().map(|x| format!("keyword:{x}")));
    v.extend(r.iter().map(|x| format!("regexp:{x}")));
    v
}

fn domains_of(st: &State, m: &Match) -> Vec<String> {
    let mut v = vec![];
    for s in &m.sets {
        if let Some(set) = st.sets.get(s) {
            v.extend(xray_domains(&set.domains));
            v.extend(set.geosite.iter().map(|g| format!("geosite:{g}")));
        }
    }
    v.extend(xray_domains(&m.domains));
    v.extend(m.geosite.iter().map(|g| format!("geosite:{g}")));
    v
}

fn ips_of(st: &State, m: &Match) -> Vec<String> {
    let mut v: Vec<String> = m.ip.iter().map(|i| i.to_string()).collect();
    for s in &m.sets {
        if let Some(set) = st.sets.get(s) {
            v.extend(set.ips.iter().map(|i| i.to_string()));
            v.extend(set.geoip.iter().map(|g| format!("geoip:{g}")));
        }
    }
    v.extend(m.geoip.iter().map(|g| format!("geoip:{g}")));
    v
}

pub fn config(st: &State, host: &str) -> Result<String> {
    let h = st.host(host)?;
    let inb = st.inbounds.get(host).cloned().unwrap_or_default();
    let hs = st.secrets.hosts.get(host).cloned().unwrap_or_default();
    let users = users_of(st, host);
    let incoming = in_links(st, host);
    let accounts = |p: Proto| -> Vec<(String, &UserSecrets)> {
        users
            .iter()
            .filter(|(_, u)| u.protocols.contains(&p))
            .flat_map(|(n, u)| {
                u.modes
                    .iter()
                    .map(move |m| (State::email(n, *m, p), &st.secrets.users[n]))
            })
            .collect()
    };
    let mut inbounds = vec![
        json!({ "tag": "api", "listen": "127.0.0.1", "port": 10085, "protocol": "dokodemo-door", "settings": { "address": "127.0.0.1" } }),
    ];
    if let Some(i) = inb.get(&Proto::Reality).filter(|i| i.enabled) {
        let keys = hs.reality.clone().context("no reality keys")?;
        let mut clients: Vec<Value> = accounts(Proto::Reality)
            .into_iter()
            .map(|(e, s)| json!({ "email": e, "id": s.uuid, "flow": "xtls-rprx-vision" }))
            .collect();
        let mut sids = keys.short_ids.clone();
        for l in &incoming {
            if let Some(r) = st
                .secrets
                .links
                .get(&l.key())
                .and_then(|s| s.reality.as_ref())
            {
                clients.push(json!({ "email": format!("link-{}", l.from), "id": r.uuid, "flow": "xtls-rprx-vision" }));
                sids.extend(r.keys.short_ids.iter().cloned());
            }
        }
        let (sni, hs_server, hs_port) = reality_target(st, host, i);
        inbounds.push(json!({ "tag": "reality", "listen": "0.0.0.0", "port": i.port, "protocol": "vless", "settings": { "clients": clients, "decryption": "none" },
            "streamSettings": { "network": "tcp", "security": "reality", "realitySettings": { "dest": format!("{hs_server}:{hs_port}"), "serverNames": [sni], "privateKey": keys.private, "shortIds": sids } },
            "sniffing": { "enabled": true, "destOverride": ["http", "tls", "quic"] } }));
    }
    if let Some(i) = inb.get(&Proto::Ws).filter(|i| i.enabled) {
        let clients: Vec<Value> = accounts(Proto::Ws)
            .into_iter()
            .map(|(e, s)| json!({ "email": e, "id": s.uuid }))
            .collect();
        inbounds.push(json!({ "tag": "ws", "listen": "127.0.0.1", "port": i.port, "protocol": "vless", "settings": { "clients": clients, "decryption": "none" },
            "streamSettings": { "network": "ws", "wsSettings": { "path": i.path.clone().unwrap_or("/ws".into()) } }, "sniffing": { "enabled": true, "destOverride": ["http", "tls"] } }));
    }
    if h.role == Role::Entry {
        inbounds.push(json!({ "tag": "probe", "listen": "127.0.0.1", "port": PROBE_PORT, "protocol": "socks", "settings": { "udp": true }, "sniffing": { "enabled": true, "destOverride": ["http", "tls", "quic"] } }));
    }

    // link outbounds and balancers
    let mut outbounds = vec![
        json!({ "tag": "direct", "protocol": "freedom" }),
        json!({ "tag": "block", "protocol": "blackhole" }),
    ];
    let mut balancers = vec![];
    let routing = st.routing.get(host).cloned().unwrap_or_default();
    let mut link_tags = vec![];
    for (i, l) in out_links(st, host) {
        let to = st.host(&l.to)?;
        let to_inb = st.inbounds.get(&l.to).cloned().unwrap_or_default();
        let ls = st.secrets.links.get(&l.key()).cloned().unwrap_or_default();
        let mut tags = vec![];
        for p in &l.protocols {
            let tag = format!("{}-{}", l.to, p.name());
            match p {
                Proto::Awg => outbounds.push(json!({ "tag": tag, "protocol": "freedom", "streamSettings": { "sockopt": { "interface": format!("awg{i}") } } })),
                Proto::Reality => {
                    let r = ls.reality.clone().context("no reality secrets for the link")?;
                    let pk = st.secrets.hosts.get(&l.to).and_then(|h| h.reality.as_ref()).map(|k| k.public.clone()).unwrap_or_default();
                    let port = to_inb.get(p).map(|x| x.port).unwrap_or(443);
                    outbounds.push(json!({ "tag": tag, "protocol": "vless", "settings": { "vnext": [{ "address": to.ip.to_string(), "port": port, "users": [{ "id": r.uuid, "encryption": "none", "flow": "xtls-rprx-vision" }] }] },
                        "streamSettings": { "network": "tcp", "security": "reality", "realitySettings": { "serverName": to_inb.get(p).map(|x| reality_target(st, &l.to, x).0).unwrap_or_default(), "fingerprint": "chrome", "publicKey": pk, "shortId": r.keys.short_ids.first().cloned().unwrap_or_default() } } }));
                }
                _ => continue, // hy2 is unavailable on xray
            }
            tags.push(tag);
        }
        let gtag = format!("link-{}", l.to);
        let selected = match (l.policy, l.active) {
            (Policy::Fixed, Some(a)) => vec![format!("{}-{}", l.to, a.name())],
            _ => tags.clone(),
        };
        balancers.push(
            json!({ "tag": gtag, "selector": selected, "strategy": { "type": "leastPing" } }),
        );
        link_tags.push(gtag);
    }
    let exit_sel: Vec<String> = match (routing.exit.policy, &routing.exit.prefer) {
        (ExitPolicy::Fixed, Some(p)) => vec![format!("{p}-")],
        _ => link_tags
            .iter()
            .map(|t| format!("{}-", t.trim_start_matches("link-")))
            .collect(),
    };
    if !link_tags.is_empty() {
        balancers.push(json!({ "tag": "exit-auto", "selector": exit_sel, "strategy": { "type": "leastPing" } }));
    }
    let subjects: Vec<String> = out_links(st, host)
        .iter()
        .map(|(_, l)| format!("{}-", l.to))
        .collect();

    let mut rules = vec![json!({ "type": "field", "inboundTag": ["api"], "outboundTag": "api" })];
    let target = |a: &Action| -> Value {
        match a {
            Action::Simple(Simple::Direct) => json!({ "outboundTag": "direct" }),
            Action::Simple(Simple::Block) => json!({ "outboundTag": "block" }),
            Action::Link { link } if link == "auto" => {
                if link_tags.is_empty() {
                    json!({ "outboundTag": "direct" })
                } else {
                    json!({ "balancerTag": "exit-auto" })
                }
            }
            Action::Link { link } => json!({ "balancerTag": format!("link-{link}") }),
        }
    };
    for (group, mode) in [
        (&routing.groups.all, None),
        (&routing.groups.split, Some(Mode::Split)),
        (&routing.groups.full, Some(Mode::Full)),
    ] {
        let scope: Option<Vec<String>> = mode.map(|m| {
            users
                .iter()
                .filter(|(_, u)| u.modes.contains(&m))
                .flat_map(|(n, u)| {
                    u.protocols
                        .iter()
                        .filter(|p| p.is_engine_user())
                        .map(move |p| State::email(n, m, *p))
                })
                .collect()
        });
        if scope.as_ref().is_some_and(|s| s.is_empty()) {
            continue;
        }
        for r in group.iter().filter(|r| r.enabled) {
            let mut kinds: Vec<Value> = vec![];
            let d = domains_of(st, &r.match_);
            if !d.is_empty() {
                kinds.push(json!({ "domain": d }));
            }
            let ip = ips_of(st, &r.match_);
            if !ip.is_empty() {
                kinds.push(json!({ "ip": ip }));
            }
            if !r.match_.inbound.is_empty() {
                kinds.push(json!({ "inboundTag": r.match_.inbound }));
            }
            if !r.match_.port.is_empty() {
                kinds.push(json!({ "port": r.match_.port.iter().map(|p| p.to_string()).collect::<Vec<_>>().join(",") }));
            }
            if kinds.is_empty() {
                kinds.push(json!({}));
            }
            for k in kinds {
                let mut rule = json!({ "type": "field" });
                let o = rule.as_object_mut().unwrap();
                o.extend(k.as_object().unwrap().clone());
                if let Some(s) = &scope {
                    o.insert("user".into(), json!(s));
                }
                o.extend(target(&r.action).as_object().unwrap().clone());
                rules.push(rule);
            }
        }
    }
    let cfg = json!({
        "log": { "loglevel": "warning" },
        "api": { "tag": "api", "services": ["StatsService"] },
        "stats": {},
        "policy": { "levels": { "0": { "statsUserUplink": true, "statsUserDownlink": true } }, "system": { "statsOutboundUplink": true, "statsOutboundDownlink": true } },
        "inbounds": inbounds,
        "outbounds": outbounds,
        "routing": { "domainStrategy": "IPIfNonMatch", "rules": rules, "balancers": balancers },
        "observatory": { "subjectSelector": subjects, "probeUrl": PROBE_URL, "probeInterval": format!("{}s", routing.exit.urltest.interval_s) }
    });
    Ok(serde_json::to_string_pretty(&cfg)? + "\n")
}
