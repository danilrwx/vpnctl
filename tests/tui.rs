use std::collections::BTreeMap;
use std::path::PathBuf;
use vpnctl::metrics::{self, Db, HostMetrics, HostSample, Status};
use vpnctl::plan::{HostPlan, Status as FS};
use vpnctl::state::*;
use vpnctl::tui::model::*;

/// 2027-01-15 00:00 UTC.
const NOW: i64 = 1_799_971_200;

fn fixture() -> State {
    State::load(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/basic")).unwrap()
}

/// State and an empty database in a temp dir.
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
        error: (!reachable).then(|| "timeout".to_string()),
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

fn edge(
    src: &str,
    dst: &str,
    proto: &str,
    ok: bool,
    ms: Option<u64>,
    note: Option<&str>,
) -> metrics::Edge {
    metrics::Edge {
        src: src.into(),
        dst: dst.into(),
        proto: proto.into(),
        ok,
        ms,
        note: note.map(String::from),
    }
}

fn no_behind(_: &str) -> Option<usize> {
    None
}

// ---------- navigation, filter, formatting ----------

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
    assert_eq!(date_epoch("yesterday"), None);
    assert_eq!(short_date(NOW), "15.01");
    assert_eq!(short_date(date_epoch("2000-03-01").unwrap()), "01.03");
    assert_eq!(date_time(NOW + 3661), "15.01 01:01");
    assert_eq!(ago(NOW - 10, NOW), "just now");
    assert_eq!(ago(NOW - 240, NOW), "4 min ago");
    assert_eq!(ago(NOW - 2 * DAY, NOW), "2 d ago");
    assert_eq!(gb(0), "0");
    assert_eq!(gb(4_100_000_000), "4.1 GB");
}

// ---------- levels ----------

#[test]
fn host_levels_and_issues() {
    let ok = host_out(sample(true, ok_metrics()));
    assert_eq!(host_level(&ok, Some("abc1234")), Level::Ok);
    assert_eq!(host_level(&ok, Some("fffffff")), Level::Warn, "behind HEAD");
    assert_eq!(host_issues(&ok, Some("fffffff"))[0].1, "behind HEAD");

    let cert = host_out(sample(
        true,
        HostMetrics {
            cert_expires: Some(NOW + (CERT_WARN_DAYS - 1) * DAY),
            ..ok_metrics()
        },
    ));
    assert_eq!(host_level(&cert, None), Level::Warn);
    assert_eq!(host_issues(&cert, None)[0].1, "certificate 13 d");
    let cert = host_out(sample(
        true,
        HostMetrics {
            cert_expires: Some(NOW + (CERT_WARN_DAYS + 1) * DAY),
            ..ok_metrics()
        },
    ));
    assert_eq!(host_level(&cert, None), Level::Ok);

    let down = host_out(sample(false, HostMetrics::default()));
    assert_eq!(host_level(&down, None), Level::Bad);
    assert_eq!(
        host_issues(&down, None),
        vec![(Level::Bad, "✗ timeout".to_string())]
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
fn user_levels_and_expiry() {
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
    assert_eq!(date_epoch("2027-01-15"), Some(NOW));
    assert_eq!(user_level(&on, Some("2027-01-20"), NOW), Level::Warn);
    assert_eq!(user_level(&on, Some("2027-01-25"), NOW), Level::Ok);
    assert_eq!(expiry_text(None, NOW), "no expiry");
    assert_eq!(expiry_text(Some("2027-01-20"), NOW), "5 d");
    assert_eq!(expiry_text(Some("2027-03-01"), NOW), "until 2027-03-01");
    assert_eq!(expiry_text(Some("2026-01-01"), NOW), "expired");
}

#[test]
fn edge_levels_and_text() {
    let e = |ok, ms, note| edge("a", "b", "awg", ok, ms, note);
    assert_eq!(edge_level(None), Level::Off);
    assert_eq!(edge_text(None), "— not set");
    assert_eq!(edge_level(Some(&e(true, Some(1), None))), Level::Ok);
    assert_eq!(
        edge_level(Some(&e(true, Some(PROBE_SLOW_MS + 1), None))),
        Level::Warn
    );
    assert_eq!(edge_level(Some(&e(false, None, Some("udp")))), Level::Off);
    assert_eq!(edge_text(Some(&e(false, None, Some("udp")))), "— udp");
    assert_eq!(
        edge_level(Some(&e(false, None, Some("timeout")))),
        Level::Bad
    );
    assert_eq!(edge_text(Some(&e(false, None, None))), "✗ timeout");
    assert_eq!(edge_text(Some(&e(true, Some(181), None))), "● 181 ms");
}

// ---------- hosts ----------

#[test]
fn roles_services_exit() {
    let st = fixture();
    assert_eq!(role_text(&st, "ru-msk-1"), "entry · new", "no host key yet");
    let mut st2 = st.clone();
    for h in st2.hosts.values_mut() {
        h.host_key = Some("ssh-ed25519 AAAA".into());
    }
    assert_eq!(role_text(&st2, "ru-msk-1"), "entry · jump");
    assert_eq!(role_text(&st2, "fi-hel-1"), "exit · active");
    assert_eq!(
        services(&st, "ru-msk-1"),
        vec!["sing-box", "caddy", "awg", "mtg"]
    );
    assert_eq!(services(&st, "fi-hel-1"), vec!["sing-box", "awg"]);
    assert_eq!(preferred_exit(&st, "ru-msk-1").as_deref(), Some("fi-hel-1"));
    assert!(is_jump(&st, "ru-msk-1"));
}

#[test]
fn host_rows_without_samples_are_dashes() {
    let (st, s, _t) = empty();
    let rows = host_rows(&st, &s, Some("abc"), &no_behind);
    assert_eq!(rows.len(), 2);
    let fi = &rows[0];
    assert_eq!(fi.cells[0], "fi-hel-1");
    assert_eq!(fi.cells[1], "exit · new");
    assert_eq!(fi.cells[2], "— (by IP)");
    assert_eq!(
        &fi.cells[3..],
        ["—", "no bootstrap", "sing-box awg", "—", "—", "—"]
    );
    assert_eq!(fi.level, Level::Off);
    assert_eq!(rows[1].cells[2], "gw.example.ru");
}

#[test]
fn host_rows_unreachable_is_red() {
    let (st, mut s, _t) = empty();
    s.hosts.get_mut("ru-msk-1").unwrap().last = Some(sample(false, HostMetrics::default()));
    let rows = host_rows(&st, &s, Some("abc1234"), &no_behind);
    assert_eq!(rows[1].cells[3], "✗ timeout");
    assert_eq!(rows[1].level, Level::Bad);

    s.hosts.get_mut("ru-msk-1").unwrap().last = Some(sample(true, ok_metrics()));
    let rows = host_rows(&st, &s, Some("abc1234"), &no_behind);
    assert_eq!(rows[1].cells[3], "● 48 ms");
    assert_eq!(rows[1].cells[4], "abc1234 = HEAD");
    assert_eq!(rows[1].cells[6], "● fi-hel-1 410 ms");
    assert_eq!(rows[1].level, Level::Ok);
    let rows = host_rows(&st, &s, Some("ffffff"), &|_| Some(2));
    assert_eq!(rows[1].cells[4], "abc1234 −2");
    let rows = host_rows(&st, &s, Some("ffffff"), &no_behind);
    assert_eq!(rows[1].cells[4], "abc1234 ≠ HEAD");
}

#[test]
fn user_rows_disabled_is_gray_and_sort() {
    let (st, s, _t) = empty();
    let mut rows = user_rows(&st, &s, NOW);
    assert_eq!(rows.len(), 2);
    assert_eq!(rows[0].cells[0], "brat");
    assert_eq!(rows[0].cells[1], "split full");
    assert_eq!(rows[0].cells[2], "active");
    assert_eq!(rows[0].cells[3], "no expiry");
    assert_eq!(rows[0].cells[4], "0");
    assert_eq!(rows[0].cells[7], "—");
    assert_eq!(rows[0].level, Level::Ok);
    assert_eq!(rows[1].cells[0], "olga");
    assert_eq!(rows[1].cells[2], "disabled");
    assert_eq!(rows[1].level, Level::Off);
    rows[1].cells[4] = "2.0 GB".into();
    sort_users(&mut rows, true);
    assert_eq!(rows[0].cells[0], "olga");
    sort_users(&mut rows, false);
    assert_eq!(rows[0].cells[0], "brat");
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
            (Group::Full, 1)
        ]
    );
    assert_eq!(rows[0].2.cells[1], "inbound probe");
    assert_eq!(rows[0].2.cells[2], "→ link auto");
    assert_eq!(
        rows[1].2.cells[1],
        "domains from set ads · geosite:category-ads-all"
    );
    assert_eq!(rows[1].2.cells[2], "block");
    assert_eq!(
        rows[2].2.cells[1],
        "domains from set ru-direct · geosite:category-ru, geosite:yandex, geosite:vk + 1 own"
    );
    assert_eq!(rows[3].2.cells[1], "addresses geoip:ru, geoip:private");
    assert_eq!(rows[4].2.cells[2], "→ link fi-hel-1");
    assert_eq!(rows[5].2.cells[1], "everything else");
    assert_eq!(rows[5].2.cells[3], "●");
    assert!(rule_rows(&st, "nope").is_empty());
    assert_eq!(group_name(Group::All), "For all");
    assert_eq!(
        group_note(&st, "ru-msk-1", Group::All),
        "checked before modes"
    );
    assert!(group_note(&st, "ru-msk-1", Group::Split).starts_with("users split-*"));
}

#[test]
fn matrix_rows_notes_and_history() {
    let (st, mut s, _t) = empty();
    assert!(matrix_rows(&s).is_empty());
    s.matrix = vec![
        edge("laptop", "ru-msk-1", "reality", true, Some(181), None),
        edge("laptop", "ru-msk-1", "hy2", false, None, Some("udp")),
        edge("ru-msk-1", "fi-hel-1", "awg", true, Some(184), None),
        edge("ru-msk-1", "fi-hel-1", "reality", false, None, None),
        edge("laptop", "ru-spb-2", "ssh", false, None, Some("no answer")),
    ];
    let rows = matrix_rows(&s);
    assert_eq!(rows.len(), 3);
    assert_eq!(rows[0].0, ("laptop".to_string(), "ru-msk-1".to_string()));
    assert_eq!(rows[0].1[0], ("● 181 ms".to_string(), Level::Ok));
    assert_eq!(rows[0].1[2], ("— udp".to_string(), Level::Off));
    assert_eq!(rows[0].1[1], ("— not set".to_string(), Level::Off));
    assert_eq!(
        edge_note(&st, &s, "ru-msk-1", "fi-hel-1"),
        "active link · awg fine · backup reality down, hy2 not probed"
    );
    assert_eq!(
        edge_note(&st, &s, "laptop", "ru-spb-2"),
        "host not answering over ssh"
    );
    assert_eq!(edge_note(&st, &s, "laptop", "ru-msk-1"), "");

    let hist = vec![
        (NOW - 3 * DAY + 100, true, Some(100)),
        (NOW - 3 * DAY + 200, false, None),
        (NOW - 100, true, Some(900)),
    ];
    let b = day_buckets(&hist, NOW, 3);
    assert_eq!(b, vec![Some(Level::Bad), None, Some(Level::Warn)]);
}

#[test]
fn traffic_split_and_link_traffic_exact_tags() {
    let (st, mut s, _t) = empty();
    assert_eq!(traffic_split(&st, &s, "ru-msk-1"), (0, 0));
    let tr = |up, down| metrics::Traffic { up, down };
    let mut o = BTreeMap::new();
    o.insert("direct".to_string(), tr(1_000_000_000, 10_000_000_000));
    o.insert(
        "fi-hel-1-awg".to_string(),
        tr(2_000_000_000, 20_000_000_000),
    );
    o.insert("fi-hel-1-reality".to_string(), tr(0, 5_000_000_000));
    // groups over the same outbounds must not be counted twice
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
    let parts = link_parts(&s, l);
    assert_eq!(parts[0], ("awg — active".to_string(), Level::Off));
    assert_eq!(
        parts.last().unwrap(),
        &("27.0 GB/day".to_string(), Level::Off)
    );
    let rows = exit_rows(&st, &s, "ru-msk-1");
    assert_eq!(rows[0].0, "direct");
    assert_eq!(rows[0].2, "11.0 GB/day");
    assert_eq!(
        rows[1],
        (
            "fi-hel-1".to_string(),
            "awg0".to_string(),
            "active · 27.0 GB/day".to_string(),
            Level::Ok
        )
    );
    assert!(exit_rows(&st, &s, "fi-hel-1").is_empty());
}

#[test]
fn link_ms_from_matrix() {
    let (_st, mut s, _t) = empty();
    assert_eq!(link_ms(&s, "ru-msk-1", "fi-hel-1", Proto::Awg), None);
    s.matrix
        .push(edge("ru-msk-1", "fi-hel-1", "awg", true, Some(184), None));
    assert_eq!(
        link_ms(&s, "ru-msk-1", "fi-hel-1", Proto::Awg).as_deref(),
        Some("● 184 ms")
    );
    assert_eq!(link_ms(&s, "ru-msk-1", "fi-hel-1", Proto::Reality), None);
}

#[test]
fn exit_dns_set_texts() {
    let st = fixture();
    let r = &st.routing["ru-msk-1"];
    assert_eq!(exit_text(r), "fixed fi-hel-1");
    assert_eq!(exit_text(&st.routing["fi-hel-1"]), "auto (urltest)");
    assert!(dns_text(r)[0].starts_with("domains from direct sets → 77.88.8.8, 1.1.1.1 via direct"));
    assert_eq!(set_text(&st, "ads"), "ads (geosite × 1)");
    assert_eq!(
        set_text(&st, "ru-direct"),
        "ru-direct (geosite × 3 + 1 own)"
    );
    assert_eq!(set_text(&st, "nope"), "nope");
    assert_eq!(set_members(&st, "ads"), "geosite:category-ads-all");
    assert_eq!(set_examples(&st, "ru-direct", 4), "gosuslugi.ru");
}

#[test]
fn inbound_rows_and_cards() {
    let (st, s, _t) = empty();
    assert_eq!(
        inbound_users(&st, "ru-msk-1", Proto::Reality),
        1,
        "brat on, olga off"
    );
    assert_eq!(inbound_users(&st, "ru-msk-1", Proto::Awg), 1);
    assert_eq!(inbound_users(&st, "ru-msk-1", Proto::Mtg), 1);
    assert_eq!(inbound_users(&st, "fi-hel-1", Proto::Reality), 0);
    let rows = inbound_rows(&st, "ru-msk-1");
    let reality = rows.iter().find(|r| r.0 == "reality").unwrap();
    assert_eq!(reality.1, "tcp 443");
    assert_eq!(reality.2, "on · 1 users");
    assert_eq!(reality.3, Level::Ok);
    assert_eq!(rows.last().unwrap().0, "probe (socks)");
    assert_eq!(rows.last().unwrap().3, Level::Off);

    let state = host_state(&st, &s, "ru-msk-1", None);
    assert_eq!(
        state[0],
        (
            "commit".to_string(),
            "not applied yet".to_string(),
            Level::Off
        )
    );
    assert_eq!(state[2].0, "sing-box");
    let machine = host_machine(&s, "fi-hel-1", NOW);
    assert_eq!(
        machine[0],
        ("load".to_string(), "—".to_string(), Level::Off)
    );
    assert_eq!(machine[1].1, "— of —");
    let domain = host_domain(&st, &s, "ru-msk-1", None);
    assert_eq!(domain[0].1, "gw.example.ru");
    assert_eq!(domain[1].1, "— not checked");
    let ip: std::net::IpAddr = "5.6.7.8".parse().unwrap();
    assert_eq!(
        host_domain(&st, &s, "ru-msk-1", Some(&[ip]))[1].1,
        "✓ matches"
    );
    assert_eq!(
        host_domain(&st, &s, "ru-msk-1", Some(&[]))[1].1,
        "✗ does not resolve"
    );
    assert_eq!(host_domain(&st, &s, "fi-hel-1", None)[0].1, "— (by IP)");

    let links = user_links(&st, "brat").unwrap();
    assert_eq!(links.len(), 3, "split, full, tg");
    assert!(links[0].1.starts_with("https://gw.example.ru/s/"));
    assert_eq!(links[2].0, "Telegram proxy");
    assert_eq!(user_links(&st, "olga").unwrap().len(), 1);
    assert!(user_links(&st, "nope").is_err());

    let nodes = graph_nodes(&st, &s, None);
    assert_eq!(
        nodes.iter().map(|n| n.0.as_str()).collect::<Vec<_>>(),
        vec!["laptop", "ru-msk-1", "fi-hel-1"]
    );
}

// ---------- plan ----------

#[test]
fn plan_rows_and_checks() {
    let (st, s, _t) = empty();
    let plans = vec![
        HostPlan {
            name: "fi-hel-1".into(),
            unreachable: None,
            bootstrapped: true,
            marker: Some("9c0d2b1".into()),
            files: vec![
                ("/etc/sing-box/config.json".into(), FS::Changed),
                ("/etc/x".into(), FS::Unchanged),
                ("/etc/caddy/Caddyfile".into(), FS::Drift),
            ],
            notes: vec![],
        },
        HostPlan {
            name: "ru-msk-1".into(),
            unreachable: Some("timeout".into()),
            ..Default::default()
        },
        HostPlan {
            name: "ru-spb-2".into(),
            bootstrapped: false,
            ..Default::default()
        },
    ];
    let order = vec!["fi-hel-1".to_string(), "ru-msk-1".to_string()];
    let roles = BTreeMap::from([("fi-hel-1".to_string(), "exit · active".to_string())]);
    let rows = plan_rows(&plans, &order, Some("c4d7e02"), &roles);
    assert_eq!(
        rows[0],
        PlanRow::Host(
            "fi-hel-1 · exit · active · 9c0d2b1 → c4d7e02 · order 1".into(),
            Level::Ok
        )
    );
    assert!(
        matches!(&rows[1], PlanRow::File { path, status: FS::Changed, level: Level::Ok, .. } if path == "/etc/sing-box/config.json")
    );
    assert!(matches!(
        &rows[2],
        PlanRow::File {
            status: FS::Drift,
            level: Level::Warn,
            ..
        }
    ));
    assert!(
        matches!(&rows[3], PlanRow::Note(t, Level::Off) if t.starts_with("will be overwritten"))
    );
    assert_eq!(
        rows[4],
        PlanRow::Host("ru-msk-1 ·  · ✗ timeout".into(), Level::Bad)
    );
    assert_eq!(
        rows[5],
        PlanRow::Host("ru-spb-2 · skipped: bootstrap not done".into(), Level::Off)
    );

    let checks = plan_checks(&st, &s, &plans, "main", true, true);
    assert_eq!(checks[0], (Level::Ok, "clean tree, branch main".into()));
    assert_eq!(checks[2].0, Level::Warn, "no matrix data for the link");
    assert!(
        checks[2]
            .1
            .starts_with("fi-hel-1 answers in the matrix: awg — reality — hy2 —")
    );
    assert!(
        checks
            .iter()
            .any(|(l, t)| *l == Level::Warn && t == "drift on fi-hel-1 · decision needed")
    );
    let checks = plan_checks(&st, &s, &plans, "dev", false, true);
    assert_eq!(checks[0], (Level::Bad, "branch dev, tree dirty".into()));
}

// ---------- trace ----------

#[test]
fn trace_by_rules() {
    let st = fixture();
    let (steps, result) = trace(&st, "ru-msk-1", Mode::Split, "www.youtube.com:443");
    assert_eq!(result, "→ link fi-hel-1 (auto)", "the catch-all split rule");
    assert_eq!(steps.len(), 6);
    assert!(
        steps[0].text.contains("not a service inbound"),
        "{}",
        steps[0].text
    );
    assert!(
        steps[4]
            .text
            .contains("geosite:youtube — geodata not checked offline")
    );
    assert!(steps[5].matched && steps[5].text == "everything else");
    let (steps, result) = trace(&st, "ru-msk-1", Mode::Split, "gosuslugi.ru:443");
    assert_eq!(result, "direct");
    assert_eq!(
        steps.last().unwrap().text,
        "matched gosuslugi.ru in set ru-direct"
    );
    let (_, result) = trace(&st, "ru-msk-1", Mode::Full, "mail.gosuslugi.ru");
    assert_eq!(
        result, "→ link fi-hel-1 (auto)",
        "full mode has no direct rule"
    );
    let (_, result) = trace(&st, "nope", Mode::Full, "x");
    assert_eq!(result, "no routing for the entry");
    let m = Match {
        port: vec![22],
        ip: vec!["10.0.0.0/8".parse().unwrap()],
        ..Default::default()
    };
    assert_eq!(
        rule_matches(&st, &m, "10.1.2.3", "10.1.2.3".parse().ok(), None),
        (true, "matched 10.0.0.0/8".into())
    );
    assert_eq!(
        rule_matches(&st, &m, "h", None, Some(22)),
        (true, "matched port 22".into())
    );
    assert_eq!(
        rule_matches(&st, &m, "h", None, Some(80)),
        (false, "no match".into())
    );
}

// ---------- forms ----------

#[test]
fn form_navigation_and_input() {
    let st = fixture();
    let mut f = user_form(&st, None);
    assert_eq!(f.fields.len(), 8);
    assert_eq!(f.get("modes"), "split");
    f.input('x');
    assert_eq!(f.get("name"), "x");
    f.backspace();
    f.prev();
    assert_eq!(f.cur, 7);
    f.next();
    assert_eq!(f.cur, 0);
    let e = user_form(&st, Some("olga"));
    assert_eq!(e.fields.len(), 6, "no awg and entry when editing");
    assert_eq!(e.get("note"), "сестра");
    assert_eq!(e.get("protocols"), "reality,ws,hy2");
    assert_eq!(e.title, "User olga");
}

fn set(f: &mut Form, label: &str, v: &str) {
    let i = f.fields.iter().position(|fld| fld.label == label).unwrap();
    f.fields[i].value = v.into();
}

#[test]
fn submit_user_add_and_edit() {
    let mut st = fixture();
    let mut f = user_form(&st, None);
    set(&mut f, "name", "sergey");
    set(&mut f, "modes", "split, full");
    set(&mut f, "protocols", "reality,ws");
    set(&mut f, "tg proxy", "yes");
    assert_eq!(
        submit(&f, &mut st).unwrap(),
        "users: + sergey (split, full)"
    );
    let u = &st.users["sergey"];
    assert_eq!(u.modes, vec![Mode::Split, Mode::Full]);
    assert_eq!(u.protocols, vec![Proto::Reality, Proto::Ws]);
    assert!(u.tg);
    assert!(st.secrets.users.contains_key("sergey"));

    set(&mut f, "protocols", "reality,ftp");
    assert!(submit(&f, &mut st).unwrap_err().to_string().contains("ftp"));
    set(&mut f, "name", "");
    assert_eq!(
        submit(&f, &mut st).unwrap_err().to_string(),
        "name is empty"
    );

    let mut e = user_form(&st, Some("olga"));
    set(&mut e, "expiry (YYYY-MM-DD)", "2027-01-01");
    set(&mut e, "tg proxy", "no");
    assert!(submit(&e, &mut st).unwrap().starts_with("users: olga:"));
    assert_eq!(st.users["olga"].expires.as_deref(), Some("2027-01-01"));
}

#[test]
fn submit_rule_add_edit_and_errors() {
    let mut st = fixture();
    let mut f = rule_form(&st, "ru-msk-1", Group::Split, None);
    assert_eq!(f.fields[0].label, "position");
    set(&mut f, "sets", "ru-direct");
    set(&mut f, "port", "443, 8443");
    set(&mut f, "action", "link:fi-hel-1");
    set(&mut f, "note", "banks");
    let msg = submit(&f, &mut st).unwrap();
    assert!(msg.contains("banks → link:fi-hel-1"), "{msg}");
    let r = &st.routing["ru-msk-1"].groups.split;
    assert_eq!(r.len(), 5);
    assert_eq!(r[3].match_.port, vec![443, 8443]);
    assert_eq!(
        r[3].action,
        Action::Link {
            link: "fi-hel-1".into()
        }
    );

    let mut e = rule_form(&st, "ru-msk-1", Group::Split, Some(4));
    assert_eq!(e.get("port"), "443,8443");
    assert_eq!(e.get("action"), "link:fi-hel-1");
    set(&mut e, "action", "block");
    submit(&e, &mut st).unwrap();
    assert_eq!(
        st.routing["ru-msk-1"].groups.split[3].action,
        Action::Simple(Simple::Block)
    );

    set(&mut e, "ip", "10.0.0.0/8, nope");
    assert!(
        submit(&e, &mut st)
            .unwrap_err()
            .to_string()
            .contains("ip «nope»")
    );
    set(&mut e, "ip", "");
    set(&mut e, "action", "teleport");
    assert!(submit(&e, &mut st).is_err());
    set(&mut f, "position", "99");
    assert!(submit(&f, &mut st).unwrap_err().to_string().contains("99"));
}

#[test]
fn submit_link_and_exit() {
    let mut st = fixture();
    let mut f = link_form(&st, "ru-msk-1", "fi-hel-1");
    assert_eq!(f.get("protocols"), "awg,reality,hy2");
    assert_eq!(f.get("policy (auto/fixed/rr)"), "fixed");
    assert_eq!(f.get("active"), "awg");
    set(&mut f, "protocols", "awg,reality");
    set(&mut f, "active", "reality");
    set(&mut f, "policy (auto/fixed/rr)", "auto");
    let msg = submit(&f, &mut st).unwrap();
    assert!(
        msg.contains("-hy2") && msg.contains("active reality"),
        "{msg}"
    );
    let l = st.link("ru-msk-1", "fi-hel-1").unwrap();
    assert_eq!(l.protocols, vec![Proto::Awg, Proto::Reality]);
    assert_eq!(l.policy, Policy::Auto);
    set(&mut f, "policy (auto/fixed/rr)", "maybe");
    assert!(submit(&f, &mut st).is_err());

    let mut n = link_form(&st, "", "");
    assert_eq!(n.title, "New link");
    set(&mut n, "from", "fi-hel-1");
    set(&mut n, "to", "ru-msk-1");
    set(&mut n, "protocols", "reality");
    set(&mut n, "policy (auto/fixed/rr)", "fixed");
    assert_eq!(
        submit(&n, &mut st).unwrap(),
        "links: + fi-hel-1->ru-msk-1 (reality)"
    );
    assert!(st.link("fi-hel-1", "ru-msk-1").is_some());

    let mut x = exit_form(&st, "ru-msk-1");
    assert_eq!(x.get("policy (auto/fixed)"), "fixed");
    assert_eq!(x.get("prefer"), "fi-hel-1");
    set(&mut x, "policy (auto/fixed)", "auto");
    assert!(submit(&x, &mut st).unwrap().contains("policy auto"));
    assert_eq!(st.routing["ru-msk-1"].exit.policy, ExitPolicy::Auto);
    set(&mut x, "policy (auto/fixed)", "maybe");
    assert!(submit(&x, &mut st).is_err());
}

#[test]
fn preview_on_the_fly() {
    let st = fixture();
    let mut f = rule_form(&st, "ru-msk-1", Group::Split, None);
    set(&mut f, "sets", "nope");
    let e = preview(&f, &st).unwrap_err().to_string();
    assert!(e.contains("nope"), "{e}");
    set(&mut f, "sets", "ru-direct");
    set(&mut f, "note", "banks");
    let (msg, rows) = preview(&f, &st).unwrap();
    assert_eq!(msg, "routing(ru-msk-1): split #4: banks → direct");
    assert_eq!(rows.len(), 5, "4 split rules + the new one");
    assert!(rows[3].cells[1].starts_with("domains from set ru-direct"));
    assert_eq!(
        st.routing["ru-msk-1"].groups.split.len(),
        4,
        "state untouched"
    );

    let mut u = user_form(&st, None);
    set(&mut u, "name", "sergey");
    let (msg, rows) = preview(&u, &st).unwrap();
    assert_eq!(msg, "users: + sergey (split)");
    assert!(rows.is_empty());
    assert!(!st.users.contains_key("sergey"));
}

/// Сообщение для пересылки: подписка, резервная ссылка на IP, проверка и Telegram.
#[test]
fn share_message_has_subscription_and_fallbacks() {
    let st = fixture();
    let m = share_message(&st, "brat").unwrap();
    assert!(m.contains("https://gw.example.ru/s/"), "подписка: {m}");
    assert!(m.contains("Karing"), "клиенты: {m}");
    assert!(m.contains("vless://"), "резервная ссылка: {m}");
    assert!(
        m.contains("@5.6.7.8:443"),
        "резервная ссылка идёт на IP, а не на домен: {m}"
    );
    assert!(m.contains("sni=gw.example.ru"), "SNI остаётся доменом: {m}");
    assert_eq!(
        m.matches("vless://").count(),
        2,
        "обе прямые ссылки (reality и ws) для Podkop: {m}"
    );
    assert!(
        m.contains("#vpn-reality") && m.contains("#vpn-ws"),
        "метки профилей: {m}"
    );
    assert!(m.contains("ifconfig.me"), "проверка: {m}");
    assert!(
        m.contains("tg://proxy?server=5.6.7.8"),
        "Telegram по IP: {m}"
    );
}
