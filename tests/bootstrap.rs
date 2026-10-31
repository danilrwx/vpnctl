use std::path::PathBuf;
use vpnctl::bootstrap::{self, reboot_days, rule_set_tags, rule_set_url};
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

#[test]
fn entry_script() {
    let st = fixture();
    let s = bootstrap::script(&st, "ru-msk-1").unwrap();
    sh_ok(&s);
    assert!(s.contains("sing-box-1.12.4-linux-amd64.tar.gz"));
    assert!(s.contains("caddy_2.10.0_linux_amd64.tar.gz"));
    assert!(s.contains("mtg-2.1.7-linux-amd64.tar.gz"));
    assert!(!s.contains("Xray-linux-64.zip"));
    assert!(s.contains("/etc/sing-box/rules/geosite-youtube.srs"));
    assert!(s.contains("/etc/sing-box/rules/geoip-ru.srs"));
    assert!(s.contains("OnCalendar=*-*-01,03,05"));
    assert!(s.contains("useradd --system --home /var/lib/caddy"));
    assert!(!s.contains("openssl req"));
    assert!(s.contains(
        "systemctl enable -q awg-quick@awg0 awg-quick@awg1 caddy mtg.service sing-box.service"
    ));
    assert!(
        s.contains("sha256 is not pinned"),
        "without sha there is a warning"
    );
    assert!(
        s.contains("\" > /etc/vpnctl/bootstrap"),
        "marker is the repo HEAD or - outside git"
    );
    assert!(s.contains("table inet filter"), "nftables from the render");
}

#[test]
fn exit_script() {
    let st = fixture();
    let s = bootstrap::script(&st, "fi-hel-1").unwrap();
    sh_ok(&s);
    assert!(s.contains("openssl req -x509"));
    assert!(!s.contains("caddy_"));
    assert!(!s.contains("mtg-"));
    assert!(s.contains("OnCalendar=*-*-02,04,06"));
    assert!(
        s.contains("rule-set'ы: 0 файлов") || !s.contains("rule-set'ы"),
        "на выходе нет правил с наборами"
    );
}

#[test]
fn xray_engine_and_sha() {
    let mut st = fixture();
    st.hosts.get_mut("ru-msk-1").unwrap().engine = Engine::Xray;
    st.versions.get_mut("xray").unwrap().sha256 = Some("abcd".into());
    let s = bootstrap::script(&st, "ru-msk-1").unwrap();
    sh_ok(&s);
    assert!(s.contains("Xray-linux-64.zip"));
    assert!(!s.contains("sing-box-1.12.4"));
    assert!(s.contains("fetch \"xray\" \"https://github.com/XTLS/Xray-core/releases/download/v25.9.11/Xray-linux-64.zip\" \"abcd\""));
    assert!(
        !s.contains("rules/geosite-"),
        "xray не использует rule-set'ы sing-box"
    );
}

#[test]
fn helpers() {
    assert_eq!(
        reboot_days(Role::Entry),
        "01,03,05,07,09,11,13,15,17,19,21,23,25,27,29,31"
    );
    assert_eq!(
        reboot_days(Role::Exit),
        "02,04,06,08,10,12,14,16,18,20,22,24,26,28,30"
    );
    assert_eq!(
        rule_set_url("geoip-ru", "latest"),
        "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-ru.srs"
    );
    assert_eq!(
        rule_set_url("geosite-vk", "20261027000000"),
        "https://github.com/SagerNet/sing-geosite/releases/download/20261027000000/geosite-vk.srs"
    );
    let cfg = r#"{"route":{"rule_set":[{"tag":"geosite-vk","format":"binary"},{"tag":"set-x","format":"source"},{"tag":"geoip-ru","format":"binary"}]}}"#;
    assert_eq!(rule_set_tags(cfg), vec!["geoip-ru", "geosite-vk"]);
    assert!(rule_set_tags("не json").is_empty());
}

#[test]
fn missing_version_is_error() {
    let mut st = fixture();
    st.versions.remove("mtg");
    let e = bootstrap::script(&st, "ru-msk-1").unwrap_err().to_string();
    assert!(e.contains("«mtg»"), "{e}");
}

#[test]
fn awg_only_when_needed() {
    let mut st = fixture();
    let s = bootstrap::script(&st, "ru-msk-1").unwrap();
    assert!(s.contains("amneziawg-linux-kernel-module"));
    for inb in st.inbounds.values_mut() {
        inb.remove(&Proto::Awg);
    }
    st.links[0].protocols.retain(|p| *p != Proto::Awg);
    st.links[0].active = Some(Proto::Reality);
    for u in st.users.values_mut() {
        u.awg = None;
        u.protocols.retain(|p| *p != Proto::Awg);
    }
    let s = bootstrap::script(&st, "ru-msk-1").unwrap();
    sh_ok(&s);
    assert!(!s.contains("amneziawg"));
    assert!(!s.contains("linux-headers"));
}
