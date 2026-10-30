use std::path::PathBuf;
use vpnctl::render;
use vpnctl::state::*;

fn fixture() -> State {
    State::load(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/basic")).unwrap()
}

#[test]
fn snapshots_singbox() {
    let r = render::check(&fixture()).unwrap();
    for (host, files) in &r.hosts {
        for (path, body) in files {
            insta::assert_snapshot!(format!("{host}{}", path.replace('/', "_")), body);
        }
    }
}

#[test]
fn snapshots_xray() {
    let mut st = fixture();
    // xray на входе: без hy2 и без L3-клиентов
    st.hosts.get_mut("ru-msk-1").unwrap().engine = Engine::Xray;
    let inb = st.inbounds.get_mut("ru-msk-1").unwrap();
    inb.remove(&Proto::Hy2);
    inb.remove(&Proto::Awg);
    for u in st.users.values_mut() {
        u.awg = None;
        u.protocols
            .retain(|p| !matches!(p, Proto::Awg | Proto::Hy2));
    }
    st.links[0].protocols.retain(|p| *p != Proto::Hy2);
    assert!(
        vpnctl::validate::validate(&st).is_empty(),
        "{:?}",
        vpnctl::validate::validate(&st)
    );
    let files = render::render_host(&st, "ru-msk-1").unwrap();
    insta::assert_snapshot!("xray_config", files["/etc/xray/config.json"]);
    assert!(files["/etc/systemd/system/xray.service"].contains("/usr/local/bin/xray run"));
    assert!(!files.contains_key("/etc/amnezia/amneziawg/awg0.conf"));
}

#[test]
fn stub_secrets_render() {
    let st =
        State::load_stub(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/basic"))
            .unwrap();
    assert!(vpnctl::validate::validate(&st).is_empty());
    let r = render::check(&st).unwrap();
    assert!(r.hosts["ru-msk-1"]["/etc/sing-box/config.json"].contains("STUB-uuid-brat"));
}

#[test]
fn disabled_protocol_leaves_subscriptions() {
    let mut st = fixture();
    st.inbounds
        .get_mut("ru-msk-1")
        .unwrap()
        .get_mut(&Proto::Hy2)
        .unwrap()
        .enabled = false;
    let files = render::render_host(&st, "ru-msk-1").unwrap();
    for (p, body) in &files {
        if p.starts_with("/var/www/sub/") {
            let plain = if p.ends_with(".json") || p.ends_with(".yaml") {
                body.clone()
            } else {
                String::from_utf8(
                    base64::Engine::decode(&base64::engine::general_purpose::STANDARD, body.trim())
                        .unwrap(),
                )
                .unwrap()
            };
            assert!(!plain.contains("hysteria2"), "{p}");
        }
    }
    assert!(!files["/etc/sing-box/config.json"].contains("\"tag\": \"hy2\""));
}

#[test]
fn link_active_change_touches_only_source() {
    let st = fixture();
    let before = render::render_host(&st, "fi-hel-1").unwrap();
    let mut st2 = st.clone();
    st2.links[0].active = Some(Proto::Reality);
    let after = render::render_host(&st2, "fi-hel-1").unwrap();
    assert_eq!(before, after);
    assert_ne!(
        render::render_host(&st, "ru-msk-1").unwrap(),
        render::render_host(&st2, "ru-msk-1").unwrap()
    );
}

#[test]
fn exit_without_awg_link_has_no_awg0() {
    let mut st = fixture();
    st.links[0].protocols = vec![Proto::Reality];
    st.links[0].active = Some(Proto::Reality);
    let files = render::render_host(&st, "fi-hel-1").unwrap();
    assert!(!files.contains_key("/etc/amnezia/amneziawg/awg0.conf"));
    assert!(!files["/etc/nftables.conf"].contains("awg0"));
}

#[test]
fn reality_target_with_port() {
    let st = fixture();
    let mut i = st.inbounds["fi-hel-1"][&Proto::Reality].clone();
    i.sni = Some("cover.lab".into());
    i.handshake = Some("127.0.0.1:8443".into());
    assert_eq!(
        render::reality_target(&st, "fi-hel-1", &i),
        ("cover.lab".into(), "127.0.0.1".into(), 8443)
    );
    i.handshake = None;
    i.sni = Some("www.microsoft.com".into());
    assert_eq!(render::reality_target(&st, "fi-hel-1", &i).2, 443);
    i.sni = None;
    assert_eq!(
        render::reality_target(&st, "ru-msk-1", &i),
        (
            "gw.example.ru".into(),
            "127.0.0.1".into(),
            render::CADDY_PORT
        )
    );
}
