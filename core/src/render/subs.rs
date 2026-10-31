//! User subscriptions and links: base64 list, sing-box JSON, Clash YAML, tg://proxy.
use super::*;
use base64::Engine as _;
use serde_json::{Value, json};

pub struct Acc<'a> {
    pub proto: Proto,
    pub email: String,
    pub domain: &'a str,
    pub port: u16,
    pub sni: String,
    pub path: String,
    pub uuid: &'a str,
    pub password: &'a str,
    pub pbk: String,
    pub sid: String,
}

/// User accounts in a mode on the entry: only enabled engine inbounds.
pub fn accounts<'a>(
    st: &'a State,
    entry: &str,
    user: &str,
    u: &User,
    mode: Mode,
) -> Result<Vec<Acc<'a>>> {
    let h = st.host(entry)?;
    let domain = h.domain.as_deref().context("the entry has no domain")?;
    let inb = st.inbounds.get(entry).cloned().unwrap_or_default();
    let hs = st.secrets.hosts.get(entry).cloned().unwrap_or_default();
    let us = st
        .secrets
        .users
        .get(user)
        .with_context(|| format!("no secrets for user {user}"))?;
    let mut v = vec![];
    for p in [Proto::Reality, Proto::Ws, Proto::Hy2] {
        if !u.protocols.contains(&p) {
            continue;
        }
        let Some(i) = inb.get(&p).filter(|i| i.enabled) else {
            continue;
        };
        let r = hs.reality.clone().unwrap_or_default();
        v.push(Acc {
            proto: p,
            email: State::email(user, mode, p),
            domain,
            // ws goes through the web server on 443, the others use their own port
            port: if p == Proto::Ws { 443 } else { i.port },
            sni: match p {
                Proto::Reality => reality_target(st, entry, i).0,
                _ => domain.to_string(),
            },
            path: i.path.clone().unwrap_or("/ws".into()),
            uuid: &us.uuid,
            password: &us.hy2_password,
            pbk: r.public,
            sid: r.short_ids.first().cloned().unwrap_or_default(),
        });
    }
    Ok(v)
}

fn enc(s: &str) -> String {
    s.replace('/', "%2F").replace(':', "%3A")
}

pub fn links(st: &State, entry: &str, user: &str, u: &User, mode: Mode) -> Result<Vec<String>> {
    Ok(accounts(st, entry, user, u, mode)?
        .iter()
        .map(|a| match a.proto {
            Proto::Reality => format!("vless://{}@{}:{}?encryption=none&flow=xtls-rprx-vision&security=reality&sni={}&fp=chrome&pbk={}&sid={}&type=tcp#{}", a.uuid, a.domain, a.port, a.sni, a.pbk, a.sid, a.email),
            Proto::Ws => format!("vless://{}@{}:{}?encryption=none&security=tls&sni={}&type=ws&host={}&path={}#{}", a.uuid, a.domain, a.port, a.domain, a.domain, enc(&a.path), a.email),
            Proto::Hy2 => format!("hysteria2://{}@{}:{}?sni={}#{}", a.password, a.domain, a.port, a.sni, a.email),
            _ => unreachable!(),
        })
        .collect())
}

pub fn base64_list(links: &[String]) -> String {
    base64::engine::general_purpose::STANDARD.encode(links.join("\n")) + "\n"
}

pub fn singbox_json(st: &State, entry: &str, user: &str, u: &User, mode: Mode) -> Result<String> {
    let accs = accounts(st, entry, user, u, mode)?;
    let mut outs: Vec<Value> = accs
        .iter()
        .map(|a| match a.proto {
            Proto::Reality => json!({ "type": "vless", "tag": a.email, "server": a.domain, "server_port": a.port, "uuid": a.uuid, "flow": "xtls-rprx-vision",
                "tls": { "enabled": true, "server_name": a.sni, "utls": { "enabled": true, "fingerprint": "chrome" }, "reality": { "enabled": true, "public_key": a.pbk, "short_id": a.sid } } }),
            Proto::Ws => json!({ "type": "vless", "tag": a.email, "server": a.domain, "server_port": a.port, "uuid": a.uuid,
                "tls": { "enabled": true, "server_name": a.domain }, "transport": { "type": "ws", "path": a.path, "headers": { "Host": a.domain } } }),
            Proto::Hy2 => json!({ "type": "hysteria2", "tag": a.email, "server": a.domain, "server_port": a.port, "password": a.password, "tls": { "enabled": true, "server_name": a.domain } }),
            _ => unreachable!(),
        })
        .collect();
    let tags: Vec<String> = accs.iter().map(|a| a.email.clone()).collect();
    outs.insert(0, json!({ "type": "urltest", "tag": format!("{user}-{}", mode.name()), "outbounds": tags, "url": PROBE_URL, "interval": "60s" }));
    outs.push(json!({ "type": "direct", "tag": "direct" }));
    let cfg = json!({ "outbounds": outs, "route": { "final": format!("{user}-{}", mode.name()), "auto_detect_interface": true } });
    Ok(serde_json::to_string_pretty(&cfg)? + "\n")
}

pub fn clash_yaml(st: &State, entry: &str, user: &str, u: &User, mode: Mode) -> Result<String> {
    let accs = accounts(st, entry, user, u, mode)?;
    let proxies: Vec<Value> = accs
        .iter()
        .map(|a| match a.proto {
            Proto::Reality => json!({ "name": a.email, "type": "vless", "server": a.domain, "port": a.port, "uuid": a.uuid, "network": "tcp", "tls": true, "udp": true, "flow": "xtls-rprx-vision",
                "servername": a.sni, "client-fingerprint": "chrome", "reality-opts": { "public-key": a.pbk, "short-id": a.sid } }),
            Proto::Ws => json!({ "name": a.email, "type": "vless", "server": a.domain, "port": a.port, "uuid": a.uuid, "network": "ws", "tls": true, "udp": true,
                "servername": a.domain, "ws-opts": { "path": a.path, "headers": { "Host": a.domain } } }),
            Proto::Hy2 => json!({ "name": a.email, "type": "hysteria2", "server": a.domain, "port": a.port, "password": a.password, "sni": a.domain }),
            _ => unreachable!(),
        })
        .collect();
    let names: Vec<String> = accs.iter().map(|a| a.email.clone()).collect();
    let group = format!("{user}-{}", mode.name());
    let cfg = json!({ "proxies": proxies, "proxy-groups": [{ "name": group, "type": "url-test", "proxies": names, "url": PROBE_URL, "interval": 60 }], "rules": [format!("MATCH,{group}")] });
    Ok(serde_yaml::to_string(&cfg)?)
}

/// Public subscription link.
pub fn sub_url(st: &State, entry: &str, user: &str, mode: Mode) -> Result<String> {
    let domain = st
        .host(entry)?
        .domain
        .as_deref()
        .context("the entry has no domain")?;
    let us = st
        .secrets
        .users
        .get(user)
        .with_context(|| format!("no secrets for user {user}"))?;
    Ok(format!("https://{domain}/s/{}/{}", us.sub_id, mode.name()))
}

pub fn tg_link(st: &State, entry: &str) -> Option<String> {
    let h = st.hosts.get(entry)?;
    let port = st
        .inbounds
        .get(entry)?
        .get(&Proto::Mtg)
        .filter(|i| i.enabled)?
        .port;
    let secret = st.secrets.hosts.get(entry)?.mtg_secret.as_ref()?;
    Some(format!(
        "tg://proxy?server={}&port={port}&secret={secret}",
        h.domain.as_ref()?
    ))
}

pub fn qr(text: &str) -> String {
    match qrcode::QrCode::new(text) {
        Ok(c) => c
            .render::<qrcode::render::unicode::Dense1x2>()
            .quiet_zone(true)
            .build(),
        Err(_) => "(too long for a QR code)".into(),
    }
}
