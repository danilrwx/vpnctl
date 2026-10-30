use std::path::PathBuf;
use vpnctl::metrics::{self, Db, HostMetrics, HostSample, Status};
use vpnctl::state::*;
use vpnctl::tui::model::*;

/// 2027-01-15 00:00 UTC.
const NOW: i64 = 1_799_971_200;
const DAY: i64 = 86_400;

fn fixture() -> State {
    State::load(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/basic")).unwrap()
}

/// Состояние и пустая база во временном каталоге.
fn empty() -> (State, Status, tempfile::TempDir) {
    let st = fixture();
    let tmp = tempfile::tempdir().unwrap();
    let s = metrics::status(&st, &Db::open(tmp.path()).unwrap(), NOW).unwrap();
    (st, s, tmp)
}

fn sample(reachable: bool, m: HostMetrics) -> HostSample {
    HostSample {
        ts: NOW - 300,
        reachable,
        error: (!reachable).then(|| "таймаут".to_string()),
        marker: Some("abc1234".into()),
        drift: Some(false),
        ssh_ms: Some(48),
        m,
    }
}

fn ok_metrics() -> HostMetrics {
    HostMetrics {
        probe_code: Some(204),
        probe_ms: Some(410),
        updates: Some(0),
        reboot: Some(false),
        ..Default::default()
    }
}

fn host_out(s: HostSample) -> metrics::HostOut {
    metrics::HostOut {
        role: Role::Entry,
        domain: None,
        cert_days: s.m.cert_expires.map(|e| (e - NOW) / DAY),
        last: Some(s),
        probe: None,
    }
}

// ---------- навигация и фильтр ----------

#[test]
fn step_clamps() {
    assert_eq!(step(0, 0, 1), 0);
    assert_eq!(step(0, 3, -1), 0);
    assert_eq!(step(2, 3, 1), 2);
    assert_eq!(step(1, 3, 1), 2);
    assert_eq!(step(5, 3, 0), 2);
}

#[test]
fn filter_substring_ignores_case() {
    let rows = vec![
        Row {
            cells: vec!["Olga".into(), "split".into()],
            level: Level::Ok,
        },
        Row {
            cells: vec!["brat".into(), "full".into()],
            level: Level::Ok,
        },
    ];
    assert_eq!(filter(&rows, ""), vec![0, 1]);
    assert_eq!(filter(&rows, "ol"), vec![0]);
    assert_eq!(filter(&rows, "FULL"), vec![1]);
    assert!(filter(&rows, "zzz").is_empty());
}

#[test]
fn dates_and_ago() {
    assert_eq!(date_epoch("1970-01-02"), Some(DAY));
    assert_eq!(date_epoch("2000-03-01"), Some(951_868_800));
    assert_eq!(date_epoch("2026-13-01"), None);
    assert_eq!(date_epoch("вчера"), None);
    assert_eq!(ago(NOW - 10, NOW), "только что");
    assert_eq!(ago(NOW - 240, NOW), "4 мин назад");
    assert_eq!(ago(NOW - 2 * DAY, NOW), "2 дн назад");
}

// ---------- уровни ----------

#[test]
fn host_levels() {
    let ok = host_out(sample(true, ok_metrics()));
    assert_eq!(host_level(&ok, Some("abc1234")), Level::Ok);
    assert_eq!(
        host_level(&ok, Some("fffffff")),
        Level::Warn,
        "отстаёт от HEAD"
    );
    assert_eq!(host_level(&ok, None), Level::Ok);

    let cert = host_out(sample(
        true,
        HostMetrics {
            cert_expires: Some(NOW + (CERT_WARN_DAYS - 1) * DAY),
            ..ok_metrics()
        },
    ));
    assert_eq!(host_level(&cert, None), Level::Warn);
    let cert = host_out(sample(
        true,
        HostMetrics {
            cert_expires: Some(NOW + (CERT_WARN_DAYS + 1) * DAY),
            ..ok_metrics()
        },
    ));
    assert_eq!(host_level(&cert, None), Level::Ok);

    assert_eq!(
        host_level(&host_out(sample(false, HostMetrics::default())), None),
        Level::Bad
    );
    let probe = host_out(sample(
        true,
        HostMetrics {
            probe_code: Some(502),
            ..ok_metrics()
        },
    ));
    assert_eq!(host_level(&probe, None), Level::Bad);
    let mut none = host_out(sample(true, ok_metrics()));
    none.last = None;
    assert_eq!(host_level(&none, None), Level::Off);
}

#[test]
fn user_levels() {
    let on = metrics::UserOut {
        enabled: true,
        day: Default::default(),
        week: Default::default(),
        total: Default::default(),
    };
    let off = metrics::UserOut {
        enabled: false,
        ..on
    };
    assert_eq!(user_level(&on, None, NOW), Level::Ok);
    assert_eq!(user_level(&off, None, NOW), Level::Off);
    assert_eq!(user_level(&on, Some("2000-01-01"), NOW), Level::Bad);
    assert_eq!(user_level(&on, Some("2100-01-01"), NOW), Level::Ok);
    // 2027-01-20 — через 5 дней, 2027-01-25 — через 10
    assert_eq!(date_epoch("2027-01-15"), Some(NOW));
    assert_eq!(user_level(&on, Some("2027-01-20"), NOW), Level::Warn);
    assert_eq!(user_level(&on, Some("2027-01-25"), NOW), Level::Ok);
}

#[test]
fn edge_levels() {
    let e = |ok: bool, note: Option<&str>| metrics::Edge {
        src: "a".into(),
        dst: "b".into(),
        proto: "awg".into(),
        ok,
        ms: Some(1),
        note: note.map(String::from),
    };
    assert_eq!(edge_level(None), Level::Off);
    assert_eq!(edge_level(Some(&e(true, None))), Level::Ok);
    assert_eq!(edge_level(Some(&e(false, Some("udp")))), Level::Off);
    assert_eq!(edge_level(Some(&e(false, Some("таймаут")))), Level::Bad);
}

// ---------- строки таблиц ----------

#[test]
fn host_rows_without_samples_are_dashes() {
    let (st, s, _t) = empty();
    let rows = host_rows(&st, &s, Some("abc"));
    assert_eq!(rows.len(), 2);
    let fi = &rows[0];
    assert_eq!(fi.cells[0], "fi-hel-1");
    assert_eq!(fi.cells[1], "выход via ru-msk-1");
    assert_eq!(fi.cells[2], "— (по IP)");
    assert_eq!(&fi.cells[3..], ["—", "—", "", "—", "—", "—"]);
    assert_eq!(fi.level, Level::Off);
    assert_eq!(rows[1].cells[2], "gw.example.ru");
}

#[test]
fn host_rows_unreachable_is_red() {
    let (st, mut s, _t) = empty();
    s.hosts.get_mut("ru-msk-1").unwrap().last = Some(sample(false, HostMetrics::default()));
    let rows = host_rows(&st, &s, Some("abc1234"));
    assert_eq!(rows[1].cells[3], "✗ таймаут");
    assert_eq!(rows[1].level, Level::Bad);

    s.hosts.get_mut("ru-msk-1").unwrap().last = Some(sample(true, ok_metrics()));
    let rows = host_rows(&st, &s, Some("abc1234"));
    assert_eq!(rows[1].cells[3], "● 48 мс");
    assert_eq!(rows[1].cells[4], "abc1234 = HEAD");
    assert_eq!(rows[1].cells[6], "● 204 за 410 мс");
    assert_eq!(rows[1].level, Level::Ok);
}

#[test]
fn user_rows_disabled_is_gray() {
    let (st, s, _t) = empty();
    let rows = user_rows(&st, &s, NOW);
    assert_eq!(rows.len(), 2);
    assert_eq!(rows[0].cells[0], "brat");
    assert_eq!(rows[0].cells[1], "split full");
    assert_eq!(rows[0].cells[2], "активен");
    assert_eq!(rows[0].cells[3], "без срока");
    assert_eq!(rows[0].cells[4], "0");
    assert_eq!(rows[0].level, Level::Ok);
    assert_eq!(rows[1].cells[0], "olga");
    assert_eq!(rows[1].cells[2], "отключён");
    assert_eq!(rows[1].level, Level::Off);
}

#[test]
fn rule_rows_in_group_order() {
    let st = fixture();
    let rows = rule_rows(&st, "ru-msk-1");
    let groups: Vec<(Group, usize)> = rows.iter().map(|(g, i, _)| (*g, *i)).collect();
    assert_eq!(
        groups,
        vec![
            (Group::All, 1),
            (Group::All, 2),
            (Group::Split, 1),
            (Group::Split, 2),
            (Group::Split, 3),
            (Group::Split, 4),
            (Group::Full, 1),
        ]
    );
    assert_eq!(rows[0].2.cells[1], "inbound probe");
    assert_eq!(rows[0].2.cells[2], "→ линк авто");
    assert_eq!(rows[1].2.cells[1], "набор ads (geosite × 1)");
    assert_eq!(rows[1].2.cells[2], "block");
    assert_eq!(rows[3].2.cells[1], "geoip:ru · geoip:private");
    assert_eq!(rows[4].2.cells[2], "→ линк fi-hel-1");
    assert_eq!(rows[5].2.cells[1], "всё остальное");
    assert_eq!(rows[5].2.cells[3], "●");
    assert!(rule_rows(&st, "nope").is_empty());
}

#[test]
fn matrix_rows_from_edges() {
    let (_st, mut s, _t) = empty();
    assert!(matrix_rows(&s).1.is_empty());
    s.matrix = vec![
        metrics::Edge {
            src: "ноутбук".into(),
            dst: "ru-msk-1".into(),
            proto: "reality".into(),
            ok: true,
            ms: Some(181),
            note: None,
        },
        metrics::Edge {
            src: "ноутбук".into(),
            dst: "ru-msk-1".into(),
            proto: "hy2".into(),
            ok: false,
            ms: None,
            note: Some("udp".into()),
        },
    ];
    let (cols, rows) = matrix_rows(&s);
    assert_eq!(cols, vec!["reality", "ws", "hy2", "awg", "mtg", "ssh"]);
    assert_eq!(rows.len(), 1);
    assert_eq!(rows[0].0, "ноутбук → ru-msk-1");
    assert_eq!(rows[0].1[0], ("● 181 мс".to_string(), Level::Ok));
    assert_eq!(rows[0].1[2], ("— udp".to_string(), Level::Off));
    assert_eq!(rows[0].1[1], ("— не описан".to_string(), Level::Off));
}

#[test]
fn user_links_and_host_info() {
    let (st, s, _t) = empty();
    let links = user_links(&st, "brat").unwrap();
    assert_eq!(links.len(), 3, "split, full, tg");
    assert!(links[0].1.starts_with("https://gw.example.ru/s/"));
    assert_eq!(links[2].0, "tg-прокси");
    assert_eq!(user_links(&st, "olga").unwrap().len(), 1);
    assert!(user_links(&st, "nope").is_err());

    let info = host_info(&st, &s, "fi-hel-1", None, NOW);
    assert_eq!(info[0].0, "Состояние");
    assert_eq!(info[0].1[2], "метрики ещё не собирались");
    assert_eq!(info[1].1[0], "load —");
    assert_eq!(info[1].1[1], "память — из —");
    assert_eq!(info[2].1[1], "сертификат —");
    let links = host_links(&st, &s, "ru-msk-1");
    assert_eq!(
        links,
        vec!["→ fi-hel-1 · awg активный, reality, hy2 · fixed · 0/сутки"]
    );
}

// ---------- формы ----------

#[test]
fn form_navigation_and_input() {
    let st = fixture();
    let mut f = user_form(&st, None);
    assert_eq!(f.fields.len(), 8);
    assert_eq!(f.get("режимы"), "split");
    f.input('x');
    assert_eq!(f.get("имя"), "x");
    f.backspace();
    f.prev();
    assert_eq!(f.cur, 7);
    f.next();
    assert_eq!(f.cur, 0);
    let e = user_form(&st, Some("olga"));
    assert_eq!(e.fields.len(), 6, "при правке нет awg и входа");
    assert_eq!(e.get("заметка"), "сестра");
    assert_eq!(e.get("протоколы"), "reality,ws,hy2");
}

fn set(f: &mut Form, label: &str, v: &str) {
    let i = f.fields.iter().position(|(l, _)| *l == label).unwrap();
    f.fields[i].1 = v.into();
}

#[test]
fn submit_user_add_and_edit() {
    let mut st = fixture();
    let mut f = user_form(&st, None);
    set(&mut f, "имя", "sergey");
    set(&mut f, "режимы", "split, full");
    set(&mut f, "протоколы", "reality,ws");
    set(&mut f, "tg (да/нет)", "да");
    assert_eq!(
        submit(&f, &mut st).unwrap(),
        "users: + sergey (split, full)"
    );
    let u = &st.users["sergey"];
    assert_eq!(u.modes, vec![Mode::Split, Mode::Full]);
    assert_eq!(u.protocols, vec![Proto::Reality, Proto::Ws]);
    assert!(u.tg);
    assert!(st.secrets.users.contains_key("sergey"));

    set(&mut f, "протоколы", "reality,ftp");
    assert!(submit(&f, &mut st).unwrap_err().to_string().contains("ftp"));
    set(&mut f, "имя", "");
    assert!(submit(&f, &mut st).is_err());

    let mut e = user_form(&st, Some("olga"));
    set(&mut e, "срок (YYYY-MM-DD)", "2027-01-01");
    set(&mut e, "tg (да/нет)", "нет");
    assert!(submit(&e, &mut st).unwrap().starts_with("users: olga:"));
    assert_eq!(st.users["olga"].expires.as_deref(), Some("2027-01-01"));
}

#[test]
fn submit_rule_add_edit_and_errors() {
    let mut st = fixture();
    let mut f = rule_form(&st, "ru-msk-1", Group::Split, None);
    assert_eq!(f.fields[0].0, "позиция");
    set(&mut f, "наборы", "ru-direct");
    set(&mut f, "порт", "443, 8443");
    set(&mut f, "действие", "link:fi-hel-1");
    set(&mut f, "заметка", "банки");
    let msg = submit(&f, &mut st).unwrap();
    assert!(msg.contains("банки → link:fi-hel-1"), "{msg}");
    let r = &st.routing["ru-msk-1"].groups.split;
    assert_eq!(r.len(), 5);
    assert_eq!(r[3].match_.port, vec![443, 8443]);
    assert_eq!(
        r[3].action,
        Action::Link {
            link: "fi-hel-1".into()
        }
    );
    assert_eq!(r[4].note.as_deref(), Some("всё остальное через выход"));

    let mut e = rule_form(&st, "ru-msk-1", Group::Split, Some(4));
    assert_eq!(e.get("порт"), "443,8443");
    assert_eq!(e.get("действие"), "link:fi-hel-1");
    set(&mut e, "действие", "block");
    submit(&e, &mut st).unwrap();
    assert_eq!(
        st.routing["ru-msk-1"].groups.split[3].action,
        Action::Simple(Simple::Block)
    );

    set(&mut e, "ip", "10.0.0.0/8, нет");
    assert!(
        submit(&e, &mut st)
            .unwrap_err()
            .to_string()
            .contains("ip «нет»")
    );
    set(&mut e, "ip", "");
    set(&mut e, "действие", "teleport");
    assert!(submit(&e, &mut st).is_err());
    set(&mut f, "позиция", "99");
    assert!(
        submit(&f, &mut st)
            .unwrap_err()
            .to_string()
            .contains("позиция 99")
    );
}

#[test]
fn submit_link_set_and_add() {
    let mut st = fixture();
    let mut f = link_form(&st, "ru-msk-1", "fi-hel-1");
    assert_eq!(f.get("протоколы"), "awg,reality,hy2");
    assert_eq!(f.get("политика (auto/fixed/rr)"), "fixed");
    assert_eq!(f.get("активный"), "awg");
    set(&mut f, "протоколы", "awg,reality");
    set(&mut f, "активный", "reality");
    set(&mut f, "политика (auto/fixed/rr)", "auto");
    let msg = submit(&f, &mut st).unwrap();
    assert!(
        msg.contains("-hy2") && msg.contains("active reality"),
        "{msg}"
    );
    let l = st.link("ru-msk-1", "fi-hel-1").unwrap();
    assert_eq!(l.protocols, vec![Proto::Awg, Proto::Reality]);
    assert_eq!(l.policy, Policy::Auto);

    set(&mut f, "политика (auto/fixed/rr)", "maybe");
    assert!(submit(&f, &mut st).is_err());

    let mut n = link_form(&st, "", "");
    assert_eq!(n.title, "Новый линк");
    set(&mut n, "от", "fi-hel-1");
    set(&mut n, "к", "ru-msk-1");
    set(&mut n, "протоколы", "reality");
    set(&mut n, "политика (auto/fixed/rr)", "fixed");
    assert_eq!(
        submit(&n, &mut st).unwrap(),
        "links: + fi-hel-1->ru-msk-1 (reality)"
    );
    assert!(st.link("fi-hel-1", "ru-msk-1").is_some());
}

// ---------- вторая итерация: трафик, линки, выход, DNS, превью ----------

fn tr(up: u64, down: u64) -> metrics::Traffic {
    metrics::Traffic { up, down }
}

#[test]
fn traffic_split_and_link_traffic_exact_tags() {
    let (st, mut s, _t) = empty();
    assert_eq!(traffic_split(&st, &s, "ru-msk-1"), (0, 0));
    let mut o = std::collections::BTreeMap::new();
    o.insert("direct".to_string(), tr(1_000_000_000, 10_000_000_000));
    o.insert(
        "fi-hel-1-awg".to_string(),
        tr(2_000_000_000, 20_000_000_000),
    );
    o.insert("fi-hel-1-reality".to_string(), tr(0, 5_000_000_000));
    // группы над теми же outbound'ами — не должны считаться повторно
    o.insert(
        "link-fi-hel-1".to_string(),
        tr(2_000_000_000, 25_000_000_000),
    );
    o.insert("exit-auto".to_string(), tr(2_000_000_000, 25_000_000_000));
    s.outbounds.insert("ru-msk-1".into(), o);
    assert_eq!(
        traffic_split(&st, &s, "ru-msk-1"),
        (11_000_000_000, 27_000_000_000)
    );
    let l = st.link("ru-msk-1", "fi-hel-1").unwrap();
    assert_eq!(link_traffic(&s, "ru-msk-1", l), 27_000_000_000);
    assert_eq!(
        host_links(&st, &s, "ru-msk-1"),
        vec!["→ fi-hel-1 · awg активный, reality, hy2 · fixed · 27.0 ГБ/сутки"]
    );
}

#[test]
fn link_ms_from_matrix() {
    let (_st, mut s, _t) = empty();
    assert_eq!(link_ms(&s, "ru-msk-1", "fi-hel-1", Proto::Awg), None);
    s.matrix.push(metrics::Edge {
        src: "ru-msk-1".into(),
        dst: "fi-hel-1".into(),
        proto: "awg".into(),
        ok: true,
        ms: Some(184),
        note: None,
    });
    assert_eq!(
        link_ms(&s, "ru-msk-1", "fi-hel-1", Proto::Awg).as_deref(),
        Some("● 184 мс")
    );
    assert_eq!(link_ms(&s, "ru-msk-1", "fi-hel-1", Proto::Reality), None);
}

#[test]
fn exit_dns_set_texts() {
    let st = fixture();
    let r = &st.routing["ru-msk-1"];
    assert_eq!(exit_text(r), "фиксированный fi-hel-1");
    assert_eq!(exit_text(&st.routing["fi-hel-1"]), "авто (urltest)");
    assert_eq!(
        dns_text(r),
        "домены из direct-наборов → 77.88.8.8, 1.1.1.1 напрямую · остальное → https://1.1.1.1/dns-query через линк"
    );
    assert_eq!(set_text(&st, "ads"), "ads (geosite × 1)");
    assert!(set_text(&st, "ru-direct").starts_with("ru-direct (geosite × 3 + "));
    assert_eq!(set_text(&st, "nope"), "nope");
    let rows = rule_rows(&st, "ru-msk-1");
    assert_eq!(rows[1].2.cells[1], "набор ads (geosite × 1)");
}

#[test]
fn inbound_users_counts() {
    let st = fixture();
    // brat включён (reality, ws, hy2, awg-пир, tg); olga выключена
    assert_eq!(inbound_users(&st, "ru-msk-1", Proto::Reality), 1);
    assert_eq!(inbound_users(&st, "ru-msk-1", Proto::Awg), 1);
    assert_eq!(inbound_users(&st, "ru-msk-1", Proto::Mtg), 1);
    assert_eq!(inbound_users(&st, "fi-hel-1", Proto::Reality), 0);
    let (_, s, _t) = empty();
    let info = host_info(&st, &s, "ru-msk-1", None, NOW);
    assert_eq!(info[3].0, "Версии (versions.yaml)");
    let inb = &info[4].1;
    assert!(
        inb.iter()
            .any(|l| l.starts_with("reality ") && l.ends_with("1 польз.")),
        "{inb:?}"
    );
    assert_eq!(inb.last().unwrap(), "выход: фиксированный fi-hel-1");
}

#[test]
fn preview_on_the_fly() {
    let st = fixture();
    let mut f = rule_form(&st, "ru-msk-1", Group::Split, None);
    set(&mut f, "наборы", "nope");
    let e = preview(&f, &st).unwrap_err().to_string();
    assert!(e.contains("nope"), "{e}");
    set(&mut f, "наборы", "ru-direct");
    set(&mut f, "заметка", "банки");
    let (msg, rows) = preview(&f, &st).unwrap();
    assert_eq!(msg, "routing(ru-msk-1): split #4: банки → direct");
    assert_eq!(rows.len(), 5, "4 правила split + новое");
    assert!(rows[3].cells[1].starts_with("набор ru-direct"));
    assert!(
        st.routing["ru-msk-1"].groups.split.len() == 4,
        "состояние не тронуто"
    );

    let mut u = user_form(&st, None);
    set(&mut u, "имя", "sergey");
    let (msg, rows) = preview(&u, &st).unwrap();
    assert_eq!(msg, "users: + sergey (split)");
    assert!(rows.is_empty());
    assert!(!st.users.contains_key("sergey"));
}
