//! sing-box config (schema 1.12) from the state.
use super::*;
use serde_json::{Value, json};

/// Domains in xray syntax -> (suffix, full, keyword, regex)
pub fn domain_parts(domains: &[String]) -> (Vec<String>, Vec<String>, Vec<String>, Vec<String>) {
    let (mut s, mut f, mut k, mut r) = (vec![], vec![], vec![], vec![]);
    for d in domains {
        match d.split_once(':') {
            Some(("full", b)) => f.push(b.to_string()),
            Some(("keyword", b)) => k.push(b.to_string()),
            Some(("regexp", b)) => r.push(b.to_string()),
            Some(("domain", b)) => s.push(b.to_string()),
            _ => s.push(d.clone()),
        }
    }
    (s, f, k, r)
}

fn headless(domains: &[String], ips: &[ipnet::IpNet]) -> Value {
    let (s, f, k, r) = domain_parts(domains);
    let mut o = serde_json::Map::new();
    if !s.is_empty() {
        o.insert("domain_suffix".into(), json!(s));
    }
    if !f.is_empty() {
        o.insert("domain".into(), json!(f));
    }
    if !k.is_empty() {
        o.insert("domain_keyword".into(), json!(k));
    }
    if !r.is_empty() {
        o.insert("domain_regex".into(), json!(r));
    }
    if !ips.is_empty() {
        o.insert(
            "ip_cidr".into(),
            json!(ips.iter().map(|i| i.to_string()).collect::<Vec<_>>()),
        );
    }
    Value::Object(o)
}

/// Local rule sets for our own set entries: `/etc/sing-box/rules/<set>.json`.
pub fn rule_sets(st: &State, host: &str) -> Result<Vec<(String, String)>> {
    let mut out = vec![];
    for (name, s) in &st.sets {
        if used_sets(st, host).contains(name) && (!s.domains.is_empty() || !s.ips.is_empty()) {
            out.push((
                name.clone(),
                serde_json::to_string_pretty(
                    &json!({ "version": 3, "rules": [headless(&s.domains, &s.ips)] }),
                )? + "\n",
            ));
        }
    }
    Ok(out)
}

fn used_sets(st: &State, host: &str) -> Vec<String> {
    let Some(r) = st.routing.get(host) else {
        return vec![];
    };
    let mut v: Vec<String> = [&r.groups.all, &r.groups.split, &r.groups.full]
        .iter()
        .flat_map(|g| g.iter())
        .flat_map(|r| r.match_.sets.clone())
        .collect();
    v.sort();
    v.dedup();
    v
}

/// Rule set tags for a set: our entries + geosite + geoip.
fn set_tags(st: &State, set: &str) -> Vec<String> {
    let Some(s) = st.sets.get(set) else {
        return vec![];
    };
    let mut t = vec![];
    if !s.domains.is_empty() || !s.ips.is_empty() {
        t.push(format!("set-{set}"));
    }
    t.extend(s.geosite.iter().map(|g| format!("geosite-{g}")));
    t.extend(s.geoip.iter().map(|g| format!("geoip-{g}")));
    t
}

struct RuleSets(BTreeMap<String, Value>);

impl RuleSets {
    fn add(&mut self, tag: &str) {
        if self.0.contains_key(tag) {
            return;
        }
        let v = if let Some(set) = tag.strip_prefix("set-") {
            json!({ "type": "local", "tag": tag, "format": "source", "path": format!("rules/{set}.json") })
        } else {
            json!({ "type": "local", "tag": tag, "format": "binary", "path": format!("rules/{tag}.srs") })
        };
        self.0.insert(tag.into(), v);
    }
}

/// Rule condition -> sing-box: a single condition or logical/or per condition kind.
fn condition(st: &State, m: &Match, rs: &mut RuleSets) -> Option<Value> {
    let mut parts = vec![];
    let mut tags: Vec<String> = m.sets.iter().flat_map(|s| set_tags(st, s)).collect();
    tags.extend(m.geosite.iter().map(|g| format!("geosite-{g}")));
    tags.extend(
        m.geoip
            .iter()
            .filter(|g| *g != "private")
            .map(|g| format!("geoip-{g}")),
    );
    tags.sort();
    tags.dedup();
    if !tags.is_empty() {
        for t in &tags {
            rs.add(t);
        }
        parts.push(json!({ "rule_set": tags }));
    }
    if m.geoip.iter().any(|g| g == "private") {
        parts.push(json!({ "ip_is_private": true }));
    }
    if !m.domains.is_empty() || !m.ip.is_empty() {
        parts.push(headless(&m.domains, &m.ip));
    }
    if !m.inbound.is_empty() {
        parts.push(json!({ "inbound": m.inbound }));
    }
    if !m.port.is_empty() {
        parts.push(json!({ "port": m.port }));
    }
    match parts.len() {
        0 => None,
        1 => parts.pop(),
        _ => Some(json!({ "type": "logical", "mode": "or", "rules": parts })),
    }
}

fn merge(a: Value, b: Value) -> Value {
    let (Value::Object(mut a), Value::Object(b)) = (a, b) else {
        unreachable!()
    };
    a.extend(b);
    Value::Object(a)
}

fn action_of(a: &Action, exit_tag: &str) -> Value {
    match a {
        Action::Simple(Simple::Direct) => json!({ "outbound": "direct" }),
        Action::Simple(Simple::Block) => json!({ "action": "reject" }),
        Action::Link { link } if link == "auto" => json!({ "outbound": exit_tag }),
        Action::Link { link } => json!({ "outbound": format!("link-{link}") }),
    }
}

/// Group scope: accounts of the mode OR the L3 peer subnet of the mode.
fn scope(st: &State, host: &str, mode: Mode, users: &[(String, User)]) -> Option<Value> {
    let emails: Vec<String> = users
        .iter()
        .filter(|(_, u)| u.modes.contains(&mode))
        .flat_map(|(n, u)| {
            u.protocols
                .iter()
                .filter(|p| p.is_engine_user())
                .map(move |p| State::email(n, mode, *p))
        })
        .collect();
    let subnet = st
        .inbounds
        .get(host)
        .and_then(|m| m.get(&Proto::Awg))
        .filter(|i| i.enabled)
        .and_then(|i| i.subnets.as_ref())
        .map(|s| s.for_mode(mode).to_string());
    let mut parts = vec![];
    if !emails.is_empty() {
        parts.push(json!({ "auth_user": emails }));
    }
    if let Some(s) = subnet {
        parts.push(json!({ "source_ip_cidr": [s] }));
    }
    match parts.len() {
        0 => None,
        1 => parts.pop(),
        _ => Some(json!({ "type": "logical", "mode": "or", "rules": parts })),
    }
}

fn link_outbounds(st: &State, host: &str) -> Result<(Vec<Value>, String)> {
    let mut outs = vec![];
    let mut groups = vec![];
    let routing = st.routing.get(host).cloned().unwrap_or_default();
    for (i, l) in out_links(st, host) {
        let to = st.host(&l.to)?;
        let to_inb = st.inbounds.get(&l.to).cloned().unwrap_or_default();
        let ls = st.secrets.links.get(&l.key()).cloned().unwrap_or_default();
        let mut tags = vec![];
        let mut protos = l.protocols.clone();
        if let Some(a) = l.active {
            protos.retain(|p| *p != a);
            protos.insert(0, a);
        }
        for p in protos {
            let tag = format!("{}-{}", l.to, p.name());
            let port = to_inb.get(&p).map(|x| x.port).unwrap_or(443);
            let v = match p {
                Proto::Awg => {
                    json!({ "type": "direct", "tag": tag, "bind_interface": format!("awg{i}") })
                }
                Proto::Reality => {
                    let r = ls
                        .reality
                        .clone()
                        .context("no reality secrets for the link")?;
                    let pk = st
                        .secrets
                        .hosts
                        .get(&l.to)
                        .and_then(|h| h.reality.as_ref())
                        .map(|k| k.public.clone())
                        .unwrap_or_default();
                    json!({ "type": "vless", "tag": tag, "server": to.ip.to_string(), "server_port": port, "uuid": r.uuid, "flow": "xtls-rprx-vision",
                        "tls": { "enabled": true, "server_name": to_inb.get(&p).map(|x| reality_target(st, &l.to, x).0).unwrap_or_default(), "utls": { "enabled": true, "fingerprint": "chrome" },
                                 "reality": { "enabled": true, "public_key": pk, "short_id": r.keys.short_ids.first().cloned().unwrap_or_default() } } })
                }
                Proto::Hy2 => {
                    json!({ "type": "hysteria2", "tag": tag, "server": to.ip.to_string(), "server_port": port, "password": ls.hy2_password.clone().unwrap_or_default(),
                    "tls": { "enabled": true, "server_name": l.to, "insecure": true } })
                }
                _ => continue,
            };
            outs.push(v);
            tags.push(tag);
        }
        let gtag = format!("link-{}", l.to);
        let g = match l.policy {
            Policy::Fixed => {
                json!({ "type": "selector", "tag": gtag, "outbounds": tags, "default": tags[0], "interrupt_exist_connections": true })
            }
            // rr: sing-box has no round-robin, behave like auto
            Policy::Auto | Policy::Rr => {
                json!({ "type": "urltest", "tag": gtag, "outbounds": tags, "url": PROBE_URL, "interval": format!("{}s", l.urltest.interval_s), "tolerance": l.urltest.tolerance_ms })
            }
        };
        groups.push((gtag, g));
    }
    let exit_tag = if groups.is_empty() {
        "direct".to_string()
    } else {
        let mut tags: Vec<String> = groups.iter().map(|(t, _)| t.clone()).collect();
        if let Some(p) = &routing.exit.prefer {
            let pt = format!("link-{p}");
            if let Some(pos) = tags.iter().position(|t| *t == pt) {
                tags.remove(pos);
                tags.insert(0, pt);
            }
        }
        let g = match routing.exit.policy {
            ExitPolicy::Fixed => {
                json!({ "type": "selector", "tag": "exit-auto", "outbounds": tags, "default": tags[0], "interrupt_exist_connections": true })
            }
            ExitPolicy::Auto => {
                json!({ "type": "urltest", "tag": "exit-auto", "outbounds": tags, "url": PROBE_URL, "interval": format!("{}s", routing.exit.urltest.interval_s), "tolerance": routing.exit.urltest.tolerance_ms })
            }
        };
        groups.push(("exit-auto".into(), g));
        "exit-auto".to_string()
    };
    outs.extend(groups.into_iter().map(|(_, g)| g));
    Ok((outs, exit_tag))
}

pub fn config(st: &State, host: &str) -> Result<String> {
    let h = st.host(host)?;
    let inb = st.inbounds.get(host).cloned().unwrap_or_default();
    let hs = st.secrets.hosts.get(host).cloned().unwrap_or_default();
    let users = users_of(st, host);
    let (cert, key) = cert_paths(st, host);
    let mut inbounds = vec![];
    let mut emails = vec![];

    let user_accounts = |p: Proto| -> Vec<(String, &UserSecrets)> {
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
    let incoming = in_links(st, host);

    if let Some(i) = inb.get(&Proto::Reality).filter(|i| i.enabled) {
        let keys = hs.reality.clone().context("no reality keys")?;
        let mut us: Vec<Value> = user_accounts(Proto::Reality)
            .into_iter()
            .map(|(e, s)| json!({ "name": e, "uuid": s.uuid, "flow": "xtls-rprx-vision" }))
            .collect();
        let mut sids = keys.short_ids.clone();
        for l in &incoming {
            if let Some(r) = st
                .secrets
                .links
                .get(&l.key())
                .and_then(|s| s.reality.as_ref())
            {
                us.push(json!({ "name": format!("link-{}", l.from), "uuid": r.uuid, "flow": "xtls-rprx-vision" }));
                sids.extend(r.keys.short_ids.iter().cloned());
            }
        }
        emails.extend(us.iter().map(|u| u["name"].as_str().unwrap().to_string()));
        let (sni, hs_server, hs_port) = reality_target(st, host, i);
        inbounds.push(json!({ "type": "vless", "tag": "reality", "listen": "::", "listen_port": i.port, "users": us,
            "tls": { "enabled": true, "server_name": sni, "reality": { "enabled": true, "handshake": { "server": hs_server, "server_port": hs_port }, "private_key": keys.private, "short_id": sids } } }));
    }
    if let Some(i) = inb.get(&Proto::Ws).filter(|i| i.enabled) {
        let us: Vec<Value> = user_accounts(Proto::Ws)
            .into_iter()
            .map(|(e, s)| json!({ "name": e, "uuid": s.uuid }))
            .collect();
        emails.extend(us.iter().map(|u| u["name"].as_str().unwrap().to_string()));
        inbounds.push(json!({ "type": "vless", "tag": "ws", "listen": "127.0.0.1", "listen_port": i.port, "users": us, "transport": { "type": "ws", "path": i.path.clone().unwrap_or("/ws".into()) } }));
    }
    if let Some(i) = inb.get(&Proto::Hy2).filter(|i| i.enabled) {
        let mut us: Vec<Value> = user_accounts(Proto::Hy2)
            .into_iter()
            .map(|(e, s)| json!({ "name": e, "password": s.hy2_password }))
            .collect();
        for l in &incoming {
            if let Some(p) = st
                .secrets
                .links
                .get(&l.key())
                .and_then(|s| s.hy2_password.clone())
            {
                us.push(json!({ "name": format!("link-{}", l.from), "password": p }));
            }
        }
        emails.extend(us.iter().map(|u| u["name"].as_str().unwrap().to_string()));
        inbounds.push(json!({ "type": "hysteria2", "tag": "hy2", "listen": "::", "listen_port": i.port, "users": us, "tls": { "enabled": true, "certificate_path": cert, "key_path": key } }));
    }
    if h.role == Role::Entry {
        if inb.get(&Proto::Awg).is_some_and(|i| i.enabled) {
            inbounds.push(json!({ "type": "tproxy", "tag": "tproxy", "listen": "127.0.0.1", "listen_port": TPROXY_PORT }));
        }
        inbounds.push(json!({ "type": "socks", "tag": "probe", "listen": "127.0.0.1", "listen_port": PROBE_PORT }));
    }

    emails.sort();
    emails.dedup();
    let (mut outbounds, exit_tag) = link_outbounds(st, host)?;
    outbounds.insert(0, json!({ "type": "direct", "tag": "direct" }));

    let mut rs = RuleSets(BTreeMap::new());
    let mut rules = vec![
        json!({ "action": "sniff" }),
        json!({ "protocol": "dns", "action": "hijack-dns" }),
    ];
    let mut direct_tags = vec![];
    let routing = st.routing.get(host).cloned().unwrap_or_default();
    for (group, mode) in [
        (&routing.groups.all, None),
        (&routing.groups.split, Some(Mode::Split)),
        (&routing.groups.full, Some(Mode::Full)),
    ] {
        let sc = match mode {
            None => None,
            Some(m) => match scope(st, host, m, &users) {
                Some(s) => Some(s),
                None => continue, // nobody in this mode, no rules needed
            },
        };
        for r in group.iter().filter(|r| r.enabled) {
            let cond = condition(st, &r.match_, &mut rs);
            if r.action == Action::Simple(Simple::Direct)
                && let Some(Value::Object(c)) = &cond
                && let Some(Value::Array(t)) = c.get("rule_set")
            {
                direct_tags.extend(t.iter().filter_map(|x| x.as_str().map(String::from)));
            }
            let m = match (sc.clone(), cond) {
                (None, None) => json!({}),
                (Some(s), None) => s,
                (None, Some(c)) => c,
                (Some(s), Some(c)) => json!({ "type": "logical", "mode": "and", "rules": [s, c] }),
            };
            rules.push(merge(m, action_of(&r.action, &exit_tag)));
        }
    }

    let dns = if h.role == Role::Entry {
        let mut servers: Vec<Value> = routing.dns.direct.iter().enumerate().map(|(i, s)| json!({ "type": "udp", "tag": format!("dns-direct-{}", i + 1), "server": s.to_string() })).collect();
        let (proto, server) = routing
            .dns
            .remote
            .split_once("://")
            .unwrap_or(("udp", routing.dns.remote.as_str()));
        let (server, path) = server
            .split_once('/')
            .map(|(s, p)| (s.to_string(), format!("/{p}")))
            .unwrap_or((server.to_string(), "/dns-query".into()));
        let mut remote = json!({ "type": proto, "tag": "dns-remote", "server": server });
        // An unconfigured "direct" is an empty outbound: sing-box 1.12 rejects a detour to it
        if exit_tag != "direct" {
            remote["detour"] = json!(exit_tag);
        }
        if proto == "https" {
            remote["path"] = json!(path);
        }
        servers.push(remote);
        let mut drules = vec![];
        direct_tags.sort();
        direct_tags.dedup();
        direct_tags.retain(|t| t.starts_with("geosite-") || t.starts_with("set-"));
        if !direct_tags.is_empty() && !routing.dns.direct.is_empty() {
            drules.push(json!({ "rule_set": direct_tags, "server": "dns-direct-1" }));
        }
        json!({ "servers": servers, "rules": drules, "final": "dns-remote", "strategy": "prefer_ipv4" })
    } else {
        // An exit does not need a public resolver: UDP/53 is often blocked by the provider, while REALITY
        // must resolve its handshake domain. The system resolver works via DHCP settings.
        json!({ "servers": [{ "type": "local", "tag": "dns-direct-1" }], "final": "dns-direct-1" })
    };

    let cfg = json!({
        "log": { "level": "info", "timestamp": true },
        "dns": dns,
        "inbounds": inbounds,
        "outbounds": outbounds,
        "route": { "rules": rules, "rule_set": rs.0.values().collect::<Vec<_>>(), "final": "direct", "auto_detect_interface": true },
        // the official sing-box build has no v2ray_api; clash_api is available
        "experimental": { "clash_api": { "external_controller": STATS_API } }
    });
    Ok(serde_json::to_string_pretty(&cfg)? + "\n")
}
