use std::path::PathBuf;
use vpnctl::metrics::*;
use vpnctl::state::*;

fn fixture() -> State {
    State::load(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/basic")).unwrap()
}

fn sh_ok(script: &str) {
    let tmp = tempfile::NamedTempFile::new().unwrap();
    std::fs::write(tmp.path(), script).unwrap();
    let out = std::process::Command::new("sh")
        .arg("-n")
        .arg(tmp.path())
        .output()
        .unwrap();
    assert!(
        out.status.success(),
        "sh -n: {}",
        String::from_utf8_lossy(&out.stderr)
    );
}

// ---------- protobuf ----------

fn varint(mut v: u64) -> Vec<u8> {
    let mut out = vec![];
    loop {
        let b = (v & 0x7f) as u8;
        v >>= 7;
        if v == 0 {
            out.push(b);
            return out;
        }
        out.push(b | 0x80);
    }
}

fn bytes_field(num: u8, b: &[u8]) -> Vec<u8> {
    let mut v = vec![num << 3 | 2];
    v.extend(varint(b.len() as u64));
    v.extend(b);
    v
}

/// Кадр gRPC с QueryStatsResponse из пар (имя, значение).
fn frame(stats: &[(&str, u64)]) -> Vec<u8> {
    let mut msg = vec![];
    for (n, v) in stats {
        let mut stat = bytes_field(1, n.as_bytes());
        stat.push(2 << 3);
        stat.extend(varint(*v));
        msg.extend(bytes_field(1, &stat));
    }
    let mut f = vec![0u8];
    f.extend((msg.len() as u32).to_be_bytes());
    f.extend(msg);
    f
}

#[test]
fn stats_decode_and_fold() {
    let body = frame(&[
        ("user>>>brat-split-reality>>>traffic>>>uplink", 1500),
        ("user>>>brat-split-reality>>>traffic>>>downlink", 300_000),
        ("outbound>>>fi-hel-1-awg>>>traffic>>>downlink", 1 << 40),
        ("inbound>>>reality>>>something>>>uplink", 7),
    ]);
    let stats = parse_stats(&body).unwrap();
    assert_eq!(stats.len(), 4);
    assert_eq!(
        stats["outbound>>>fi-hel-1-awg>>>traffic>>>downlink"],
        1 << 40
    );
    let c = fold_counters(&stats);
    assert_eq!(
        c[&("user".to_string(), "brat-split-reality".to_string())],
        (1500, 300_000)
    );
    assert_eq!(
        c[&("outbound".to_string(), "fi-hel-1-awg".to_string())],
        (0, 1 << 40)
    );
    assert_eq!(c.len(), 2, "не-traffic счётчики отброшены");
    // два кадра подряд, пустое тело, сжатый кадр, обрезанный кадр
    let mut two = frame(&[("a>>>b>>>traffic>>>uplink", 1)]);
    two.extend(frame(&[("a>>>b>>>traffic>>>downlink", 2)]));
    assert_eq!(fold_counters(&parse_stats(&two).unwrap()).len(), 1);
    assert_eq!(parse_stats(b""), None);
    assert_eq!(parse_stats(&[1, 0, 0, 0, 0]), None);
    let mut cut = frame(&[("x", 1)]);
    cut.truncate(cut.len() - 1);
    assert_eq!(parse_stats(&cut), None);
}

#[test]
fn deltas() {
    assert_eq!(delta(Some(1000), 1500), 500);
    assert_eq!(delta(Some(1000), 200), 200, "перезапуск движка");
    assert_eq!(delta(None, 777), 777, "первое снятие");
    assert_eq!(delta(Some(5), 5), 0);
}

// ---------- скрипт хоста ----------

#[test]
fn host_script_and_parse() {
    let s = host_script(Engine::SingBox, Role::Entry, "/etc/vpnctl/self.crt");
    sh_ok(&s);
    assert!(s.contains("--socks5-hostname 127.0.0.1:1080"));
    // у sing-box из официальной сборки нет v2ray_api: запрос уйдёт в никуда, парсер вернёт пусто
    assert!(s.contains("/v2ray.core.app.stats.command.StatsService/QueryStats"));
    assert!(s.contains("printf '\\0\\0\\0\\0\\0'"));
    assert!(s.contains("openssl x509 -enddate -noout -in '/etc/vpnctl/self.crt'"));
    let x = host_script(Engine::Xray, Role::Exit, "/c");
    assert!(x.contains("127.0.0.1:10085/") && !x.contains("socks5"));

    use base64::Engine as _;
    let b64 = base64::engine::general_purpose::STANDARD
        .encode(frame(&[("user>>>olga-split-ws>>>traffic>>>downlink", 42)]));
    let out = format!(
        "load=0.21\nmem=432013312 2147483648\ndisk=6549995520 26843545600\nconns=184\nuptime=777600\nupdates=3\nreboot=yes\ncert=1798000000\nprobe=204 0.412\nstats={b64}\n"
    );
    let m = parse_host(&out);
    assert_eq!(m.load1, Some(0.21));
    assert_eq!(
        (m.mem_used, m.mem_total),
        (Some(432013312), Some(2147483648))
    );
    assert_eq!(m.disk_total, Some(26843545600));
    assert_eq!(m.conns, Some(184));
    assert_eq!(m.uptime, Some(777600));
    assert_eq!(m.updates, Some(3));
    assert_eq!(m.reboot, Some(true));
    assert_eq!(m.cert_expires, Some(1798000000));
    assert_eq!((m.probe_code, m.probe_ms), (Some(204), Some(412)));
    let c = m.counters.unwrap();
    assert_eq!(c[&("user".into(), "olga-split-ws".into())], (0, 42));

    // нет сертификата, проба не ответила, stats пустые
    let m = parse_host("load=0.5\nprobe=000 0.000\nstats=\n");
    assert_eq!(m.cert_expires, None);
    assert_eq!((m.probe_code, m.probe_ms), (None, None));
    assert_eq!(m.counters, None);
    assert_eq!(parse_host("мусор"), HostMetrics::default());
}

// ---------- база ----------

#[test]
fn db_deltas_totals_retention() {
    let tmp = tempfile::tempdir().unwrap();
    let db = Db::open(tmp.path()).unwrap();
    let now = 1_800_000_000;
    let k = ("user".to_string(), "brat-split-reality".to_string());
    let c1: Counters = [(k.clone(), (1000, 5000))].into();
    let c2: Counters = [(k.clone(), (1500, 6000))].into();
    let c3: Counters = [(k.clone(), (200, 100))].into();
    db.traffic(now - 70 * 86400, "ru", &c1).unwrap();
    db.traffic(now - 3600, "ru", &c2).unwrap();
    db.traffic(now, "ru", &c3).unwrap();
    assert_eq!(
        db.sum("user", "brat-split-reality", now - 86400).unwrap(),
        Traffic {
            up: 500 + 200,
            down: 1000 + 100
        }
    );
    assert_eq!(
        db.total("user", "brat-split-reality").unwrap(),
        Traffic {
            up: 1000 + 500 + 200,
            down: 5000 + 1000 + 100
        }
    );
    db.prune(now).unwrap();
    assert_eq!(
        db.sum("user", "brat-split-reality", 0).unwrap(),
        Traffic {
            up: 700,
            down: 1100
        },
        "старое снятие удалено"
    );
    assert_eq!(
        db.total("user", "brat-split-reality").unwrap().up,
        1700,
        "итог не уменьшился"
    );

    let s = HostSample {
        ts: now,
        reachable: true,
        marker: Some("a1f3c9e".into()),
        drift: Some(false),
        ssh_ms: Some(48),
        m: HostMetrics {
            load1: Some(0.2),
            probe_code: Some(204),
            probe_ms: Some(410),
            ..Default::default()
        },
        ..Default::default()
    };
    db.sample("ru", "collect", &s).unwrap();
    let p = HostSample {
        ts: now + 60,
        reachable: true,
        m: HostMetrics {
            probe_code: Some(204),
            probe_ms: Some(390),
            ..Default::default()
        },
        ..Default::default()
    };
    db.sample("ru", "probe", &p).unwrap();
    assert_eq!(db.last_sample("ru", Some("collect")).unwrap().unwrap(), s);
    assert_eq!(
        db.last_sample("ru", None).unwrap().unwrap().m.probe_ms,
        Some(390)
    );
    assert_eq!(db.last_sample("fi", Some("collect")).unwrap(), None);
    assert_eq!(
        Db::open(tmp.path()).unwrap().last_edges().unwrap(),
        (None, vec![])
    );
}

// ---------- матрица ----------

#[test]
fn edges_script_parse_table() {
    let st = fixture();
    let s = edge_script(&st, "ru-msk-1");
    sh_ok(&s);
    assert!(s.contains("bash -c 'exec 3<>/dev/tcp/1.2.3.4/443'"));
    assert!(s.contains("awg show awg1 latest-handshakes"));
    assert!(s.contains("ping -c1 -W2 10.9.1.2"));
    assert_eq!(
        edge_script(&st, "fi-hel-1"),
        "",
        "у выхода нет исходящих линков"
    );

    let now = 1_800_000_000;
    let out = format!(
        "fi-hel-1 reality ok 201\nfi-hel-1 hy2 fail\nfi-hel-1 awg hs {} 12.4\nnl awg hs {} \nnl2 awg hs 0 \n",
        now - 30,
        now - 600
    );
    let e = parse_edges("ru-msk-1", &out, now);
    assert_eq!(e.len(), 5);
    assert!(e[0].ok && e[0].ms == Some(201) && e[0].proto == "reality");
    assert!(!e[1].ok && e[1].note.as_deref() == Some("таймаут"));
    assert!(e[2].ok && e[2].ms == Some(12));
    assert!(!e[3].ok && e[3].note.as_deref() == Some("рукопожатие 10 мин"));
    assert!(!e[4].ok && e[4].note.as_deref() == Some("нет рукопожатия"));

    let t = laptop_targets(&st, "ru-msk-1");
    assert!(t.contains(&(Proto::Reality, Some("5.6.7.8:443".into()))));
    assert!(t.contains(&(Proto::Ws, Some("gw.example.ru:443".into()))));
    assert!(t.contains(&(Proto::Mtg, Some("5.6.7.8:2053".into()))));
    assert!(t.contains(&(Proto::Hy2, None)));
    assert_eq!(
        tcp_ms("127.0.0.1:1", std::time::Duration::from_millis(200)),
        None
    );

    let mut all = vec![Edge {
        src: LAPTOP.into(),
        dst: "ru-msk-1".into(),
        proto: "hy2".into(),
        ok: false,
        ms: None,
        note: Some("udp".into()),
    }];
    all.extend(e);
    let tbl = table(&all);
    let lines: Vec<&str> = tbl.lines().collect();
    assert_eq!(lines.len(), 5, "заголовок + 4 ребра: {tbl}");
    assert!(lines[0].starts_with("ребро") && lines[0].contains("ssh"));
    assert!(lines[1].starts_with("ноутбук → ru-msk-1") && lines[1].contains("— udp"));
    assert!(
        lines[2].contains("● 201 мс")
            && lines[2].contains("✗ таймаут")
            && lines[2].contains("— не описан")
    );
}

// ---------- status ----------

#[test]
fn status_json() {
    let st = fixture();
    let tmp = tempfile::tempdir().unwrap();
    let db = Db::open(tmp.path()).unwrap();
    let now = 1_800_000_000;
    let empty = status(&st, &db, now).unwrap();
    let v: serde_json::Value =
        serde_json::from_str(&serde_json::to_string(&empty).unwrap()).unwrap();
    assert!(v["hosts"]["ru-msk-1"]["last"].is_null());
    assert_eq!(v["users"]["brat"]["day"]["down"], 0);
    assert!(v["matrix"].as_array().unwrap().is_empty());

    assert_eq!(
        accounts("brat", &st.users["brat"]),
        vec![
            "brat-split-reality",
            "brat-split-ws",
            "brat-split-hy2",
            "brat-full-reality",
            "brat-full-ws",
            "brat-full-hy2"
        ]
    );
    let c: Counters = [
        (
            ("user".to_string(), "brat-split-reality".to_string()),
            (10, 1_000_000_000),
        ),
        (
            ("user".to_string(), "brat-full-hy2".to_string()),
            (20, 2_000_000_000),
        ),
        (("user".to_string(), "olga-split-ws".to_string()), (1, 1)),
        (
            ("outbound".to_string(), "fi-hel-1-awg".to_string()),
            (5, 50),
        ),
    ]
    .into();
    db.traffic(now - 8 * 86400, "ru-msk-1", &c).unwrap();
    db.traffic(now - 60, "ru-msk-1", &c).unwrap();
    db.sample(
        "ru-msk-1",
        "collect",
        &HostSample {
            ts: now - 60,
            reachable: true,
            marker: Some("a1f3c9e".into()),
            drift: Some(true),
            m: HostMetrics {
                cert_expires: Some(now + 61 * 86400 + 100),
                probe_code: Some(204),
                probe_ms: Some(410),
                ..Default::default()
            },
            ..Default::default()
        },
    )
    .unwrap();
    db.edges(
        now,
        &[Edge {
            src: LAPTOP.into(),
            dst: "ru-msk-1".into(),
            proto: "ssh".into(),
            ok: true,
            ms: Some(48),
            note: None,
        }],
    )
    .unwrap();
    let s = status(&st, &db, now).unwrap();
    let v: serde_json::Value = serde_json::from_str(&serde_json::to_string(&s).unwrap()).unwrap();
    assert_eq!(
        v["users"]["brat"]["day"]["down"], 0,
        "второе снятие тех же показаний — дельта 0"
    );
    assert_eq!(v["users"]["brat"]["week"]["down"], 0);
    assert_eq!(
        v["users"]["brat"]["total"]["down"], 3_000_000_000u64,
        "две учётки суммируются"
    );
    assert_eq!(v["users"]["olga"]["enabled"], false);
    assert_eq!(v["hosts"]["ru-msk-1"]["last"]["marker"], "a1f3c9e");
    assert_eq!(v["hosts"]["ru-msk-1"]["last"]["drift"], true);
    assert_eq!(v["hosts"]["ru-msk-1"]["cert_days"], 61);
    assert_eq!(v["hosts"]["ru-msk-1"]["probe"]["ms"], 410);
    assert_eq!(v["hosts"]["fi-hel-1"]["role"], "exit");
    assert_eq!(v["outbounds"]["ru-msk-1"]["fi-hel-1-awg"]["down"], 0);
    assert_eq!(v["matrix_ts"], now);
    assert_eq!(v["matrix"][0]["proto"], "ssh");
    assert!(summary("ru-msk-1", &s.hosts["ru-msk-1"].last.clone().unwrap()).contains("дрейф"));
}

#[test]
fn init_ignores_db() {
    let tmp = tempfile::tempdir().unwrap();
    vpnctl::ops::init(tmp.path(), None).unwrap();
    assert!(
        std::fs::read_to_string(tmp.path().join(".gitignore"))
            .unwrap()
            .contains("/metrics.sqlite")
    );
}
