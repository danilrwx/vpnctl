//! Pure TUI functions: table rows, status levels, navigation, filter, forms, trace.
//! No terminal, no SSH — everything is covered by tests on the fixture.
use crate::metrics::{self, Edge, HostOut, Status, UserOut};
use crate::ops;
use crate::plan::{HostPlan, Status as FileStatus};
use crate::state::*;
use anyhow::{Context, Result, bail};
use std::collections::BTreeMap;
use std::str::FromStr;

pub const CERT_WARN_DAYS: i64 = 14;
pub const EXPIRES_WARN_DAYS: i64 = 7;
/// Chain probe slower than this is yellow (as in the mockups: > 0.7 s).
pub const PROBE_SLOW_MS: u64 = 700;
pub const DAY: i64 = 86_400;

/// Row status: green / yellow / red / grey.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Level {
    Ok,
    Warn,
    Bad,
    Off,
}

#[derive(Clone, Debug, PartialEq)]
pub struct Row {
    pub cells: Vec<String>,
    pub level: Level,
}

// ---------- navigation and filter ----------

/// Move the selection without wrapping.
pub fn step(cur: usize, len: usize, delta: isize) -> usize {
    if len == 0 {
        return 0;
    }
    (cur as isize + delta).clamp(0, len as isize - 1) as usize
}

/// Indices of rows containing `q` (case-insensitive); an empty query keeps all rows.
pub fn filter(rows: &[Row], q: &str) -> Vec<usize> {
    let q = q.to_lowercase();
    rows.iter()
        .enumerate()
        .filter(|(_, r)| q.is_empty() || r.cells.iter().any(|c| c.to_lowercase().contains(&q)))
        .map(|(i, _)| i)
        .collect()
}

// ---------- formatting ----------

pub fn gb(b: u64) -> String {
    if b == 0 {
        "0".into()
    } else {
        format!("{:.1} GB", b as f64 / 1e9)
    }
}

/// "N min ago" from two epochs.
pub fn ago(ts: i64, now: i64) -> String {
    let d = (now - ts).max(0);
    match d {
        0..60 => "just now".into(),
        60..3600 => format!("{} min ago", d / 60),
        3600..86400 => format!("{} h ago", d / 3600),
        _ => format!("{} d ago", d / DAY),
    }
}

/// `YYYY-MM-DD` → epoch of midnight UTC (days_from_civil).
pub fn date_epoch(s: &str) -> Option<i64> {
    let mut it = s.split('-').map(|p| p.parse::<i64>().ok());
    let (y, m, d) = (it.next()??, it.next()??, it.next()??);
    if !(1..=12).contains(&m) || !(1..=31).contains(&d) {
        return None;
    }
    let y = if m <= 2 { y - 1 } else { y };
    let era = y.div_euclid(400);
    let yoe = y - era * 400;
    let doy = (153 * (if m > 2 { m - 3 } else { m + 9 }) + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    Some((era * 146_097 + doe - 719_468) * DAY)
}

/// Epoch → `DD.MM` (civil_from_days).
pub fn short_date(ts: i64) -> String {
    let z = ts.div_euclid(DAY) + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z - era * 146_097;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    format!("{d:02}.{m:02}")
}

/// Epoch → `DD.MM HH:MM` UTC.
pub fn date_time(ts: i64) -> String {
    let s = ts.rem_euclid(DAY);
    format!("{} {:02}:{:02}", short_date(ts), s / 3600, s % 3600 / 60)
}

// ---------- hosts ----------

/// Is this entry a jump for some other host?
pub fn is_jump(st: &State, name: &str) -> bool {
    st.hosts.values().any(|h| h.via.as_deref() == Some(name))
}

/// Is this exit preferred by some entry (or the only exit)?
pub fn is_active_exit(st: &State, name: &str) -> bool {
    let exits = st.hosts.values().filter(|h| h.role == Role::Exit).count();
    exits == 1
        || st
            .routing
            .values()
            .any(|r| r.exit.prefer.as_deref() == Some(name))
}

/// `entry · jump`, `exit · active`, `exit · standby`, `entry · new` (no host key yet).
pub fn role_text(st: &State, name: &str) -> String {
    let Some(h) = st.hosts.get(name) else {
        return String::new();
    };
    let tag = match h.role {
        _ if h.host_key.is_none() => "new",
        _ if h.retiring => "retiring",
        Role::Entry if is_jump(st, name) => "jump",
        Role::Entry => "",
        Role::Exit if is_active_exit(st, name) => "active",
        Role::Exit => "standby",
    };
    let role = match h.role {
        Role::Entry => "entry",
        Role::Exit => "exit",
    };
    if tag.is_empty() {
        role.into()
    } else {
        format!("{role} · {tag}")
    }
}

/// Services the host should run, from the state: engine, caddy, awg, mtg.
pub fn services(st: &State, name: &str) -> Vec<&'static str> {
    let Some(h) = st.hosts.get(name) else {
        return vec![];
    };
    let inb = st.inbounds.get(name);
    let has = |p: Proto| inb.is_some_and(|m| m.get(&p).is_some_and(|i| i.enabled));
    let mut v = vec![match h.engine {
        Engine::SingBox => "sing-box",
        Engine::Xray => "xray",
    }];
    if h.role == Role::Entry && h.domain.is_some() {
        v.push("caddy");
    }
    if has(Proto::Awg)
        || st
            .links
            .iter()
            .any(|l| (l.from == name || l.to == name) && l.protocols.contains(&Proto::Awg))
    {
        v.push("awg");
    }
    if has(Proto::Mtg) {
        v.push("mtg");
    }
    v
}

/// Preferred exit of an entry: explicit `prefer`, else its only link target.
pub fn preferred_exit(st: &State, entry: &str) -> Option<String> {
    st.routing
        .get(entry)
        .and_then(|r| r.exit.prefer.clone())
        .or_else(|| {
            let mut it = st.links_from(entry);
            match (it.next(), it.next()) {
                (Some(l), None) => Some(l.to.clone()),
                _ => None,
            }
        })
}

// ---------- levels ----------

/// What is wrong with a host: (level, reason). Empty — fine or never collected.
pub fn host_issues(h: &HostOut, head: Option<&str>) -> Vec<(Level, String)> {
    let Some(l) = &h.last else {
        return vec![];
    };
    let mut v = vec![];
    if !l.reachable {
        v.push((
            Level::Bad,
            format!("✗ {}", l.error.as_deref().unwrap_or("no answer")),
        ));
        return v;
    }
    match l.m.probe_code {
        Some(204) => {}
        Some(c) => v.push((Level::Bad, format!("probe {c}"))),
        None => v.push((Level::Bad, "probe: no answer".into())),
    }
    if l.drift == Some(true) {
        v.push((Level::Warn, "drift".into()));
    }
    if head.is_some_and(|hd| l.marker.as_deref().is_some_and(|m| m != hd)) {
        v.push((Level::Warn, "behind HEAD".into()));
    }
    if let Some(d) = h.cert_days.filter(|d| *d < CERT_WARN_DAYS) {
        v.push((Level::Warn, format!("certificate {d} d")));
    }
    if let Some(u) = l.m.updates.filter(|u| *u > 0) {
        v.push((Level::Warn, format!("updates pending: {u}")));
    }
    if l.m.reboot == Some(true) {
        v.push((Level::Warn, "reboot required".into()));
    }
    v
}

pub fn host_level(h: &HostOut, head: Option<&str>) -> Level {
    if h.last.is_none() {
        return Level::Off;
    }
    let issues = host_issues(h, head);
    if issues.iter().any(|(l, _)| *l == Level::Bad) {
        Level::Bad
    } else if issues.is_empty() {
        Level::Ok
    } else {
        Level::Warn
    }
}

pub fn user_level(u: &UserOut, expires: Option<&str>, now: i64) -> Level {
    if !u.enabled {
        return Level::Off;
    }
    match expires.and_then(date_epoch) {
        Some(e) if e < now => Level::Bad,
        Some(e) if e - now < EXPIRES_WARN_DAYS * DAY => Level::Warn,
        _ => Level::Ok,
    }
}

pub fn edge_level(e: Option<&Edge>) -> Level {
    match e {
        None => Level::Off,
        Some(e) if e.ok && e.ms.is_some_and(|m| m > PROBE_SLOW_MS) => Level::Warn,
        Some(e) if e.ok => Level::Ok,
        Some(e) if e.note.as_deref() == Some("udp") => Level::Off,
        Some(_) => Level::Bad,
    }
}

/// `● 181 ms`, `✗ timeout`, `— not configured`, `— udp`.
pub fn edge_text(e: Option<&Edge>) -> String {
    match e {
        None => "— not set".into(),
        Some(e) if e.ok => format!("● {} ms", e.ms.unwrap_or(0)),
        Some(e) if e.note.as_deref() == Some("udp") => "— udp".into(),
        Some(e) => format!("✗ {}", e.note.as_deref().unwrap_or("timeout")),
    }
}

// ---------- table rows ----------

pub const HOST_HEADER: [&str; 9] = [
    "host", "role", "domain", "ssh", "commit", "services", "chain", "cert", "OS",
];

/// `behind(marker)` — commits between the marker and HEAD, when known.
pub fn host_rows(
    st: &State,
    s: &Status,
    head: Option<&str>,
    behind: &dyn Fn(&str) -> Option<usize>,
) -> Vec<Row> {
    st.hosts
        .iter()
        .map(|(name, h)| {
            let o = &s.hosts[name];
            let l = o.last.as_ref();
            let ssh = match l {
                None => "—".into(),
                Some(l) if !l.reachable => {
                    format!("✗ {}", l.error.as_deref().unwrap_or("no answer"))
                }
                Some(l) => format!(
                    "● {}{}",
                    l.ssh_ms.map(|ms| format!("{ms} ms")).unwrap_or_default(),
                    h.via
                        .as_deref()
                        .map(|v| format!(" via {v}"))
                        .unwrap_or_default()
                ),
            };
            let commit = match l.and_then(|l| l.marker.as_deref()) {
                None if h.host_key.is_none() => "no bootstrap".into(),
                None => "—".into(),
                Some(m) if head == Some(m) => format!("{m} = HEAD"),
                Some(m) => match behind(m) {
                    Some(n) => format!("{m} −{n}"),
                    None => format!("{m} ≠ HEAD"),
                },
            };
            let chain = match (h.role, l.map(|l| (l.m.probe_code, l.m.probe_ms))) {
                (Role::Exit, _) | (_, None) => "—".into(),
                (_, Some((Some(204), ms))) => format!(
                    "● {}{} ms",
                    preferred_exit(st, name)
                        .map(|e| format!("{e} "))
                        .unwrap_or_default(),
                    ms.unwrap_or(0)
                ),
                (_, Some((Some(c), _))) => format!("✗ {c}"),
                (_, Some((None, _))) => "✗ no answer".into(),
            };
            let updates = match l.and_then(|l| l.m.updates) {
                None => "—".into(),
                Some(u) if l.and_then(|l| l.m.reboot) == Some(true) => format!("{u} ↻"),
                Some(u) => u.to_string(),
            };
            Row {
                cells: vec![
                    name.clone(),
                    role_text(st, name),
                    h.domain.clone().unwrap_or("— (by IP)".into()),
                    ssh,
                    commit,
                    services(st, name).join(" "),
                    chain,
                    o.cert_days.map(|d| format!("{d} d")).unwrap_or("—".into()),
                    updates,
                ],
                level: host_level(o, head),
            }
        })
        .collect()
}

pub const USER_HEADER: [&str; 8] = [
    "name",
    "modes",
    "status",
    "expiry",
    "day",
    "week",
    "total",
    "last seen",
];

/// `expiry` cell: `no expiry`, `expired`, `N d` when soon, `until YYYY-MM-DD`.
pub fn expiry_text(expires: Option<&str>, now: i64) -> String {
    match expires {
        None => "no expiry".into(),
        Some(e) => match date_epoch(e) {
            Some(t) if t < now => "expired".into(),
            Some(t) if t - now < EXPIRES_WARN_DAYS * DAY => format!("{} d", (t - now) / DAY),
            _ => format!("until {e}"),
        },
    }
}

pub fn user_rows(st: &State, s: &Status, now: i64) -> Vec<Row> {
    st.users
        .iter()
        .map(|(name, u)| {
            let o = &s.users[name];
            Row {
                cells: vec![
                    name.clone(),
                    u.modes
                        .iter()
                        .map(|m| m.name())
                        .collect::<Vec<_>>()
                        .join(" "),
                    if u.enabled { "active" } else { "disabled" }.into(),
                    expiry_text(u.expires.as_deref(), now),
                    gb(o.day.down + o.day.up),
                    gb(o.week.down + o.week.up),
                    gb(o.total.down + o.total.up),
                    // ponytail: last activity is not collected (stats API has no timestamps)
                    "—".into(),
                ],
                level: user_level(o, u.expires.as_deref(), now),
            }
        })
        .collect()
}

/// Sort the users table by name or by traffic per day (descending).
pub fn sort_users(rows: &mut [Row], by_traffic: bool) {
    if by_traffic {
        rows.sort_by_key(|r| {
            r.cells[4]
                .trim_end_matches(" GB")
                .parse::<f64>()
                .map(|g| -(g * 10.0) as i64)
                .unwrap_or(0)
        });
    } else {
        rows.sort_by(|a, b| a.cells[0].cmp(&b.cells[0]));
    }
}

/// Set contents: `ru-direct (geosite × 3 + 14 own)`; an unknown set is just its name.
pub fn set_text(st: &State, name: &str) -> String {
    let Some(s) = st.sets.get(name) else {
        return name.into();
    };
    let mut p = vec![];
    if !s.geosite.is_empty() {
        p.push(format!("geosite × {}", s.geosite.len()));
    }
    if !s.geoip.is_empty() {
        p.push(format!("geoip × {}", s.geoip.len()));
    }
    let own = s.domains.len() + s.ips.len();
    if own > 0 {
        p.push(format!("{own} own"));
    }
    if p.is_empty() {
        format!("{name} (empty)")
    } else {
        format!("{name} ({})", p.join(" + "))
    }
}

/// Set members in one line: `geosite:category-ru, yandex, vk + 14 own`.
pub fn set_members(st: &State, name: &str) -> String {
    let Some(s) = st.sets.get(name) else {
        return String::new();
    };
    let mut p: Vec<String> = s.geosite.iter().map(|g| format!("geosite:{g}")).collect();
    p.extend(s.geoip.iter().map(|g| format!("geoip:{g}")));
    let own = s.domains.len() + s.ips.len();
    let mut out = p.join(", ");
    if own > 0 {
        if !out.is_empty() {
            out.push_str(" + ");
        }
        out.push_str(&format!("{own} own"));
    }
    out
}

/// Example own domains of a set for the grey line under its name.
pub fn set_examples(st: &State, name: &str, n: usize) -> String {
    let Some(s) = st.sets.get(name) else {
        return String::new();
    };
    let mut v: Vec<String> = s.domains.iter().take(n).cloned().collect();
    v.extend(
        s.ips
            .iter()
            .take(n.saturating_sub(v.len()))
            .map(|i| i.to_string()),
    );
    let more = s.domains.len() + s.ips.len() > v.len();
    let mut out = v.join(", ");
    if more {
        out.push_str(" …");
    }
    out
}

/// Rule condition in words; empty — `everything else`.
pub fn match_text(st: &State, m: &Match) -> String {
    let mut p = vec![];
    for n in &m.sets {
        let members = set_members(st, n);
        p.push(if members.is_empty() {
            format!("domains from set {n}")
        } else {
            format!("domains from set {n} · {members}")
        });
    }
    if !m.geosite.is_empty() {
        p.push(format!(
            "domains {}",
            m.geosite
                .iter()
                .map(|g| format!("geosite:{g}"))
                .collect::<Vec<_>>()
                .join(", ")
        ));
    }
    if !m.geoip.is_empty() {
        p.push(format!(
            "addresses {}",
            m.geoip
                .iter()
                .map(|g| format!("geoip:{g}"))
                .collect::<Vec<_>>()
                .join(", ")
        ));
    }
    p.extend(m.domains.iter().cloned());
    p.extend(m.ip.iter().map(|i| i.to_string()));
    if !m.inbound.is_empty() {
        p.push(format!("inbound {}", m.inbound.join(", ")));
    }
    if !m.port.is_empty() {
        p.push(format!(
            "port {}",
            m.port
                .iter()
                .map(|x| x.to_string())
                .collect::<Vec<_>>()
                .join(", ")
        ));
    }
    if p.is_empty() {
        "everything else".into()
    } else {
        p.join(" · ")
    }
}

pub fn action_text(a: &Action) -> String {
    match a {
        Action::Simple(Simple::Direct) => "direct".into(),
        Action::Simple(Simple::Block) => "block".into(),
        Action::Link { link } if link == "auto" => "→ link auto".into(),
        Action::Link { link } => format!("→ link {link}"),
    }
}

pub fn group_name(g: Group) -> &'static str {
    match g {
        Group::All => "For all",
        Group::Split => "Mode split",
        Group::Full => "Mode full",
    }
}

/// Group subtitle: `checked before modes`, `users split-* and awg peers from 10.8.1.0/24`.
pub fn group_note(st: &State, host: &str, g: Group) -> String {
    let subnet = |m: Mode| {
        st.inbounds
            .get(host)
            .and_then(|i| i.get(&Proto::Awg))
            .and_then(|i| i.subnets.as_ref())
            .map(|s| format!(" and awg peers from {}", s.for_mode(m)))
            .unwrap_or_default()
    };
    match g {
        Group::All => "checked before modes".into(),
        Group::Split => format!("users split-*{}", subnet(Mode::Split)),
        Group::Full => format!("users full-*{}", subnet(Mode::Full)),
    }
}

pub const GROUPS: [Group; 3] = [Group::All, Group::Split, Group::Full];

fn group_rules(r: &Routing, g: Group) -> &[Rule] {
    match g {
        Group::All => &r.groups.all,
        Group::Split => &r.groups.split,
        Group::Full => &r.groups.full,
    }
}

/// Rules of an entry, group by group: (group, 1-based number, row).
pub fn rule_rows(st: &State, host: &str) -> Vec<(Group, usize, Row)> {
    let Some(r) = st.routing.get(host) else {
        return vec![];
    };
    GROUPS
        .iter()
        .flat_map(|g| {
            group_rules(r, *g).iter().enumerate().map(|(i, rule)| {
                (
                    *g,
                    i + 1,
                    Row {
                        cells: vec![
                            (i + 1).to_string(),
                            match_text(st, &rule.match_),
                            action_text(&rule.action),
                            if rule.enabled { "●" } else { "○" }.into(),
                            rule.note.clone().unwrap_or_default(),
                        ],
                        level: if rule.enabled { Level::Ok } else { Level::Off },
                    },
                )
            })
        })
        .collect()
}

pub const MATRIX_COLS: [&str; 6] = ["reality", "ws", "hy2", "awg", "mtg", "ssh"];

/// A matrix row: (src, dst) and the cells per protocol with a level.
pub type MatrixRow = ((String, String), Vec<(String, Level)>);

/// "edge × protocol" matrix of the last run.
pub fn matrix_rows(s: &Status) -> Vec<MatrixRow> {
    let mut edges: Vec<(&str, &str)> = vec![];
    for e in &s.matrix {
        if !edges.contains(&(&e.src, &e.dst)) {
            edges.push((&e.src, &e.dst));
        }
    }
    edges
        .into_iter()
        .map(|(src, dst)| {
            let cells = MATRIX_COLS
                .iter()
                .map(|c| {
                    let e = s
                        .matrix
                        .iter()
                        .find(|e| e.src == src && e.dst == dst && e.proto == *c);
                    (edge_text(e), edge_level(e))
                })
                .collect();
            ((src.to_string(), dst.to_string()), cells)
        })
        .collect()
}

/// The `note` column of the matrix: what the edge means.
pub fn edge_note(st: &State, s: &Status, src: &str, dst: &str) -> String {
    let edges: Vec<&Edge> = s
        .matrix
        .iter()
        .filter(|e| e.src == src && e.dst == dst)
        .collect();
    let ssh = edges.iter().find(|e| e.proto == "ssh");
    if ssh.is_some_and(|e| !e.ok) {
        return match st.hosts.get(dst) {
            Some(h) if h.host_key.is_none() => {
                "host not answering over ssh · bootstrap not done".into()
            }
            _ => "host not answering over ssh".into(),
        };
    }
    if let Some(l) = st.link(src, dst) {
        let state = |p: Proto| {
            edges
                .iter()
                .find(|e| e.proto == p.name())
                .map(|e| if e.ok { "fine" } else { "down" })
                .unwrap_or("not probed")
        };
        let active = l.active.unwrap_or(l.protocols[0]);
        let backup: Vec<String> = l
            .protocols
            .iter()
            .filter(|p| **p != active)
            .map(|p| format!("{} {}", p.name(), state(*p)))
            .collect();
        return format!(
            "{} link · {} {}{}",
            if is_active_exit(st, dst) {
                "active"
            } else {
                "standby"
            },
            active.name(),
            state(active),
            if backup.is_empty() {
                String::new()
            } else {
                format!(" · backup {}", backup.join(", "))
            }
        );
    }
    let failed: Vec<&str> = edges
        .iter()
        .filter(|e| !e.ok && e.note.as_deref() != Some("udp"))
        .map(|e| e.proto.as_str())
        .collect();
    match st.hosts.get(dst).map(|h| h.role) {
        Some(Role::Exit) if failed.is_empty() => "direct access to the exit".into(),
        _ if failed.is_empty() => String::new(),
        _ => format!("{} not answering", failed.join(", ")),
    }
}

fn rank(l: Level) -> u8 {
    match l {
        Level::Off => 0,
        Level::Ok => 1,
        Level::Warn => 2,
        Level::Bad => 3,
    }
}

/// Worst level per day over the last `days` (the last bucket ends at `now`) from (ts, ok, ms); None — no data.
pub fn day_buckets(hist: &[(i64, bool, Option<u64>)], now: i64, days: usize) -> Vec<Option<Level>> {
    let start = now - days as i64 * DAY;
    (0..days)
        .map(|d| {
            let (from, to) = (start + d as i64 * DAY, start + (d as i64 + 1) * DAY);
            hist.iter()
                .filter(|(ts, _, _)| *ts >= from && *ts < to)
                .map(|(_, ok, ms)| {
                    if !ok {
                        Level::Bad
                    } else if ms.is_some_and(|m| m > PROBE_SLOW_MS) {
                        Level::Warn
                    } else {
                        Level::Ok
                    }
                })
                .max_by_key(|l| rank(*l))
        })
        .collect()
}

// ---------- traffic, links, exit, DNS ----------

/// Link outbound traffic per day: only `<exit>-<proto>` tags (`link-*`/`exit-auto` groups double-count them).
pub fn link_traffic(s: &Status, host: &str, l: &Link) -> u64 {
    let Some(o) = s.outbounds.get(host) else {
        return 0;
    };
    l.protocols
        .iter()
        .filter_map(|p| o.get(&format!("{}-{}", l.to, p.name())))
        .map(|t| t.up + t.down)
        .sum()
}

/// (direct, via exit) per day from the host's outbounds.
pub fn traffic_split(st: &State, s: &Status, host: &str) -> (u64, u64) {
    let direct = s
        .outbounds
        .get(host)
        .and_then(|o| o.get("direct"))
        .map(|t| t.up + t.down)
        .unwrap_or(0);
    let exit = st.links_from(host).map(|l| link_traffic(s, host, l)).sum();
    (direct, exit)
}

/// Edge latency text from the last matrix run.
pub fn link_ms(s: &Status, from: &str, to: &str, proto: Proto) -> Option<String> {
    s.matrix
        .iter()
        .find(|e| e.src == from && e.dst == to && e.proto == proto.name())
        .map(|e| edge_text(Some(e)))
}

/// Exit policy of an entry: `auto (urltest), prefer fi` / `fixed fi`.
pub fn exit_text(r: &Routing) -> String {
    let prefer = r.exit.prefer.as_deref();
    match (r.exit.policy, prefer) {
        (ExitPolicy::Auto, Some(p)) => format!("auto (urltest), prefer {p}"),
        (ExitPolicy::Auto, None) => "auto (urltest)".into(),
        (ExitPolicy::Fixed, Some(p)) => format!("fixed {p}"),
        (ExitPolicy::Fixed, None) => "fixed, no exit chosen".into(),
    }
}

pub fn dns_text(r: &Routing) -> Vec<String> {
    vec![
        format!(
            "domains from direct sets → {} via direct · everything else → {} via link",
            r.dns
                .direct
                .iter()
                .map(|i| i.to_string())
                .collect::<Vec<_>>()
                .join(", "),
            r.dns.remote
        ),
        "awg clients get DNS = entry address, answered by the same engine by these rules".into(),
    ]
}

/// Enabled users of an entry with a protocol (awg — peers, mtg — tg proxy users).
pub fn inbound_users(st: &State, entry: &str, proto: Proto) -> usize {
    st.users
        .values()
        .filter(|u| u.enabled && st.entry_of(u).as_deref() == Some(entry))
        .filter(|u| match proto {
            Proto::Awg => u.awg.is_some(),
            Proto::Mtg => u.tg,
            p => u.protocols.contains(&p),
        })
        .count()
}

/// Three-column rows with a level: (name, middle, status).
pub type Row3 = (String, String, String, Level);

/// Inbound rows of a host: (proto label, `tcp 443`, status, level).
pub fn inbound_rows(st: &State, name: &str) -> Vec<Row3> {
    let Some(h) = st.hosts.get(name) else {
        return vec![];
    };
    let mut rows: Vec<Row3> = st
        .inbounds
        .get(name)
        .map(|m| {
            m.iter()
                .map(|(p, i)| {
                    let (label, transport) = match p {
                        Proto::Reality => ("reality", "tcp"),
                        Proto::Ws => ("ws (caddy)", "tcp"),
                        Proto::Hy2 => ("hysteria2", "udp"),
                        Proto::Awg => ("awg", "udp"),
                        Proto::Mtg => ("mtproto (mtg)", "tcp"),
                    };
                    let port = match p {
                        Proto::Ws => format!("{transport} 443 →{}", i.port),
                        _ => format!("{transport} {}", i.port),
                    };
                    let (status, level) = if !i.enabled {
                        ("off".to_string(), Level::Off)
                    } else if h.role == Role::Entry {
                        let n = inbound_users(st, name, *p);
                        let what = if *p == Proto::Awg { "peers" } else { "users" };
                        (
                            format!("on · {n} {what}"),
                            if n == 0 { Level::Warn } else { Level::Ok },
                        )
                    } else {
                        ("on · link inbound".to_string(), Level::Ok)
                    };
                    (label.to_string(), port, status, level)
                })
                .collect()
        })
        .unwrap_or_default();
    if h.role == Role::Entry {
        rows.push((
            "probe (socks)".into(),
            format!("127.0.0.1:{}", crate::render::PROBE_PORT),
            "service · mtg upstream".into(),
            Level::Off,
        ));
    }
    rows
}

/// Exit rows of an entry: direct and each link with its interface and traffic per day.
pub fn exit_rows(st: &State, s: &Status, name: &str) -> Vec<Row3> {
    if st.hosts.get(name).is_none_or(|h| h.role != Role::Entry) {
        return vec![];
    }
    let (direct, _) = traffic_split(st, s, name);
    let mut rows = vec![(
        "direct".to_string(),
        "—".to_string(),
        format!("{}/day", gb(direct)),
        Level::Ok,
    )];
    for (i, l) in st.links_from(name).enumerate() {
        let active = is_active_exit(st, &l.to);
        rows.push((
            l.to.clone(),
            if l.protocols.contains(&Proto::Awg) {
                format!("awg{i}")
            } else {
                l.active.unwrap_or(l.protocols[0]).name().into()
            },
            format!(
                "{} · {}/day",
                if active { "active" } else { "standby" },
                gb(link_traffic(s, name, l))
            ),
            if active { Level::Ok } else { Level::Off },
        ));
    }
    rows
}

/// One link for the overview "Links between nodes": (text, level) parts.
pub fn link_parts(s: &Status, l: &Link) -> Vec<(String, Level)> {
    let mut v: Vec<(String, Level)> = l
        .protocols
        .iter()
        .map(|p| {
            let e = s
                .matrix
                .iter()
                .find(|e| e.src == l.from && e.dst == l.to && e.proto == p.name());
            let mut t = format!(
                "{} {}",
                p.name(),
                e.map(|e| edge_text(Some(e))).unwrap_or("—".into())
            );
            if l.active == Some(*p) {
                t.push_str(" active");
            }
            (
                t,
                if e.is_none() {
                    Level::Off
                } else {
                    edge_level(e)
                },
            )
        })
        .collect();
    v.push((
        format!("{}/day", gb(link_traffic(s, &l.from, l))),
        Level::Off,
    ));
    v
}

// ---------- plan ----------

#[derive(Clone, Debug, PartialEq)]
pub enum PlanRow {
    Host(String, Level),
    File {
        host: String,
        path: String,
        status: FileStatus,
        level: Level,
    },
    Note(String, Level),
}

/// Left panel of the plan screen; `order` — apply order, `roles` — host → role text.
pub fn plan_rows(
    plans: &[HostPlan],
    order: &[String],
    head: Option<&str>,
    roles: &BTreeMap<String, String>,
) -> Vec<PlanRow> {
    let mut out = vec![];
    for p in plans {
        let role = roles.get(&p.name).cloned().unwrap_or_default();
        let order_text = match order.iter().position(|n| *n == p.name).map(|i| i + 1) {
            Some(i) if i == order.len() => format!("order {i} (last)"),
            Some(i) => format!("order {i}"),
            None => String::new(),
        };
        if let Some(e) = &p.unreachable {
            out.push(PlanRow::Host(
                format!("{} · {role} · ✗ {e}", p.name),
                Level::Bad,
            ));
            continue;
        }
        if !p.bootstrapped {
            out.push(PlanRow::Host(
                format!("{} · skipped: bootstrap not done", p.name),
                Level::Off,
            ));
            continue;
        }
        let from = p.marker.as_deref().unwrap_or("—");
        let to = head.unwrap_or("HEAD");
        let changes = p.changes();
        out.push(PlanRow::Host(
            if changes == 0 {
                format!("{} · {role} · unchanged · {order_text}", p.name)
            } else {
                format!("{} · {role} · {from} → {to} · {order_text}", p.name)
            },
            if changes == 0 { Level::Off } else { Level::Ok },
        ));
        for (path, s) in &p.files {
            let level = match s {
                FileStatus::Unchanged => continue,
                FileStatus::Changed | FileStatus::New => Level::Ok,
                FileStatus::Removed | FileStatus::Drift => Level::Warn,
            };
            out.push(PlanRow::File {
                host: p.name.clone(),
                path: path.clone(),
                status: *s,
                level,
            });
            if *s == FileStatus::Drift {
                out.push(PlanRow::Note(
                    "will be overwritten · d show diff · m adopt into repo".into(),
                    Level::Off,
                ));
            }
        }
        for n in &p.notes {
            out.push(PlanRow::Note(format!("! {n}"), Level::Warn));
        }
    }
    out
}

/// "Checks before apply": (level, text).
pub fn plan_checks(
    st: &State,
    s: &Status,
    plans: &[HostPlan],
    branch: &str,
    clean: bool,
    reproducible: bool,
) -> Vec<(Level, String)> {
    let mut v = vec![
        if clean && branch == "main" {
            (Level::Ok, "clean tree, branch main".into())
        } else {
            (
                Level::Bad,
                format!(
                    "branch {branch}, tree {}",
                    if clean { "clean" } else { "dirty" }
                ),
            )
        },
        if reproducible {
            (
                Level::Ok,
                "render reproducible · engine check on every config during apply".into(),
            )
        } else {
            (Level::Bad, "render is not reproducible".into())
        },
    ];
    for l in &st.links {
        let probe = |p: Proto| {
            s.matrix
                .iter()
                .find(|e| e.src == l.from && e.dst == l.to && e.proto == p.name())
        };
        let protos: Vec<String> = l
            .protocols
            .iter()
            .map(|p| {
                format!(
                    "{} {}",
                    p.name(),
                    match probe(*p) {
                        Some(e) if e.ok => "●",
                        Some(_) => "✗",
                        None => "—",
                    }
                )
            })
            .collect();
        let ok = l.protocols.iter().any(|p| probe(*p).is_some_and(|e| e.ok));
        v.push((
            if ok { Level::Ok } else { Level::Warn },
            format!("{} answers in the matrix: {}", l.to, protos.join(" ")),
        ));
    }
    let no_cert: Vec<&str> = st
        .hosts
        .iter()
        .filter(|(n, h)| {
            h.role == Role::Entry && h.domain.is_some() && s.hosts[*n].cert_days.is_none()
        })
        .map(|(n, _)| n.as_str())
        .collect();
    v.push(if no_cert.is_empty() {
        (Level::Ok, "certificates in place on all hosts".into())
    } else {
        (
            Level::Warn,
            format!("no certificate data for {}", no_cert.join(", ")),
        )
    });
    for p in plans {
        if p.files.iter().any(|(_, s)| *s == FileStatus::Drift) {
            v.push((
                Level::Warn,
                format!("drift on {} · decision needed", p.name),
            ));
        }
    }
    v
}

// ---------- trace ----------

#[derive(Clone, Debug, PartialEq)]
pub struct TraceStep {
    pub group: Group,
    pub idx: usize,
    pub text: String,
    pub matched: bool,
}

fn domain_matches(pattern: &str, host: &str) -> bool {
    if let Some(p) = pattern.strip_prefix("full:") {
        host == p
    } else if let Some(p) = pattern.strip_prefix("keyword:") {
        host.contains(p)
    } else if pattern.starts_with("regexp:") {
        false
    } else {
        host == pattern || host.ends_with(&format!(".{pattern}"))
    }
}

/// Does the rule match `host`/`ip`/`port`? → (matched, why). Conditions of one rule are OR'ed;
/// geosite/geoip cannot be checked offline: reported and treated as no match.
pub fn rule_matches(
    st: &State,
    m: &Match,
    host: &str,
    ip: Option<std::net::IpAddr>,
    port: Option<u16>,
) -> (bool, String) {
    if m.is_empty() {
        return (true, "everything else".into());
    }
    let mut why = vec![];
    for name in &m.sets {
        let Some(s) = st.sets.get(name) else {
            why.push(format!("{name} — unknown set"));
            continue;
        };
        if let Some(d) = s.domains.iter().find(|d| domain_matches(d, host)) {
            return (true, format!("matched {d} in set {name}"));
        }
        if let Some(ip) = ip
            && s.ips.iter().any(|n| n.contains(&ip))
        {
            return (true, format!("matched {ip} in set {name}"));
        }
        why.push(format!(
            "{name} — not in the set{}",
            if s.geosite.is_empty() && s.geoip.is_empty() {
                ""
            } else {
                " (geodata not checked offline)"
            }
        ));
    }
    if let Some(d) = m.domains.iter().find(|d| domain_matches(d, host)) {
        return (true, format!("matched {d}"));
    }
    if let Some(ip) = ip
        && let Some(n) = m.ip.iter().find(|n| n.contains(&ip))
    {
        return (true, format!("matched {n}"));
    }
    if let Some(p) = port
        && m.port.contains(&p)
    {
        return (true, format!("matched port {p}"));
    }
    why.extend(
        m.geosite
            .iter()
            .map(|g| format!("geosite:{g} — geodata not checked offline")),
    );
    why.extend(
        m.geoip
            .iter()
            .map(|g| format!("geoip:{g} — geodata not checked offline")),
    );
    if !m.inbound.is_empty() {
        why.push("not a service inbound".into());
    }
    if !m.domains.is_empty() || !m.ip.is_empty() || !m.port.is_empty() {
        why.push("no match".into());
    }
    (false, why.join(", "))
}

/// Walk the entry's rules for a user mode and an address `host[:port]` or `ip[:port]`:
/// the steps and the result (`→ link fi-hel-1 (auto)`, `direct`, `block`, or the engine default).
pub fn trace(st: &State, entry: &str, mode: Mode, addr: &str) -> (Vec<TraceStep>, String) {
    let (host, port) = match addr.rsplit_once(':') {
        Some((h, p)) if p.parse::<u16>().is_ok() => (h, p.parse().ok()),
        _ => (addr, None),
    };
    let ip: Option<std::net::IpAddr> = host.parse().ok();
    let mut steps = vec![];
    let Some(r) = st.routing.get(entry) else {
        return (steps, "no routing for the entry".into());
    };
    let groups = [
        Group::All,
        if mode == Mode::Split {
            Group::Split
        } else {
            Group::Full
        },
    ];
    for g in groups {
        for (i, rule) in group_rules(r, g).iter().enumerate() {
            if !rule.enabled {
                steps.push(TraceStep {
                    group: g,
                    idx: i + 1,
                    text: "disabled".into(),
                    matched: false,
                });
                continue;
            }
            let (ok, why) = rule_matches(st, &rule.match_, host, ip, port);
            steps.push(TraceStep {
                group: g,
                idx: i + 1,
                text: why,
                matched: ok,
            });
            if ok {
                let result = match &rule.action {
                    Action::Link { link } if link == "auto" => match preferred_exit(st, entry) {
                        Some(e) => format!("→ link {e} (auto)"),
                        None => "→ link auto".into(),
                    },
                    a => action_text(a),
                };
                return (steps, result);
            }
        }
    }
    (steps, "direct (engine default, nothing matched)".into())
}

// ---------- forms ----------

#[derive(Clone, Debug, PartialEq)]
pub enum Kind {
    /// `edit` — the name when editing, otherwise a new user.
    User { edit: Option<String> },
    /// `idx` — rule number when editing, otherwise a new rule.
    Rule {
        host: String,
        group: Group,
        idx: Option<usize>,
    },
    /// Link `from->to`: edited when it exists, created otherwise.
    Link,
    /// Exit policy of an entry.
    Exit { host: String },
}

#[derive(Clone, Debug, PartialEq)]
pub struct Form {
    pub title: String,
    pub kind: Kind,
    pub fields: Vec<(&'static str, String)>,
    pub cur: usize,
    pub error: Option<String>,
}

impl Form {
    fn new(title: impl Into<String>, kind: Kind, fields: &[(&'static str, String)]) -> Form {
        Form {
            title: title.into(),
            kind,
            fields: fields.to_vec(),
            cur: 0,
            error: None,
        }
    }
    pub fn next(&mut self) {
        self.cur = (self.cur + 1) % self.fields.len();
    }
    pub fn prev(&mut self) {
        self.cur = (self.cur + self.fields.len() - 1) % self.fields.len();
    }
    pub fn input(&mut self, c: char) {
        self.fields[self.cur].1.push(c);
    }
    pub fn backspace(&mut self) {
        self.fields[self.cur].1.pop();
    }
    pub fn get(&self, label: &str) -> &str {
        self.fields
            .iter()
            .find(|(l, _)| *l == label)
            .map(|(_, v)| v.trim())
            .unwrap_or("")
    }
}

fn csv(s: &str) -> Vec<String> {
    s.split(',')
        .map(str::trim)
        .filter(|x| !x.is_empty())
        .map(String::from)
        .collect()
}

fn list<T: FromStr<Err = anyhow::Error>>(s: &str) -> Result<Vec<T>> {
    csv(s).iter().map(|x| x.parse()).collect()
}

fn opt(s: &str) -> Option<String> {
    (!s.is_empty()).then(|| s.to_string())
}

fn join<T: Copy>(v: &[T], f: impl Fn(T) -> &'static str) -> String {
    v.iter().map(|x| f(*x)).collect::<Vec<_>>().join(",")
}

pub fn user_form(st: &State, edit: Option<&str>) -> Form {
    let u = edit.and_then(|n| st.users.get(n));
    let mut f = vec![
        ("name", edit.unwrap_or("").to_string()),
        (
            "modes",
            u.map(|u| join(&u.modes, Mode::name))
                .unwrap_or("split".into()),
        ),
        (
            "protocols",
            u.map(|u| join(&u.protocols, Proto::name))
                .unwrap_or("reality,ws,hy2".into()),
        ),
        (
            "tg proxy (yes/no)",
            if u.is_some_and(|u| u.tg) { "yes" } else { "no" }.into(),
        ),
        (
            "expiry (YYYY-MM-DD)",
            u.and_then(|u| u.expires.clone()).unwrap_or_default(),
        ),
        ("note", u.and_then(|u| u.note.clone()).unwrap_or_default()),
    ];
    if u.is_none() {
        f.push(("awg peer (split/full)", String::new()));
        f.push(("entry", String::new()));
    }
    Form::new(
        match edit {
            Some(n) => format!("User {n}"),
            None => "New user".into(),
        },
        Kind::User {
            edit: edit.map(String::from),
        },
        &f,
    )
}

pub fn rule_form(st: &State, host: &str, g: Group, idx: Option<usize>) -> Form {
    let r = idx.and_then(|i| {
        st.routing
            .get(host)
            .and_then(|r| group_rules(r, g).get(i - 1))
    });
    let m = r.map(|r| r.match_.clone()).unwrap_or_default();
    let mut f = vec![];
    if idx.is_none() {
        f.push(("position", String::new()));
    }
    f.extend([
        ("sets", m.sets.join(",")),
        ("geosite", m.geosite.join(",")),
        ("geoip", m.geoip.join(",")),
        ("domains", m.domains.join(",")),
        (
            "ip",
            m.ip.iter()
                .map(|i| i.to_string())
                .collect::<Vec<_>>()
                .join(","),
        ),
        ("inbound", m.inbound.join(",")),
        (
            "port",
            m.port
                .iter()
                .map(|p| p.to_string())
                .collect::<Vec<_>>()
                .join(","),
        ),
        (
            "action",
            r.map(|r| r.action.to_string()).unwrap_or("direct".into()),
        ),
        ("note", r.and_then(|r| r.note.clone()).unwrap_or_default()),
    ]);
    Form::new(
        format!(
            "{} · {} · {}",
            host,
            group_name(g),
            idx.map(|i| format!("rule #{i}"))
                .unwrap_or("new rule".into())
        ),
        Kind::Rule {
            host: host.into(),
            group: g,
            idx,
        },
        &f,
    )
}

pub fn link_form(st: &State, from: &str, to: &str) -> Form {
    let l = st.link(from, to);
    Form::new(
        if l.is_some() {
            format!("Link {from} → {to}")
        } else {
            "New link".into()
        },
        Kind::Link,
        &[
            ("from", from.into()),
            ("to", to.into()),
            (
                "protocols",
                l.map(|l| join(&l.protocols, Proto::name))
                    .unwrap_or("awg,reality".into()),
            ),
            (
                "policy (auto/fixed/rr)",
                l.map(|l| {
                    serde_yaml::to_string(&l.policy)
                        .unwrap_or_default()
                        .trim()
                        .into()
                })
                .unwrap_or("auto".into()),
            ),
            (
                "active",
                l.and_then(|l| l.active)
                    .map(|p| p.name().into())
                    .unwrap_or_default(),
            ),
        ],
    )
}

pub fn exit_form(st: &State, host: &str) -> Form {
    let r = st.routing.get(host).cloned().unwrap_or_default();
    Form::new(
        format!("Exit of {host}"),
        Kind::Exit { host: host.into() },
        &[
            (
                "policy (auto/fixed)",
                serde_yaml::to_string(&r.exit.policy)
                    .unwrap_or_default()
                    .trim()
                    .into(),
            ),
            ("prefer", r.exit.prefer.unwrap_or_default()),
        ],
    )
}

fn policy(s: &str) -> Result<Policy> {
    Ok(match s {
        "auto" => Policy::Auto,
        "fixed" => Policy::Fixed,
        "rr" => Policy::Rr,
        _ => bail!("policy: auto, fixed or rr"),
    })
}

fn yes(s: &str) -> Result<bool> {
    match s {
        "yes" | "y" | "1" | "да" => Ok(true),
        "no" | "n" | "0" | "нет" | "" => Ok(false),
        _ => bail!("tg proxy: yes or no"),
    }
}

/// Parse the form and run the operation; returns the commit message. Only the in-memory state changes.
pub fn submit(f: &Form, st: &mut State) -> Result<String> {
    match &f.kind {
        Kind::User { edit: None } => {
            let name = f.get("name");
            if name.is_empty() {
                bail!("name is empty");
            }
            ops::user_add(
                st,
                name,
                list(f.get("modes"))?,
                list(f.get("protocols"))?,
                opt(f.get("awg peer (split/full)"))
                    .map(|m| m.parse())
                    .transpose()?,
                yes(f.get("tg proxy (yes/no)"))?,
                opt(f.get("expiry (YYYY-MM-DD)")),
                opt(f.get("note")),
                opt(f.get("entry")),
            )
        }
        Kind::User { edit: Some(name) } => ops::user_edit(
            st,
            name,
            ops::UserPatch {
                modes: Some(list(f.get("modes"))?),
                protocols: Some(list(f.get("protocols"))?),
                expires: Some(opt(f.get("expiry (YYYY-MM-DD)"))),
                note: opt(f.get("note")),
                tg: Some(yes(f.get("tg proxy (yes/no)"))?),
            },
        ),
        Kind::Rule { host, group, idx } => {
            let m = Match {
                sets: csv(f.get("sets")),
                geosite: csv(f.get("geosite")),
                geoip: csv(f.get("geoip")),
                domains: csv(f.get("domains")),
                ip: csv(f.get("ip"))
                    .iter()
                    .map(|s| s.parse().with_context(|| format!("ip «{s}»")))
                    .collect::<Result<_>>()?,
                inbound: csv(f.get("inbound")),
                port: csv(f.get("port"))
                    .iter()
                    .map(|s| s.parse().with_context(|| format!("port «{s}»")))
                    .collect::<Result<_>>()?,
            };
            let action: Action = f.get("action").parse()?;
            let note = opt(f.get("note"));
            match idx {
                None => {
                    let pos = opt(f.get("position"))
                        .map(|p| p.parse().context("position must be a number"))
                        .transpose()?;
                    ops::rule_add(st, host, *group, pos, m, action, note)
                }
                Some(i) => ops::rule_edit(st, host, *group, *i, Some(m), Some(action), note),
            }
        }
        Kind::Link => {
            let (from, to) = (f.get("from"), f.get("to"));
            let protos: Vec<Proto> = list(f.get("protocols"))?;
            let pol = policy(f.get("policy (auto/fixed/rr)"))?;
            let active = opt(f.get("active")).map(|a| a.parse()).transpose()?;
            match st.link(from, to) {
                None => ops::link_add(st, from, to, protos, pol, active),
                Some(l) => {
                    let add = protos
                        .iter()
                        .filter(|p| !l.protocols.contains(p))
                        .copied()
                        .collect();
                    let rm = l
                        .protocols
                        .iter()
                        .filter(|p| !protos.contains(p))
                        .copied()
                        .collect();
                    ops::link_set(st, from, to, active, Some(pol), add, rm)
                }
            }
        }
        Kind::Exit { host } => {
            let pol = match f.get("policy (auto/fixed)") {
                "auto" => ExitPolicy::Auto,
                "fixed" => ExitPolicy::Fixed,
                _ => bail!("policy: auto or fixed"),
            };
            ops::exit_set(st, host, opt(f.get("prefer")), Some(pol))
        }
    }
}

/// Live check of a form: operation and validation on a copy of the state →
/// (commit message, rows of the rule group after the change).
pub fn preview(f: &Form, st: &State) -> Result<(String, Vec<Row>)> {
    let mut c = st.clone();
    let msg = submit(f, &mut c)?;
    let errs = crate::validate::validate(&c);
    if !errs.is_empty() {
        bail!("{}", errs.join("; "));
    }
    let rows = match &f.kind {
        Kind::Rule { host, group, .. } => rule_rows(&c, host)
            .into_iter()
            .filter(|(g, _, _)| g == group)
            .map(|(_, _, r)| r)
            .collect(),
        _ => vec![],
    };
    Ok((msg, rows))
}

// ---------- cards ----------

/// User links: (label, url) — subscriptions per mode and the Telegram proxy.
pub fn user_links(st: &State, name: &str) -> Result<Vec<(String, String)>> {
    use crate::render::subs;
    let u = st
        .users
        .get(name)
        .with_context(|| format!("user «{name}» is not described"))?;
    let entry = st.entry_of(u).context("the user has no entry")?;
    let mut v = vec![];
    for m in &u.modes {
        v.push((
            format!("subscription {}", m.name()),
            subs::sub_url(st, &entry, name, *m)?,
        ));
    }
    if u.tg
        && let Some(t) = subs::tg_link(st, &entry)
    {
        v.push(("Telegram proxy".into(), t));
    }
    Ok(v)
}

fn mb(b: u64) -> String {
    format!("{} MB", b >> 20)
}

fn or_dash<T: ToString>(v: Option<T>) -> String {
    v.map(|x| x.to_string()).unwrap_or("—".into())
}

/// Key/value lines with a level for the value (host cards).
pub type Kv = Vec<(String, String, Level)>;

/// "State" card: commit, drift, engine and service versions pinned in `versions.yaml`.
pub fn host_state(st: &State, s: &Status, name: &str, head: Option<&str>) -> Kv {
    let Some(h) = st.hosts.get(name) else {
        return vec![];
    };
    let l = s.hosts[name].last.as_ref();
    let ver = |k: &str| st.versions.get(k).map(|p| p.version.clone());
    let mut v = vec![
        match l.and_then(|l| l.marker.as_deref()) {
            Some(m) if head == Some(m) => ("commit".into(), format!("{m} = HEAD"), Level::Ok),
            Some(m) => ("commit".into(), format!("{m} · behind HEAD"), Level::Warn),
            None => ("commit".into(), "not applied yet".into(), Level::Off),
        },
        match l.and_then(|l| l.drift) {
            Some(true) => ("drift".into(), "yes".into(), Level::Warn),
            Some(false) => ("drift".into(), "none".into(), Level::Ok),
            None => ("drift".into(), "—".into(), Level::Off),
        },
    ];
    let engine = match h.engine {
        Engine::SingBox => "sing-box",
        Engine::Xray => "xray",
    };
    // ponytail: installed versions are not collected — these are the pins from versions.yaml
    v.push((engine.into(), or_dash(ver(engine)), Level::Ok));
    if h.engine == Engine::SingBox {
        v.push((
            "geodata".into(),
            format!(
                "{}{}",
                or_dash(ver("geodata")),
                if st
                    .versions
                    .get("geodata")
                    .is_some_and(|p| p.sha256.is_some())
                {
                    " · sha ✓"
                } else {
                    ""
                }
            ),
            Level::Ok,
        ));
    }
    for svc in services(st, name) {
        if svc == engine {
            continue;
        }
        v.push((
            svc.into(),
            match svc {
                "awg" => "kernel module (dkms)".into(),
                "mtg" => format!("{} · via socks", or_dash(ver("mtg"))),
                s => or_dash(ver(s)),
            },
            Level::Ok,
        ));
    }
    v
}

/// "Machine" card from the last collect; dashes before the first one.
pub fn host_machine(s: &Status, name: &str, now: i64) -> Kv {
    let l = s.hosts.get(name).and_then(|h| h.last.as_ref());
    let m = l.map(|l| &l.m);
    let lvl = |warn: bool| {
        if m.is_none() {
            Level::Off
        } else if warn {
            Level::Warn
        } else {
            Level::Ok
        }
    };
    vec![
        ("load".into(), or_dash(m.and_then(|m| m.load1)), lvl(false)),
        (
            "memory".into(),
            format!(
                "{} of {}",
                or_dash(m.and_then(|m| m.mem_used).map(mb)),
                or_dash(m.and_then(|m| m.mem_total).map(mb))
            ),
            lvl(false),
        ),
        (
            "disk".into(),
            format!(
                "{} of {}",
                or_dash(
                    m.and_then(|m| m.disk_used)
                        .map(|b| format!("{} GB", b >> 30))
                ),
                or_dash(
                    m.and_then(|m| m.disk_total)
                        .map(|b| format!("{} GB", b >> 30))
                )
            ),
            lvl(false),
        ),
        (
            "connections".into(),
            or_dash(m.and_then(|m| m.conns).map(|c| format!("{c} established"))),
            lvl(false),
        ),
        (
            "uptime".into(),
            or_dash(
                m.and_then(|m| m.uptime)
                    .map(|u| format!("{} d", u as i64 / DAY)),
            ),
            lvl(false),
        ),
        (
            "updates".into(),
            match m.and_then(|m| m.updates) {
                None => "—".into(),
                Some(u) => format!(
                    "{u} pending{}",
                    if m.and_then(|m| m.reboot) == Some(true) {
                        " · reboot required"
                    } else {
                        ""
                    }
                ),
            },
            lvl(m.and_then(|m| m.updates).is_some_and(|u| u > 0)
                || m.and_then(|m| m.reboot) == Some(true)),
        ),
        (
            "collected".into(),
            or_dash(l.map(|l| ago(l.ts, now))),
            Level::Off,
        ),
    ]
}

/// "Domain and certificate" card. `resolved` — what the domain resolves to (None: not checked).
pub fn host_domain(
    st: &State,
    s: &Status,
    name: &str,
    resolved: Option<&[std::net::IpAddr]>,
) -> Kv {
    let Some(h) = st.hosts.get(name) else {
        return vec![];
    };
    let mut v = vec![];
    match &h.domain {
        None => v.push(("domain".into(), "— (by IP)".into(), Level::Off)),
        Some(d) => {
            v.push(("domain".into(), d.clone(), Level::Ok));
            v.push(match resolved {
                None => ("A record".into(), "— not checked".into(), Level::Off),
                Some(ips) if ips.contains(&h.ip) => {
                    ("A record".into(), "✓ matches".into(), Level::Ok)
                }
                Some([]) => ("A record".into(), "✗ does not resolve".into(), Level::Bad),
                Some(ips) => (
                    "A record".into(),
                    format!("✗ points to {}", ips[0]),
                    Level::Bad,
                ),
            });
            v.push(("ACME".into(), "Caddy".into(), Level::Ok));
        }
    }
    v.push(match s.hosts[name].cert_days {
        Some(d) => (
            "expires".into(),
            format!("in {d} d"),
            if d < CERT_WARN_DAYS {
                Level::Warn
            } else {
                Level::Ok
            },
        ),
        None => ("expires".into(), "—".into(), Level::Off),
    });
    v.push(match &h.host_key {
        Some(k) => (
            "SSH".into(),
            format!(
                "{} · fingerprint ✓",
                k.split_whitespace()
                    .next()
                    .unwrap_or("")
                    .trim_start_matches("ssh-")
            ),
            Level::Ok,
        ),
        None => (
            "SSH".into(),
            "host key not recorded (bootstrap)".into(),
            Level::Warn,
        ),
    });
    v
}

/// Graph nodes for the topology screen: (name, subtitle, level) — laptop, entries, then exits.
pub fn graph_nodes(st: &State, s: &Status, head: Option<&str>) -> Vec<(String, String, Level)> {
    let mut v = vec![(metrics::LAPTOP.to_string(), "tool".to_string(), Level::Ok)];
    let mut hosts: Vec<(&String, &Host)> = st.hosts.iter().collect();
    hosts.sort_by_key(|(_, h)| h.role == Role::Exit);
    for (n, _) in hosts {
        v.push((n.clone(), role_text(st, n), host_level(&s.hosts[n], head)));
    }
    v
}
