//! Чистые функции TUI: строки таблиц, уровень статуса, навигация, фильтр, формы.
//! Ни терминала, ни SSH — всё проверяется тестами на фикстуре.
use crate::metrics::{self, Edge, HostOut, Status, UserOut};
use crate::ops;
use crate::state::*;
use anyhow::{Context, Result, bail};
use std::str::FromStr;

pub const CERT_WARN_DAYS: i64 = 14;
pub const EXPIRES_WARN_DAYS: i64 = 7;
const DAY: i64 = 86_400;

/// Статус строки: зелёный / жёлтый / красный / серый.
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

// ---------- навигация и фильтр ----------

/// Сдвиг выбора без переноса по кругу.
pub fn step(cur: usize, len: usize, delta: isize) -> usize {
    if len == 0 {
        return 0;
    }
    (cur as isize + delta).clamp(0, len as isize - 1) as usize
}

/// Индексы строк, в которых есть подстрока `q` (без регистра); пустой запрос — все.
pub fn filter(rows: &[Row], q: &str) -> Vec<usize> {
    let q = q.to_lowercase();
    rows.iter()
        .enumerate()
        .filter(|(_, r)| q.is_empty() || r.cells.iter().any(|c| c.to_lowercase().contains(&q)))
        .map(|(i, _)| i)
        .collect()
}

// ---------- форматирование ----------

pub fn gb(b: u64) -> String {
    if b == 0 {
        "0".into()
    } else {
        format!("{:.1} ГБ", b as f64 / 1e9)
    }
}

/// «N мин назад» от двух epoch.
pub fn ago(ts: i64, now: i64) -> String {
    let d = (now - ts).max(0);
    match d {
        0..60 => "только что".into(),
        60..3600 => format!("{} мин назад", d / 60),
        3600..86400 => format!("{} ч назад", d / 3600),
        _ => format!("{} дн назад", d / DAY),
    }
}

/// `YYYY-MM-DD` → epoch полуночи UTC (алгоритм days_from_civil).
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

fn role_text(h: &Host) -> String {
    match (h.role, &h.via) {
        (Role::Entry, _) => "вход".into(),
        (Role::Exit, Some(v)) => format!("выход via {v}"),
        (Role::Exit, None) => "выход".into(),
    }
}

// ---------- уровни ----------

/// Что не так с хостом: (уровень, причина). Пусто — в норме или ещё не собирался.
pub fn host_issues(h: &HostOut, head: Option<&str>) -> Vec<(Level, String)> {
    let Some(l) = &h.last else {
        return vec![];
    };
    let mut v = vec![];
    if !l.reachable {
        v.push((
            Level::Bad,
            format!("✗ {}", l.error.as_deref().unwrap_or("нет ответа")),
        ));
        return v;
    }
    match l.m.probe_code {
        Some(204) => {}
        Some(c) => v.push((Level::Bad, format!("проба {c}"))),
        None => v.push((Level::Bad, "проба без ответа".into())),
    }
    if l.drift == Some(true) {
        v.push((Level::Warn, "дрейф".into()));
    }
    if head.is_some_and(|hd| l.marker.as_deref().is_some_and(|m| m != hd)) {
        v.push((Level::Warn, "отстаёт от HEAD".into()));
    }
    if let Some(d) = h.cert_days.filter(|d| *d < CERT_WARN_DAYS) {
        v.push((Level::Warn, format!("сертификат {d} дн")));
    }
    if let Some(u) = l.m.updates.filter(|u| *u > 0) {
        v.push((Level::Warn, format!("обновлений {u}")));
    }
    if l.m.reboot == Some(true) {
        v.push((Level::Warn, "нужна перезагрузка".into()));
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
        Some(e) if e.ok => Level::Ok,
        Some(e) if e.note.as_deref() == Some("udp") => Level::Off,
        Some(_) => Level::Bad,
    }
}

// ---------- строки таблиц ----------

pub const HOST_HEADER: [&str; 9] = [
    "хост",
    "роль",
    "домен",
    "ssh",
    "коммит",
    "дрейф",
    "проба",
    "серт",
    "обновл.",
];

pub fn host_rows(st: &State, s: &Status, head: Option<&str>) -> Vec<Row> {
    st.hosts
        .iter()
        .map(|(name, h)| {
            let o = &s.hosts[name];
            let l = o.last.as_ref();
            let ssh = match l {
                None => "—".into(),
                Some(l) if !l.reachable => {
                    format!("✗ {}", l.error.as_deref().unwrap_or("нет ответа"))
                }
                Some(l) => l
                    .ssh_ms
                    .map(|ms| format!("● {ms} мс"))
                    .unwrap_or("●".into()),
            };
            let commit = match l.and_then(|l| l.marker.as_deref()) {
                None => "—".into(),
                Some(m) if head == Some(m) => format!("{m} = HEAD"),
                Some(m) => m.to_string(),
            };
            let probe = match l.map(|l| (l.m.probe_code, l.m.probe_ms)) {
                None => "—".into(),
                Some((Some(c), ms)) => {
                    format!(
                        "{} {c} за {} мс",
                        if c == 204 { "●" } else { "✗" },
                        ms.unwrap_or(0)
                    )
                }
                Some((None, _)) => "✗ нет ответа".into(),
            };
            let updates = match l.and_then(|l| l.m.updates) {
                None => "—".into(),
                Some(u) if l.and_then(|l| l.m.reboot) == Some(true) => format!("{u} ↻"),
                Some(u) => u.to_string(),
            };
            Row {
                cells: vec![
                    name.clone(),
                    role_text(h),
                    h.domain.clone().unwrap_or("— (по IP)".into()),
                    ssh,
                    commit,
                    if l.and_then(|l| l.drift) == Some(true) {
                        "дрейф".into()
                    } else {
                        String::new()
                    },
                    probe,
                    o.cert_days.map(|d| format!("{d} дн")).unwrap_or("—".into()),
                    updates,
                ],
                level: host_level(o, head),
            }
        })
        .collect()
}

pub const USER_HEADER: [&str; 7] = [
    "имя",
    "режимы",
    "статус",
    "срок",
    "сутки",
    "неделя",
    "всего",
];

pub fn user_rows(st: &State, s: &Status, now: i64) -> Vec<Row> {
    st.users
        .iter()
        .map(|(name, u)| {
            let o = &s.users[name];
            let level = user_level(o, u.expires.as_deref(), now);
            Row {
                cells: vec![
                    name.clone(),
                    u.modes
                        .iter()
                        .map(|m| m.name())
                        .collect::<Vec<_>>()
                        .join(" "),
                    if u.enabled {
                        "активен"
                    } else {
                        "отключён"
                    }
                    .into(),
                    match (&u.expires, level) {
                        (None, _) => "без срока".into(),
                        (Some(_), Level::Bad) => "истёк".into(),
                        (Some(e), _) => format!("до {e}"),
                    },
                    gb(o.day.down + o.day.up),
                    gb(o.week.down + o.week.up),
                    gb(o.total.down + o.total.up),
                ],
                level,
            }
        })
        .collect()
}

/// Состав набора: «ru-direct (geosite × 3 + 14 своих)»; неописанный — просто имя.
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
        p.push(format!("{own} своих"));
    }
    if p.is_empty() {
        format!("{name} (пустой)")
    } else {
        format!("{name} ({})", p.join(" + "))
    }
}

/// Условие правила словами; пустое — «всё остальное».
pub fn match_text(st: &State, m: &Match) -> String {
    let mut p = vec![];
    if !m.sets.is_empty() {
        p.push(format!(
            "набор {}",
            m.sets
                .iter()
                .map(|n| set_text(st, n))
                .collect::<Vec<_>>()
                .join(", ")
        ));
    }
    p.extend(m.geosite.iter().map(|g| format!("geosite:{g}")));
    p.extend(m.geoip.iter().map(|g| format!("geoip:{g}")));
    p.extend(m.domains.iter().cloned());
    p.extend(m.ip.iter().map(|i| i.to_string()));
    if !m.inbound.is_empty() {
        p.push(format!("inbound {}", m.inbound.join(", ")));
    }
    if !m.port.is_empty() {
        p.push(format!(
            "порт {}",
            m.port
                .iter()
                .map(|x| x.to_string())
                .collect::<Vec<_>>()
                .join(", ")
        ));
    }
    if p.is_empty() {
        "всё остальное".into()
    } else {
        p.join(" · ")
    }
}

pub fn action_text(a: &Action) -> String {
    match a {
        Action::Simple(Simple::Direct) => "direct".into(),
        Action::Simple(Simple::Block) => "block".into(),
        Action::Link { link } if link == "auto" => "→ линк авто".into(),
        Action::Link { link } => format!("→ линк {link}"),
    }
}

pub fn group_name(g: Group) -> &'static str {
    match g {
        Group::All => "Для всех",
        Group::Split => "Режим split",
        Group::Full => "Режим full",
    }
}

pub const GROUPS: [Group; 3] = [Group::All, Group::Split, Group::Full];

/// Правила входа подряд по группам: (группа, номер с 1, строка).
pub fn rule_rows(st: &State, host: &str) -> Vec<(Group, usize, Row)> {
    let Some(r) = st.routing.get(host) else {
        return vec![];
    };
    GROUPS
        .iter()
        .flat_map(|g| {
            let rs = match g {
                Group::All => &r.groups.all,
                Group::Split => &r.groups.split,
                Group::Full => &r.groups.full,
            };
            rs.iter().enumerate().map(|(i, rule)| {
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

/// Строка матрицы: ребро и ячейки по протоколам с уровнем.
pub type MatrixRow = (String, Vec<(String, Level)>);

/// Матрица «ребро × протокол»: заголовок колонок и строки.
pub fn matrix_rows(s: &Status) -> (Vec<&'static str>, Vec<MatrixRow>) {
    let cols: Vec<&str> = Proto::ALL.iter().map(|p| p.name()).chain(["ssh"]).collect();
    let mut edges: Vec<(&str, &str)> = vec![];
    for e in &s.matrix {
        if !edges.contains(&(&e.src, &e.dst)) {
            edges.push((&e.src, &e.dst));
        }
    }
    let rows = edges
        .into_iter()
        .map(|(src, dst)| {
            let cells = cols
                .iter()
                .map(|c| {
                    let e = s
                        .matrix
                        .iter()
                        .find(|e| e.src == src && e.dst == dst && e.proto == *c);
                    (metrics::cell(e), edge_level(e))
                })
                .collect();
            (format!("{src} → {dst}"), cells)
        })
        .collect();
    (cols, rows)
}

// ---------- формы ----------

#[derive(Clone, Debug, PartialEq)]
pub enum Kind {
    /// `edit` — имя при правке, иначе новый пользователь.
    User { edit: Option<String> },
    /// `idx` — номер правила при правке, иначе новое.
    Rule {
        host: String,
        group: Group,
        idx: Option<usize>,
    },
    /// Линк `from->to`: правка, если он есть, иначе новый.
    Link,
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
        ("имя", edit.unwrap_or("").to_string()),
        (
            "режимы",
            u.map(|u| join(&u.modes, Mode::name))
                .unwrap_or("split".into()),
        ),
        (
            "протоколы",
            u.map(|u| join(&u.protocols, Proto::name))
                .unwrap_or("reality,ws,hy2".into()),
        ),
        (
            "tg (да/нет)",
            if u.is_some_and(|u| u.tg) {
                "да"
            } else {
                "нет"
            }
            .into(),
        ),
        (
            "срок (YYYY-MM-DD)",
            u.and_then(|u| u.expires.clone()).unwrap_or_default(),
        ),
        (
            "заметка",
            u.and_then(|u| u.note.clone()).unwrap_or_default(),
        ),
    ];
    if u.is_none() {
        f.push(("awg-пир (split/full)", String::new()));
        f.push(("вход", String::new()));
    }
    Form::new(
        match edit {
            Some(n) => format!("Пользователь {n}"),
            None => "Новый пользователь".into(),
        },
        Kind::User {
            edit: edit.map(String::from),
        },
        &f,
    )
}

pub fn rule_form(st: &State, host: &str, g: Group, idx: Option<usize>) -> Form {
    let r = idx.and_then(|i| {
        let rs = &st.routing.get(host)?.groups;
        match g {
            Group::All => &rs.all,
            Group::Split => &rs.split,
            Group::Full => &rs.full,
        }
        .get(i - 1)
    });
    let m = r.map(|r| r.match_.clone()).unwrap_or_default();
    let mut f = vec![];
    if idx.is_none() {
        f.push(("позиция", String::new()));
    }
    f.extend([
        ("наборы", m.sets.join(",")),
        ("geosite", m.geosite.join(",")),
        ("geoip", m.geoip.join(",")),
        ("домены", m.domains.join(",")),
        (
            "ip",
            m.ip.iter()
                .map(|i| i.to_string())
                .collect::<Vec<_>>()
                .join(","),
        ),
        ("inbound", m.inbound.join(",")),
        (
            "порт",
            m.port
                .iter()
                .map(|p| p.to_string())
                .collect::<Vec<_>>()
                .join(","),
        ),
        (
            "действие",
            r.map(|r| r.action.to_string()).unwrap_or("direct".into()),
        ),
        (
            "заметка",
            r.and_then(|r| r.note.clone()).unwrap_or_default(),
        ),
    ]);
    Form::new(
        format!(
            "{} · {} · {}",
            host,
            group_name(g),
            idx.map(|i| format!("правило #{i}"))
                .unwrap_or("новое правило".into())
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
            format!("Линк {from} → {to}")
        } else {
            "Новый линк".into()
        },
        Kind::Link,
        &[
            ("от", from.into()),
            ("к", to.into()),
            (
                "протоколы",
                l.map(|l| join(&l.protocols, Proto::name))
                    .unwrap_or("awg,reality".into()),
            ),
            (
                "политика (auto/fixed/rr)",
                l.map(|l| {
                    serde_yaml::to_string(&l.policy)
                        .unwrap_or_default()
                        .trim()
                        .into()
                })
                .unwrap_or("auto".into()),
            ),
            (
                "активный",
                l.and_then(|l| l.active)
                    .map(|p| p.name().into())
                    .unwrap_or_default(),
            ),
        ],
    )
}

fn policy(s: &str) -> Result<Policy> {
    Ok(match s {
        "auto" => Policy::Auto,
        "fixed" => Policy::Fixed,
        "rr" => Policy::Rr,
        _ => bail!("политика: auto, fixed или rr"),
    })
}

fn yes(s: &str) -> Result<bool> {
    match s {
        "да" | "yes" | "y" | "1" => Ok(true),
        "нет" | "no" | "n" | "0" | "" => Ok(false),
        _ => bail!("tg: да или нет"),
    }
}

/// Разбор формы и вызов операции; возвращает сообщение коммита. Состояние меняется только в памяти.
pub fn submit(f: &Form, st: &mut State) -> Result<String> {
    match &f.kind {
        Kind::User { edit: None } => {
            let name = f.get("имя");
            if name.is_empty() {
                bail!("имя пустое");
            }
            ops::user_add(
                st,
                name,
                list(f.get("режимы"))?,
                list(f.get("протоколы"))?,
                opt(f.get("awg-пир (split/full)"))
                    .map(|m| m.parse())
                    .transpose()?,
                yes(f.get("tg (да/нет)"))?,
                opt(f.get("срок (YYYY-MM-DD)")),
                opt(f.get("заметка")),
                opt(f.get("вход")),
            )
        }
        Kind::User { edit: Some(name) } => ops::user_edit(
            st,
            name,
            ops::UserPatch {
                modes: Some(list(f.get("режимы"))?),
                protocols: Some(list(f.get("протоколы"))?),
                expires: Some(opt(f.get("срок (YYYY-MM-DD)"))),
                note: opt(f.get("заметка")),
                tg: Some(yes(f.get("tg (да/нет)"))?),
            },
        ),
        Kind::Rule { host, group, idx } => {
            let m = Match {
                sets: csv(f.get("наборы")),
                geosite: csv(f.get("geosite")),
                geoip: csv(f.get("geoip")),
                domains: csv(f.get("домены")),
                ip: csv(f.get("ip"))
                    .iter()
                    .map(|s| s.parse().with_context(|| format!("ip «{s}»")))
                    .collect::<Result<_>>()?,
                inbound: csv(f.get("inbound")),
                port: csv(f.get("порт"))
                    .iter()
                    .map(|s| s.parse().with_context(|| format!("порт «{s}»")))
                    .collect::<Result<_>>()?,
            };
            let action: Action = f.get("действие").parse()?;
            let note = opt(f.get("заметка"));
            match idx {
                None => {
                    let pos = opt(f.get("позиция"))
                        .map(|p| p.parse().context("позиция — число"))
                        .transpose()?;
                    ops::rule_add(st, host, *group, pos, m, action, note)
                }
                Some(i) => ops::rule_edit(st, host, *group, *i, Some(m), Some(action), note),
            }
        }
        Kind::Link => {
            let (from, to) = (f.get("от"), f.get("к"));
            let protos: Vec<Proto> = list(f.get("протоколы"))?;
            let pol = policy(f.get("политика (auto/fixed/rr)"))?;
            let active = opt(f.get("активный")).map(|a| a.parse()).transpose()?;
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
    }
}

// ---------- трафик, линки, выход, DNS ----------

/// Трафик outbound'ов линка за сутки: только теги `<выход>-<протокол>` (группы `link-*`/`exit-auto` считают их повторно).
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

/// (напрямую, через выход) за сутки по outbound'ам хоста.
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

/// Время ответа ребра из последней матрицы.
pub fn link_ms(s: &Status, from: &str, to: &str, proto: Proto) -> Option<String> {
    s.matrix
        .iter()
        .find(|e| e.src == from && e.dst == to && e.proto == proto.name())
        .map(|e| metrics::cell(Some(e)))
}

/// Политика выхода входа: «авто (urltest), предпочтение fi» / «фиксированный fi».
pub fn exit_text(r: &Routing) -> String {
    let prefer = r.exit.prefer.as_deref();
    match (r.exit.policy, prefer) {
        (ExitPolicy::Auto, Some(p)) => format!("авто (urltest), предпочтение {p}"),
        (ExitPolicy::Auto, None) => "авто (urltest)".into(),
        (ExitPolicy::Fixed, Some(p)) => format!("фиксированный {p}"),
        (ExitPolicy::Fixed, None) => "фиксированный, выход не выбран".into(),
    }
}

pub fn dns_text(r: &Routing) -> String {
    format!(
        "домены из direct-наборов → {} напрямую · остальное → {} через линк",
        r.dns
            .direct
            .iter()
            .map(|i| i.to_string())
            .collect::<Vec<_>>()
            .join(", "),
        r.dns.remote
    )
}

/// Число включённых пользователей входа с протоколом (для awg — пиров).
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

/// Проверка формы на лету: операция и валидация на копии состояния →
/// (сообщение коммита, строки группы правил после изменения).
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

// ---------- карточки ----------

/// Ссылки пользователя: (подпись, url) — подписки по режимам и tg-прокси.
pub fn user_links(st: &State, name: &str) -> Result<Vec<(String, String)>> {
    use crate::render::subs;
    let u = st
        .users
        .get(name)
        .with_context(|| format!("пользователь «{name}» не описан"))?;
    let entry = st.entry_of(u).context("у пользователя нет входа")?;
    let mut v = vec![];
    for m in &u.modes {
        v.push((
            format!("подписка {}", m.name()),
            subs::sub_url(st, &entry, name, *m)?,
        ));
    }
    if u.tg
        && let Some(t) = subs::tg_link(st, &entry)
    {
        v.push(("tg-прокси".into(), t));
    }
    Ok(v)
}

fn mb(b: u64) -> String {
    format!("{} МБ", b >> 20)
}

fn or_dash<T: ToString>(v: Option<T>) -> String {
    v.map(|x| x.to_string()).unwrap_or("—".into())
}

/// Блоки карточки хоста: (заголовок, строки). Без снятий метрик — прочерки.
pub fn host_info(
    st: &State,
    s: &Status,
    name: &str,
    head: Option<&str>,
    now: i64,
) -> Vec<(&'static str, Vec<String>)> {
    let Some(h) = st.hosts.get(name) else {
        return vec![];
    };
    let o = &s.hosts[name];
    let l = o.last.as_ref();
    let m = l.map(|l| &l.m);
    let state = vec![
        format!(
            "роль {} · движок {}",
            role_text(h),
            match h.engine {
                Engine::SingBox => "sing-box",
                Engine::Xray => "xray",
            }
        ),
        format!(
            "ssh {}{}",
            h.ssh,
            if h.retiring {
                " · выводится"
            } else {
                ""
            }
        ),
        match l {
            None => "метрики ещё не собирались".into(),
            Some(l) if !l.reachable => {
                format!("✗ {}", l.error.as_deref().unwrap_or("нет ответа"))
            }
            Some(l) => format!(
                "снято {} · ssh {}",
                ago(l.ts, now),
                or_dash(l.ssh_ms.map(|x| format!("{x} мс")))
            ),
        },
        format!(
            "коммит {}{}",
            or_dash(l.and_then(|l| l.marker.as_deref())),
            match (l.and_then(|l| l.marker.as_deref()), head) {
                (Some(m), Some(hd)) if m == hd => " = HEAD",
                (Some(_), Some(_)) => " · отстаёт от HEAD",
                _ => "",
            }
        ),
        format!(
            "дрейф {}",
            match l.and_then(|l| l.drift) {
                Some(true) => "есть",
                Some(false) => "нет",
                None => "—",
            }
        ),
        match l.map(|l| (l.m.probe_code, l.m.probe_ms)) {
            Some((Some(c), ms)) => format!("проба {c} за {} мс", ms.unwrap_or(0)),
            _ => "проба —".into(),
        },
    ];
    let machine = vec![
        format!("load {}", or_dash(m.and_then(|m| m.load1))),
        format!(
            "память {} из {}",
            or_dash(m.and_then(|m| m.mem_used).map(mb)),
            or_dash(m.and_then(|m| m.mem_total).map(mb))
        ),
        format!(
            "диск {} из {}",
            or_dash(
                m.and_then(|m| m.disk_used)
                    .map(|b| format!("{} ГБ", b >> 30))
            ),
            or_dash(
                m.and_then(|m| m.disk_total)
                    .map(|b| format!("{} ГБ", b >> 30))
            )
        ),
        format!("соединения {}", or_dash(m.and_then(|m| m.conns))),
        format!(
            "uptime {}",
            or_dash(
                m.and_then(|m| m.uptime)
                    .map(|u| format!("{} дн", u as i64 / DAY))
            )
        ),
        format!(
            "обновления {}{}",
            or_dash(m.and_then(|m| m.updates)),
            if m.and_then(|m| m.reboot) == Some(true) {
                " · нужна перезагрузка"
            } else {
                ""
            }
        ),
    ];
    let cert = vec![
        format!("домен {}", h.domain.as_deref().unwrap_or("— (по IP)")),
        format!(
            "сертификат {}",
            or_dash(o.cert_days.map(|d| format!("истекает через {d} дн")))
        ),
    ];
    let versions = st
        .versions
        .iter()
        .map(|(n, p)| format!("{n} {}", p.version))
        .collect::<Vec<_>>()
        .join(" · ");
    let mut inbounds: Vec<String> = st
        .inbounds
        .get(name)
        .map(|m| {
            m.iter()
                .map(|(p, i)| {
                    let users = match h.role {
                        Role::Entry => format!(" · {} польз.", inbound_users(st, name, *p)),
                        Role::Exit => String::new(),
                    };
                    format!(
                        "{} {} {}{users}",
                        p.name(),
                        i.port,
                        if i.enabled { "вкл" } else { "выкл" }
                    )
                })
                .collect()
        })
        .unwrap_or_default();
    if let Some(r) = st.routing.get(name).filter(|_| h.role == Role::Entry) {
        inbounds.push(format!("выход: {}", exit_text(r)));
    }
    vec![
        ("Состояние", state),
        ("Машина", machine),
        ("Домен и сертификат", cert),
        (
            "Версии (versions.yaml)",
            vec![if versions.is_empty() {
                "—".into()
            } else {
                versions
            }],
        ),
        ("Inbound'ы и выход", inbounds),
    ]
}

/// Исходящие линки хоста: протоколы с отметкой активного, политика, трафик outbound'ов за сутки.
pub fn host_links(st: &State, s: &Status, name: &str) -> Vec<String> {
    st.links_from(name)
        .map(|l| {
            let protos: Vec<String> = l
                .protocols
                .iter()
                .map(|p| {
                    if l.active == Some(*p) {
                        format!("{} активный", p.name())
                    } else {
                        p.name().into()
                    }
                })
                .collect();
            let traffic = link_traffic(s, name, l);
            format!(
                "→ {} · {} · {} · {}/сутки",
                l.to,
                protos.join(", "),
                serde_yaml::to_string(&l.policy).unwrap_or_default().trim(),
                gb(traffic)
            )
        })
        .collect()
}
