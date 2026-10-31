//! Операции над состоянием. Каждая мутирует `State` и возвращает сообщение коммита;
//! запись, валидацию и коммит делает вызывающий (`main`).
use crate::secrets;
use crate::state::*;
use anyhow::{Context, Result, bail};
use ipnet::Ipv4Net;
use std::collections::BTreeMap;
use std::net::{IpAddr, Ipv4Addr};
use std::path::Path;

pub const SOPS_RULES: &str =
    "creation_rules:\n  - path_regex: secrets\\.sops\\.yaml$\n    age: {age}\n";

/// Пустое состояние + git-репозиторий. С `age` — `.sops.yaml`, иначе секреты открытым текстом.
pub fn init(dir: &Path, age: Option<&str>) -> Result<()> {
    if dir.join("hosts.yaml").exists() {
        bail!("{} уже содержит состояние", dir.display());
    }
    crate::render::install_templates(dir)?;
    if let Some(a) = age {
        std::fs::write(dir.join(".sops.yaml"), SOPS_RULES.replace("{age}", a))?;
    }
    let mut st = State {
        dir: dir.to_path_buf(),
        ..Default::default()
    };
    st.sets.insert(
        "ru-direct".into(),
        Set {
            geosite: vec!["category-ru".into(), "yandex".into(), "vk".into()],
            domains: vec!["gosuslugi.ru".into()],
            ..Default::default()
        },
    );
    st.sets.insert(
        "ads".into(),
        Set {
            geosite: vec!["category-ads-all".into()],
            ..Default::default()
        },
    );
    for (k, v) in [
        ("sing-box", "1.12.4"),
        ("xray", "25.9.11"),
        ("geodata", "latest"),
        ("caddy", "2.10.0"),
        ("mtg", "2.1.7"),
        ("awg", "1.0.20250706"),
    ] {
        st.versions.insert(
            k.into(),
            Pin {
                version: v.into(),
                sha256: None,
            },
        );
    }
    st.save()?;
    std::fs::write(dir.join(".gitignore"), "/out\n/metrics.sqlite\n")?;
    crate::git::init(dir)?;
    crate::git::commit(dir, "init: пустое состояние vpnctl")
}

fn parse_ip(ssh: &str) -> Option<IpAddr> {
    ssh.rsplit('@').next()?.parse().ok()
}

pub fn host_add(
    st: &mut State,
    name: &str,
    role: Role,
    ssh: &str,
    ip: Option<IpAddr>,
    domain: Option<String>,
    via: Option<String>,
    engine: Engine,
    awg: bool,
) -> Result<String> {
    if st.hosts.contains_key(name) {
        bail!("хост «{name}» уже есть");
    }
    let ip = ip
        .or_else(|| parse_ip(ssh))
        .context("не удалось понять IP из --ssh, укажите --ip")?;
    st.hosts.insert(
        name.into(),
        Host {
            role,
            engine,
            ip,
            domain: domain.clone(),
            os: "debian-13".into(),
            ssh: ssh.into(),
            via,
            host_key: None,
            retiring: false,
        },
    );
    let mut inb = BTreeMap::new();
    let mut hs = HostSecrets {
        reality: Some(secrets::reality()),
        awg: awg.then(secrets::awg),
        mtg_secret: None,
    };
    // вход: Reality прикрывается своим сайтом за Caddy (sni = домен); выход: внешним сайтом
    let sni = (role == Role::Exit || domain.is_none()).then(|| "www.microsoft.com".to_string());
    inb.insert(
        Proto::Reality,
        Inbound {
            port: 443,
            enabled: true,
            sni,
            handshake: None,
            path: None,
            subnets: None,
        },
    );
    if awg {
        inb.insert(
            Proto::Awg,
            Inbound {
                port: 51820,
                enabled: true,
                sni: None,
                handshake: None,
                path: None,
                subnets: None,
            },
        );
    }
    if role == Role::Entry {
        inb.insert(
            Proto::Ws,
            Inbound {
                port: 8444,
                enabled: true,
                sni: None,
                handshake: None,
                path: Some("/ws".into()),
                subnets: None,
            },
        );
        inb.insert(
            Proto::Hy2,
            Inbound {
                port: 443,
                enabled: true,
                sni: None,
                handshake: None,
                path: None,
                subnets: None,
            },
        );
        inb.insert(
            Proto::Mtg,
            Inbound {
                port: 2053,
                enabled: true,
                sni: None,
                handshake: None,
                path: None,
                subnets: None,
            },
        );
        if let Some(a) = inb.get_mut(&Proto::Awg) {
            a.subnets = Some(AwgSubnets {
                split: "10.8.1.0/24".parse()?,
                full: "10.8.2.0/24".parse()?,
            });
        }
        hs.mtg_secret = Some(secrets::mtg(
            domain.as_deref().unwrap_or("www.microsoft.com"),
        ));
        let link_auto = || Action::Link {
            link: "auto".into(),
        };
        let rule = |m: Match, a: Action, note: &str| Rule {
            match_: m,
            action: a,
            enabled: true,
            note: Some(note.into()),
        };
        let mut split = vec![];
        if st.sets.contains_key("ru-direct") {
            split.push(rule(
                Match {
                    sets: vec!["ru-direct".into()],
                    ..Default::default()
                },
                Action::Simple(Simple::Direct),
                "российские сервисы напрямую",
            ));
        }
        split.push(rule(
            Match {
                geoip: vec!["ru".into(), "private".into()],
                ..Default::default()
            },
            Action::Simple(Simple::Direct),
            "российские адреса напрямую",
        ));
        split.push(rule(
            Match::default(),
            link_auto(),
            "всё остальное через выход",
        ));
        let mut all = vec![rule(
            Match {
                inbound: vec!["probe".into()],
                ..Default::default()
            },
            link_auto(),
            "проба цепочки и upstream mtg",
        )];
        if st.sets.contains_key("ads") {
            all.push(rule(
                Match {
                    sets: vec!["ads".into()],
                    ..Default::default()
                },
                Action::Simple(Simple::Block),
                "реклама",
            ));
        }
        st.routing.insert(
            name.into(),
            Routing {
                exit: Exit::default(),
                groups: Groups {
                    all,
                    split,
                    full: vec![rule(Match::default(), link_auto(), "всё через выход")],
                },
                dns: Dns::default(),
            },
        );
    } else {
        st.routing.insert(name.into(), Routing::default());
    }
    st.inbounds.insert(name.into(), inb);
    st.secrets.hosts.insert(name.into(), hs);
    Ok(format!(
        "hosts: + {name} ({})",
        if role == Role::Entry {
            "вход"
        } else {
            "выход"
        }
    ))
}

pub fn host_rm(st: &mut State, name: &str) -> Result<String> {
    st.hosts
        .remove(name)
        .with_context(|| format!("хост «{name}» не описан"))?;
    st.inbounds.remove(name);
    st.routing.remove(name);
    st.secrets.hosts.remove(name);
    let _ = std::fs::remove_file(st.dir.join("routing").join(format!("{name}.yaml")));
    let gone: Vec<String> = st
        .links
        .iter()
        .filter(|l| l.from == name || l.to == name)
        .map(Link::key)
        .collect();
    st.links.retain(|l| l.from != name && l.to != name);
    for k in gone {
        st.secrets.links.remove(&k);
    }
    for u in st.users.values_mut() {
        if u.entry.as_deref() == Some(name) {
            u.entry = None;
        }
    }
    Ok(format!("hosts: - {name}"))
}

fn free_peer_ip(st: &State, net: Ipv4Net) -> Result<Ipv4Addr> {
    let used: Vec<Ipv4Addr> = st
        .users
        .values()
        .filter_map(|u| u.awg.as_ref().map(|a| a.ip))
        .collect();
    net.hosts()
        .skip(1)
        .find(|ip| !used.contains(ip))
        .with_context(|| format!("в подсети {net} не осталось адресов"))
}

#[derive(Default, Clone)]
pub struct UserPatch {
    pub modes: Option<Vec<Mode>>,
    pub protocols: Option<Vec<Proto>>,
    pub expires: Option<Option<String>>,
    pub note: Option<String>,
    pub tg: Option<bool>,
}

fn modes_str(m: &[Mode]) -> String {
    m.iter().map(|x| x.name()).collect::<Vec<_>>().join(", ")
}

pub fn user_add(
    st: &mut State,
    name: &str,
    modes: Vec<Mode>,
    protocols: Vec<Proto>,
    awg_mode: Option<Mode>,
    tg: bool,
    expires: Option<String>,
    note: Option<String>,
    entry: Option<String>,
) -> Result<String> {
    if st.users.contains_key(name) {
        bail!("пользователь «{name}» уже есть");
    }
    let mut u = User {
        modes,
        protocols,
        enabled: true,
        expires,
        entry,
        awg: None,
        tg,
        note,
    };
    let entry = st
        .entry_of(&u)
        .context("не указан вход (--entry), а входов не ровно один")?;
    let mut us = UserSecrets {
        uuid: secrets::uuid(),
        hy2_password: secrets::password(),
        sub_id: secrets::sub_id(),
        awg: None,
        awg_preshared: None,
    };
    if let Some(mode) = awg_mode {
        let net = st
            .inbounds
            .get(&entry)
            .and_then(|i| i.get(&Proto::Awg))
            .and_then(|i| i.subnets.as_ref())
            .with_context(|| format!("на входе «{entry}» нет awg с подсетями"))?
            .for_mode(mode);
        u.awg = Some(AwgPeer {
            mode,
            ip: free_peer_ip(st, net)?,
        });
        if !u.protocols.contains(&Proto::Awg) {
            u.protocols.push(Proto::Awg);
        }
        us.awg = Some(secrets::awg());
        us.awg_preshared = Some(secrets::preshared());
    }
    let msg = format!("users: + {name} ({})", modes_str(&u.modes));
    st.users.insert(name.into(), u);
    st.secrets.users.insert(name.into(), us);
    Ok(msg)
}

pub fn user_edit(st: &mut State, name: &str, p: UserPatch) -> Result<String> {
    let u = st
        .users
        .get_mut(name)
        .with_context(|| format!("пользователь «{name}» не описан"))?;
    let mut what = vec![];
    if let Some(m) = p.modes {
        u.modes = m;
        what.push("режимы");
    }
    if let Some(pr) = p.protocols {
        u.protocols = pr;
        what.push("протоколы");
    }
    if let Some(e) = p.expires {
        u.expires = e;
        what.push("срок");
    }
    if let Some(n) = p.note {
        u.note = Some(n);
        what.push("заметка");
    }
    if let Some(t) = p.tg {
        u.tg = t;
        what.push("tg");
    }
    if what.is_empty() {
        bail!("нечего менять");
    }
    Ok(format!("users: {name}: {}", what.join(", ")))
}

pub fn user_enable(st: &mut State, name: &str, on: bool) -> Result<String> {
    let u = st
        .users
        .get_mut(name)
        .with_context(|| format!("пользователь «{name}» не описан"))?;
    u.enabled = on;
    Ok(format!(
        "users: {name} {}",
        if on {
            "включён"
        } else {
            "отключён"
        }
    ))
}

pub fn user_rm(st: &mut State, name: &str) -> Result<String> {
    st.users
        .remove(name)
        .with_context(|| format!("пользователь «{name}» не описан"))?;
    st.secrets.users.remove(name);
    Ok(format!("users: - {name}"))
}

fn free_tunnel(st: &State) -> Result<Ipv4Net> {
    (1..=250u8)
        .map(|n| Ipv4Net::new(Ipv4Addr::new(10, 9, n, 0), 30).unwrap())
        .find(|c| !st.links.iter().any(|l| l.tunnel == Some(*c)))
        .context("кончились подсети туннелей 10.9.0.0/16")
}

fn ensure_link_inbound(st: &mut State, host: &str, p: Proto) {
    let inb = st.inbounds.entry(host.into()).or_default();
    if inb.contains_key(&p) {
        return;
    }
    let (port, sni) = match p {
        Proto::Reality => (443, Some("www.microsoft.com".to_string())),
        Proto::Hy2 => (443, None),
        _ => (51820, None),
    };
    inb.insert(
        p,
        Inbound {
            port,
            enabled: true,
            sni,
            handshake: None,
            path: None,
            subnets: None,
        },
    );
    let hs = st.secrets.hosts.entry(host.into()).or_default();
    if p == Proto::Reality && hs.reality.is_none() {
        hs.reality = Some(secrets::reality());
    }
    if p == Proto::Awg && hs.awg.is_none() {
        hs.awg = Some(secrets::awg());
    }
}

fn link_secret_for(ls: &mut LinkSecrets, p: Proto) {
    match p {
        Proto::Awg if ls.awg.is_none() => {
            ls.awg = Some(LinkAwg {
                from: secrets::awg(),
                to: secrets::awg(),
                preshared: secrets::preshared(),
            })
        }
        Proto::Reality if ls.reality.is_none() => {
            ls.reality = Some(LinkReality {
                uuid: secrets::uuid(),
                keys: secrets::reality(),
            })
        }
        Proto::Hy2 if ls.hy2_password.is_none() => ls.hy2_password = Some(secrets::password()),
        _ => {}
    }
}

/// Включить/выключить inbound хоста.
pub fn inbound_toggle(st: &mut State, host: &str, p: Proto) -> Result<String> {
    let i = st
        .inbounds
        .get_mut(host)
        .and_then(|m| m.get_mut(&p))
        .with_context(|| format!("на хосте «{host}» нет inbound {}", p.name()))?;
    i.enabled = !i.enabled;
    Ok(format!(
        "protocols({host}): {} {}",
        p.name(),
        if i.enabled {
            "включён"
        } else {
            "выключен"
        }
    ))
}

pub fn link_add(
    st: &mut State,
    from: &str,
    to: &str,
    protocols: Vec<Proto>,
    policy: Policy,
    active: Option<Proto>,
) -> Result<String> {
    if st.link(from, to).is_some() {
        bail!("линк {from}->{to} уже есть");
    }
    st.host(from)?;
    st.host(to)?;
    let protocols = if protocols.is_empty() {
        vec![Proto::Awg, Proto::Reality]
    } else {
        protocols
    };
    let tunnel = protocols
        .contains(&Proto::Awg)
        .then(|| free_tunnel(st))
        .transpose()?;
    let active = active.or(Some(protocols[0]));
    let mut ls = LinkSecrets::default();
    for p in &protocols {
        ensure_link_inbound(st, to, *p);
        link_secret_for(&mut ls, *p);
    }
    let l = Link {
        from: from.into(),
        to: to.into(),
        protocols: protocols.clone(),
        policy,
        active,
        tunnel,
        urltest: UrlTest::default(),
    };
    st.secrets.links.insert(l.key(), ls);
    st.links.push(l);
    if let Some(r) = st.routing.get_mut(from)
        && r.exit.prefer.is_none()
    {
        r.exit.prefer = Some(to.into());
    }
    Ok(format!(
        "links: + {from}->{to} ({})",
        protocols
            .iter()
            .map(|p| p.name())
            .collect::<Vec<_>>()
            .join(", ")
    ))
}

pub fn link_set(
    st: &mut State,
    from: &str,
    to: &str,
    active: Option<Proto>,
    policy: Option<Policy>,
    add: Vec<Proto>,
    rm: Vec<Proto>,
) -> Result<String> {
    let idx = st
        .links
        .iter()
        .position(|l| l.from == from && l.to == to)
        .with_context(|| format!("линка {from}->{to} нет"))?;
    let key = st.links[idx].key();
    let mut what = vec![];
    for p in &add {
        if !st.links[idx].protocols.contains(p) {
            st.links[idx].protocols.push(*p);
            ensure_link_inbound(st, to, *p);
            link_secret_for(st.secrets.links.entry(key.clone()).or_default(), *p);
            what.push(format!("+{}", p.name()));
        }
    }
    for p in &rm {
        st.links[idx].protocols.retain(|x| x != p);
        what.push(format!("-{}", p.name()));
    }
    if st.links[idx].protocols.contains(&Proto::Awg) && st.links[idx].tunnel.is_none() {
        st.links[idx].tunnel = Some(free_tunnel_from(&st.links, idx)?);
    }
    let l = &mut st.links[idx];
    if let Some(a) = active {
        l.active = Some(a);
        what.push(format!("active {}", a.name()));
    }
    if let Some(p) = policy {
        l.policy = p;
        what.push(format!("policy {}", serde_yaml::to_string(&p)?.trim()));
    }
    if what.is_empty() {
        bail!("нечего менять");
    }
    Ok(format!("links: {from}->{to}: {}", what.join(", ")))
}

fn free_tunnel_from(links: &[Link], skip: usize) -> Result<Ipv4Net> {
    (1..=250u8)
        .map(|n| Ipv4Net::new(Ipv4Addr::new(10, 9, n, 0), 30).unwrap())
        .find(|c| {
            !links
                .iter()
                .enumerate()
                .any(|(i, l)| i != skip && l.tunnel == Some(*c))
        })
        .context("кончились подсети туннелей")
}

pub fn exit_set(
    st: &mut State,
    host: &str,
    prefer: Option<String>,
    policy: Option<ExitPolicy>,
) -> Result<String> {
    st.host(host)?;
    let r = st.routing.entry(host.into()).or_default();
    let mut what = vec![];
    if let Some(p) = prefer {
        what.push(format!("prefer {p}"));
        r.exit.prefer = Some(p);
    }
    if let Some(p) = policy {
        r.exit.policy = p;
        what.push(format!("policy {}", serde_yaml::to_string(&p)?.trim()));
    }
    if what.is_empty() {
        bail!("нечего менять");
    }
    Ok(format!("routing({host}): выход: {}", what.join(", ")))
}

fn add_rm(v: &mut Vec<String>, add: &[String], rm: &[String]) {
    for a in add {
        if !v.contains(a) {
            v.push(a.clone());
        }
    }
    v.retain(|x| !rm.contains(x));
    v.sort();
}

pub fn set_edit(
    st: &mut State,
    name: &str,
    domains: (&[String], &[String]),
    geosite: (&[String], &[String]),
    geoip: (&[String], &[String]),
) -> Result<String> {
    let s = st.sets.entry(name.into()).or_default();
    add_rm(&mut s.domains, domains.0, domains.1);
    add_rm(&mut s.geosite, geosite.0, geosite.1);
    add_rm(&mut s.geoip, geoip.0, geoip.1);
    let n = domains.0.len() + geosite.0.len() + geoip.0.len();
    let m = domains.1.len() + geosite.1.len() + geoip.1.len();
    if n + m == 0 {
        bail!("нечего менять");
    }
    Ok(format!("sets: {name} +{n} -{m}"))
}

pub fn set_rm(st: &mut State, name: &str) -> Result<String> {
    st.sets
        .remove(name)
        .with_context(|| format!("набора «{name}» нет"))?;
    let _ = std::fs::remove_file(st.dir.join("sets").join(format!("{name}.yaml")));
    Ok(format!("sets: - {name}"))
}

fn rules<'a>(st: &'a mut State, host: &str, g: Group) -> Result<&'a mut Vec<Rule>> {
    st.host(host)?;
    Ok(st.routing.entry(host.into()).or_default().groups.get_mut(g))
}

fn gname(g: Group) -> &'static str {
    match g {
        Group::All => "all",
        Group::Split => "split",
        Group::Full => "full",
    }
}

/// `pos` — позиция 1..=n+1, по умолчанию перед последним правилом-«всё остальное», иначе в конец.
pub fn rule_add(
    st: &mut State,
    host: &str,
    g: Group,
    pos: Option<usize>,
    m: Match,
    action: Action,
    note: Option<String>,
) -> Result<String> {
    let rs = rules(st, host, g)?;
    let pos = match pos {
        Some(p) if p >= 1 && p <= rs.len() + 1 => p - 1,
        Some(p) => bail!("позиция {p} вне 1..={}", rs.len() + 1),
        None if rs.last().is_some_and(|r| r.match_.is_empty()) => rs.len() - 1,
        None => rs.len(),
    };
    rs.insert(
        pos,
        Rule {
            match_: m,
            action: action.clone(),
            enabled: true,
            note: note.clone(),
        },
    );
    Ok(format!(
        "routing({host}): {} #{}: {} → {}",
        gname(g),
        pos + 1,
        note.unwrap_or_else(|| "правило".into()),
        action
    ))
}

fn rule_at(rs: &mut [Rule], idx: usize) -> Result<&mut Rule> {
    let n = rs.len();
    rs.get_mut(idx.wrapping_sub(1))
        .with_context(|| format!("правила #{idx} нет, всего {n}"))
}

pub fn rule_edit(
    st: &mut State,
    host: &str,
    g: Group,
    idx: usize,
    m: Option<Match>,
    action: Option<Action>,
    note: Option<String>,
) -> Result<String> {
    let r = rule_at(rules(st, host, g)?, idx)?;
    if let Some(m) = m {
        r.match_ = m;
    }
    if let Some(a) = action {
        r.action = a;
    }
    if let Some(n) = note {
        r.note = Some(n);
    }
    Ok(format!("routing({host}): {} #{idx} изменено", gname(g)))
}

pub fn rule_rm(st: &mut State, host: &str, g: Group, idx: usize) -> Result<String> {
    let rs = rules(st, host, g)?;
    rule_at(rs, idx)?;
    rs.remove(idx - 1);
    Ok(format!("routing({host}): {} #{idx} удалено", gname(g)))
}

pub fn rule_move(st: &mut State, host: &str, g: Group, idx: usize, to: usize) -> Result<String> {
    let rs = rules(st, host, g)?;
    rule_at(rs, idx)?;
    rule_at(rs, to)?;
    let r = rs.remove(idx - 1);
    rs.insert(to - 1, r);
    Ok(format!("routing({host}): {} #{idx} → #{to}", gname(g)))
}

pub fn rule_toggle(st: &mut State, host: &str, g: Group, idx: usize) -> Result<String> {
    let r = rule_at(rules(st, host, g)?, idx)?;
    r.enabled = !r.enabled;
    Ok(format!(
        "routing({host}): {} #{idx} {}",
        gname(g),
        if r.enabled {
            "включено"
        } else {
            "выключено"
        }
    ))
}
