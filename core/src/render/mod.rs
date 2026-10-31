//! Deterministic config rendering from the state. A pure function: no time, no randomness,
//! JSON keys are sorted, ordering comes from BTreeMaps and state files.
pub mod singbox;
pub mod subs;
pub mod xray;

use crate::state::*;
use anyhow::{Context, Result, bail};
use minijinja::{Environment, context};
use sha2::Digest;
use std::collections::BTreeMap;
use std::path::Path;

/// path on the host -> contents
pub type HostFiles = BTreeMap<String, String>;

#[derive(Debug, Default, PartialEq, Eq)]
pub struct Rendered {
    pub hosts: BTreeMap<String, HostFiles>,
}

pub const TEMPLATES: &[(&str, &str)] = &[
    ("Caddyfile.j2", include_str!("../templates/Caddyfile.j2")),
    ("awg.conf.j2", include_str!("../templates/awg.conf.j2")),
    (
        "awg-client.conf.j2",
        include_str!("../templates/awg-client.conf.j2"),
    ),
    ("mtg.toml.j2", include_str!("../templates/mtg.toml.j2")),
    (
        "nftables.conf.j2",
        include_str!("../templates/nftables.conf.j2"),
    ),
    (
        "sysctl.conf.j2",
        include_str!("../templates/sysctl.conf.j2"),
    ),
    (
        "engine.service.j2",
        include_str!("../templates/engine.service.j2"),
    ),
    (
        "mtg.service.j2",
        include_str!("../templates/mtg.service.j2"),
    ),
    (
        "bootstrap.sh.j2",
        include_str!("../templates/bootstrap.sh.j2"),
    ),
];

pub const TPROXY_PORT: u16 = 7893;
/// Caddy listens here on 127.0.0.1: cover site, ws, subscriptions; Reality falls back to it.
pub const CADDY_PORT: u16 = 8443;

/// Reality SNI and where a non-Reality handshake goes: our own site behind Caddy (entry with a domain)
/// or an external site from the inbound `sni`.
pub fn reality_target(st: &State, host: &str, i: &Inbound) -> (String, String, u16) {
    match (&i.sni, st.hosts.get(host).and_then(|h| h.domain.clone())) {
        (Some(s), _) => match i
            .handshake
            .as_deref()
            .and_then(|h| h.rsplit_once(':'))
            .and_then(|(h, p)| p.parse::<u16>().ok().map(|p| (h.to_string(), p)))
        {
            Some((h, p)) => (s.clone(), h, p),
            None => (s.clone(), s.clone(), 443),
        },
        (None, Some(d)) => (d, "127.0.0.1".into(), CADDY_PORT),
        (None, None) => ("www.microsoft.com".into(), "www.microsoft.com".into(), 443),
    }
}
pub const PROBE_PORT: u16 = 1080;
pub const STATS_API: &str = "127.0.0.1:9090";
pub const PROBE_URL: &str = "https://www.gstatic.com/generate_204";

/// Templates: from the state's `templates/` when present, built-in otherwise.
pub fn env(st: &State) -> Result<Environment<'static>> {
    let mut env = Environment::new();
    for (name, body) in TEMPLATES {
        let p = st.dir.join("templates").join(name);
        let src = if p.exists() {
            std::fs::read_to_string(&p)?
        } else {
            body.to_string()
        };
        env.add_template_owned(name.to_string(), src)?;
    }
    Ok(env)
}

/// Copies built-in templates into `templates/` (for `init`); existing files are left alone.
pub fn install_templates(dir: &Path) -> Result<()> {
    let d = dir.join("templates");
    std::fs::create_dir_all(&d)?;
    for (name, body) in TEMPLATES {
        let p = d.join(name);
        if !p.exists() {
            std::fs::write(p, body)?;
        }
    }
    Ok(())
}

pub fn sha256(s: &str) -> String {
    sha2::Sha256::digest(s.as_bytes())
        .iter()
        .map(|b| format!("{b:02x}"))
        .collect()
}

pub fn engine_bin(e: Engine) -> &'static str {
    match e {
        Engine::SingBox => "sing-box",
        Engine::Xray => "xray",
    }
}

pub fn engine_config_path(e: Engine) -> String {
    format!("/etc/{}/config.json", engine_bin(e))
}

/// Outgoing links of the host with the awg interface number (awg0 is clients and incoming links).
pub fn out_links<'a>(st: &'a State, host: &str) -> Vec<(usize, &'a Link)> {
    st.links
        .iter()
        .filter(|l| l.from == host)
        .enumerate()
        .map(|(i, l)| (i + 1, l))
        .collect()
}

pub fn in_links<'a>(st: &'a State, host: &str) -> Vec<&'a Link> {
    st.links.iter().filter(|l| l.to == host).collect()
}

pub fn users_of(st: &State, entry: &str) -> Vec<(String, User)> {
    st.users
        .iter()
        .filter(|(_, u)| u.enabled && st.entry_of(u).as_deref() == Some(entry))
        .map(|(n, u)| (n.clone(), u.clone()))
        .collect()
}

pub fn cert_paths(st: &State, host: &str) -> (String, String) {
    let h = &st.hosts[host];
    match (&h.domain, h.role) {
        (Some(d), Role::Entry) => {
            let base = format!(
                "/var/lib/caddy/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/{d}/{d}"
            );
            (format!("{base}.crt"), format!("{base}.key"))
        }
        _ => ("/etc/vpnctl/self.crt".into(), "/etc/vpnctl/self.key".into()),
    }
}

/// Gateway address inside the client subnet (the first address).
pub fn gateway(net: ipnet::Ipv4Net) -> std::net::Ipv4Addr {
    net.hosts().next().unwrap_or(net.network())
}

fn tunnel_addr(t: ipnet::Ipv4Net, side: u8) -> String {
    let mut h = t.hosts();
    let a = if side == 1 { h.next() } else { h.nth(1) };
    format!("{}/30", a.unwrap_or(t.network()))
}

pub fn render_host(st: &State, name: &str) -> Result<HostFiles> {
    let host = st.host(name)?;
    let env = env(st)?;
    let inb = st.inbounds.get(name).cloned().unwrap_or_default();
    let hs = st.secrets.hosts.get(name).cloned().unwrap_or_default();
    let mut f = HostFiles::new();
    let r = |t: &str, ctx: minijinja::Value| -> Result<String> {
        let mut s = env.get_template(t)?.render(ctx)?;
        if !s.ends_with('\n') {
            s.push('\n');
        }
        Ok(s)
    };

    // engine
    match host.engine {
        Engine::SingBox => {
            f.insert(
                engine_config_path(Engine::SingBox),
                singbox::config(st, name)?,
            );
            for (set, body) in singbox::rule_sets(st, name)? {
                f.insert(format!("/etc/sing-box/rules/{set}.json"), body);
            }
        }
        Engine::Xray => {
            f.insert(engine_config_path(Engine::Xray), xray::config(st, name)?);
        }
    }
    let bin = engine_bin(host.engine);
    // sing-box understands -D (resource dir), xray does not: it only needs -c
    let exec = match host.engine {
        Engine::SingBox => {
            format!("/usr/local/bin/{bin} run -D /etc/{bin} -c /etc/{bin}/config.json")
        }
        Engine::Xray => format!("/usr/local/bin/{bin} run -c /etc/{bin}/config.json"),
    };
    f.insert(
        format!("/etc/systemd/system/{bin}.service"),
        r(
            "engine.service.j2",
            context! { host => name, engine => bin, exec => exec },
        )?,
    );

    // web server and subscriptions
    if host.role == Role::Entry {
        let domain = host.domain.clone().context("the entry has no domain")?;
        if let Some(ws) = inb.get(&Proto::Ws) {
            f.insert(
                "/etc/caddy/Caddyfile".into(),
                r("Caddyfile.j2", context! { host => name, domain => domain.clone(), caddy_port => CADDY_PORT, ws_path => ws.path.clone().unwrap_or("/ws".into()), ws_port => ws.port, profile_title => domain.clone() })?,
            );
        }
        for (uname, u) in users_of(st, name) {
            let us = st
                .secrets
                .users
                .get(&uname)
                .with_context(|| format!("no secrets for user {uname}"))?;
            for m in &u.modes {
                let links = subs::links(st, name, &uname, &u, *m)?;
                let base = format!("/var/www/sub/{}/{}", us.sub_id, m.name());
                f.insert(base.clone(), subs::base64_list(&links));
                f.insert(
                    format!("{base}.json"),
                    subs::singbox_json(st, name, &uname, &u, *m)?,
                );
                f.insert(
                    format!("{base}.yaml"),
                    subs::clash_yaml(st, name, &uname, &u, *m)?,
                );
            }
            if let (Some(peer), Some(awg), Some(keys)) =
                (&u.awg, inb.get(&Proto::Awg), us.awg.as_ref())
            {
                let net = awg
                    .subnets
                    .as_ref()
                    .context("awg without subnets")?
                    .for_mode(peer.mode);
                f.insert(
                    format!("/etc/vpnctl/profiles/{uname}/awg.conf"),
                    r("awg-client.conf.j2", context! { user => uname, mode => peer.mode.name(), domain => domain.clone(), address => peer.ip.to_string(), private_key => keys.private.clone(), dns => gateway(net).to_string(), server_public_key => hs.awg.as_ref().map(|k| k.public.clone()).unwrap_or_default(), preshared => us.awg_preshared.clone().unwrap_or_default(), port => awg.port })?,
                );
            }
        }
        if let Some(mtg) = inb.get(&Proto::Mtg) {
            f.insert("/etc/mtg/config.toml".into(), r("mtg.toml.j2", context! { host => name, secret => hs.mtg_secret.clone().unwrap_or_default(), port => mtg.port, socks_port => PROBE_PORT })?);
            f.insert(
                "/etc/systemd/system/mtg.service".into(),
                r("mtg.service.j2", context! { host => name })?,
            );
        }
    }

    // AWG: awg0 is clients (entry) and incoming links; awgN are outgoing links
    let mut awg_ifaces = vec![];
    let mut masquerade = vec![];
    let incoming: Vec<&Link> = in_links(st, name)
        .into_iter()
        .filter(|l| l.protocols.contains(&Proto::Awg))
        .collect();
    let client_awg = if host.role == Role::Entry {
        inb.get(&Proto::Awg)
    } else {
        None
    };
    if client_awg.is_some() || !incoming.is_empty() {
        let awg_in = inb
            .get(&Proto::Awg)
            .context("awg0 without an awg inbound")?;
        let keys = hs.awg.clone().context("no awg keys for the host")?;
        let mut addresses = vec![];
        let mut peers = vec![];
        let mut post_up = vec![];
        let mut post_down = vec![];
        if let Some(a) = client_awg {
            let s = a.subnets.as_ref().context("awg without subnets")?;
            for net in [s.split, s.full] {
                addresses.push(format!("{}/{}", gateway(net), net.prefix_len()));
            }
            for (uname, u) in users_of(st, name) {
                if let (Some(peer), Some(k)) = (
                    &u.awg,
                    st.secrets.users.get(&uname).and_then(|s| s.awg.as_ref()),
                ) {
                    peers.push(context! { name => uname.clone(), public_key => k.public.clone(), preshared => st.secrets.users[&uname].awg_preshared.clone(), allowed_ips => vec![format!("{}/32", peer.ip)], endpoint => minijinja::Value::from(()) });
                }
            }
            post_up.push("ip rule add fwmark 1 lookup 100".to_string());
            post_up.push("ip route add local 0.0.0.0/0 dev lo table 100".to_string());
            post_down.push("ip rule del fwmark 1 lookup 100".to_string());
            post_down.push("ip route del local 0.0.0.0/0 dev lo table 100".to_string());
        }
        for l in &incoming {
            let t = l.tunnel.context("link without a tunnel")?;
            let ls = st
                .secrets
                .links
                .get(&l.key())
                .and_then(|s| s.awg.as_ref())
                .context("no awg keys for the link")?;
            addresses.push(tunnel_addr(t, 2));
            masquerade.push(t.to_string());
            peers.push(context! { name => format!("link from {}", l.from), public_key => ls.from.public.clone(), preshared => ls.preshared.clone(), allowed_ips => vec![tunnel_addr(t, 1).replace("/30", "/32")], endpoint => minijinja::Value::from(()) });
        }
        f.insert(
            "/etc/amnezia/amneziawg/awg0.conf".into(),
            r("awg.conf.j2", context! { host => name, iface => "awg0", purpose => "clients and incoming links", addresses => addresses, private_key => keys.private, listen_port => awg_in.port, table_off => false, post_up => post_up, post_down => post_down, peers => peers })?,
        );
        awg_ifaces.push("awg0".to_string());
    }
    for (i, l) in out_links(st, name) {
        if !l.protocols.contains(&Proto::Awg) {
            continue;
        }
        let t = l.tunnel.context("link without a tunnel")?;
        let ls = st
            .secrets
            .links
            .get(&l.key())
            .and_then(|s| s.awg.as_ref())
            .context("no awg keys for the link")?;
        let to = st.host(&l.to)?;
        let port = st
            .inbounds
            .get(&l.to)
            .and_then(|m| m.get(&Proto::Awg))
            .map(|x| x.port)
            .unwrap_or(51820);
        let iface = format!("awg{i}");
        let table = 100 + i;
        f.insert(
            format!("/etc/amnezia/amneziawg/{iface}.conf"),
            r("awg.conf.j2", context! { host => name, iface => iface.clone(), purpose => format!("link to {}", l.to), addresses => vec![tunnel_addr(t, 1)], private_key => ls.from.private.clone(), listen_port => minijinja::Value::from(()), table_off => true,
                post_up => vec![format!("ip route add 0.0.0.0/0 dev %i table {table}"), format!("ip rule add oif %i lookup {table}")],
                post_down => vec![format!("ip rule del oif %i lookup {table}")],
                peers => vec![context! { name => l.to.clone(), public_key => ls.to.public.clone(), preshared => ls.preshared.clone(), allowed_ips => vec!["0.0.0.0/0".to_string()], endpoint => format!("{}:{port}", to.ip) }] })?,
        );
        awg_ifaces.push(iface);
    }

    // network
    let mut tcp = vec![];
    let mut udp = vec![];
    for (p, i) in &inb {
        if !i.enabled {
            continue;
        }
        match p {
            Proto::Reality | Proto::Mtg => tcp.push(i.port),
            Proto::Hy2 | Proto::Awg => udp.push(i.port),
            Proto::Ws => {}
        }
    }
    if host.role == Role::Entry {
        tcp.push(80);
    }
    tcp.sort();
    tcp.dedup();
    udp.sort();
    udp.dedup();
    let tproxy = client_awg.map(|_| TPROXY_PORT);
    // IPv6 forwarding is only needed for L3 clients (awg): the kernel then disables accept_ra itself
    let awg_used = !awg_ifaces.is_empty();
    f.insert("/etc/nftables.conf".into(), r("nftables.conf.j2", context! { host => name, tcp_ports => tcp, udp_ports => udp, awg_ifaces => awg_ifaces, tproxy_port => tproxy, masquerade => masquerade })?);
    f.insert(
        "/etc/sysctl.d/90-vpnctl.conf".into(),
        r("sysctl.conf.j2", context! { host => name, awg => awg_used })?,
    );

    // overrides/<host>/<path>: taken as is (drift adopted from the host)
    let ov = st.dir.join("overrides").join(name);
    if ov.is_dir() {
        for (rel, body) in walk(&ov)? {
            f.insert(format!("/{rel}"), body);
        }
    }
    Ok(f)
}

fn walk(root: &Path) -> Result<Vec<(String, String)>> {
    let mut out = vec![];
    let mut stack = vec![root.to_path_buf()];
    while let Some(d) = stack.pop() {
        for e in std::fs::read_dir(&d)? {
            let p = e?.path();
            if p.is_dir() {
                stack.push(p);
            } else {
                let rel = p.strip_prefix(root).unwrap().to_string_lossy().to_string();
                out.push((rel, std::fs::read_to_string(&p)?));
            }
        }
    }
    out.sort();
    Ok(out)
}

pub fn render(st: &State, only: Option<&str>) -> Result<Rendered> {
    let mut r = Rendered::default();
    for name in st.hosts.keys() {
        if only.is_none_or(|o| o == name) {
            r.hosts.insert(
                name.clone(),
                render_host(st, name).with_context(|| format!("render host {name}"))?,
            );
        }
    }
    if let Some(o) = only
        && r.hosts.is_empty()
    {
        bail!("host «{o}» is not described");
    }
    Ok(r)
}

pub fn write(r: &Rendered, out: &Path) -> Result<()> {
    for (host, files) in &r.hosts {
        for (path, body) in files {
            let p = out.join(host).join(path.trim_start_matches('/'));
            std::fs::create_dir_all(p.parent().unwrap())?;
            std::fs::write(&p, body)?;
        }
    }
    Ok(())
}

/// Two render runs must match byte for byte.
pub fn check(st: &State) -> Result<Rendered> {
    let a = render(st, None)?;
    let b = render(st, None)?;
    for (host, fa) in &a.hosts {
        for (path, body) in fa {
            if b.hosts[host].get(path) != Some(body) {
                bail!("render is not reproducible: {host}:{path} differs between runs");
            }
        }
    }
    Ok(a)
}
