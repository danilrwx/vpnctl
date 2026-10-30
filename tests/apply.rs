use std::path::PathBuf;
use vpnctl::apply;
use vpnctl::plan::{self, HostPlan, Status, classify};
use vpnctl::state::*;

fn fixture() -> State {
    State::load(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/basic")).unwrap()
}

#[test]
fn classify_statuses() {
    assert_eq!(classify(Some("a"), Some("b"), Some("b")), Status::Unchanged);
    assert_eq!(classify(Some("a"), Some("b"), Some("a")), Status::Changed);
    assert_eq!(classify(None, Some("b"), Some("x")), Status::Changed);
    assert_eq!(classify(Some("a"), Some("b"), Some("x")), Status::Drift);
    assert_eq!(classify(Some("a"), Some("b"), None), Status::New);
    assert_eq!(classify(Some("a"), None, Some("a")), Status::Removed);
}

#[test]
fn probe_roundtrip() {
    let s = plan::probe_script(&["/etc/x".into(), "/etc/y".into()]);
    assert!(s.contains("sha256sum"));
    let p = plan::parse_probe("a1f3c9e\nyes\n0123 /etc/x\nmissing /etc/y\n");
    assert_eq!(p.marker.as_deref(), Some("a1f3c9e"));
    assert!(p.bootstrapped);
    assert_eq!(p.hashes["/etc/x"].as_deref(), Some("0123"));
    assert_eq!(p.hashes["/etc/y"], None);
    assert_eq!(plan::parse_probe("-\nno\n").marker, None);
}

#[test]
fn order_leaves_first() {
    let mut st = fixture();
    st.hosts.insert(
        "nl".into(),
        Host {
            via: Some("fi-hel-1".into()),
            ..st.hosts["fi-hel-1"].clone()
        },
    );
    let o = apply::order(&st, &["ru-msk-1".into(), "fi-hel-1".into(), "nl".into()]);
    assert_eq!(o, vec!["nl", "fi-hel-1", "ru-msk-1"]);
    assert_eq!(
        vpnctl::ssh::Ssh::jump_chain(&st, "nl").unwrap(),
        vec!["ru-msk-1", "fi-hel-1"]
    );
}

#[test]
fn install_and_rollback_scripts() {
    let hp = HostPlan {
        name: "ru".into(),
        files: vec![
            ("/etc/sing-box/config.json".into(), Status::Changed),
            ("/etc/amnezia/amneziawg/awg1.conf".into(), Status::New),
            ("/etc/nftables.conf".into(), Status::Unchanged),
            ("/var/www/sub/x/split".into(), Status::Removed),
            ("/etc/caddy/Caddyfile".into(), Status::Drift),
        ],
        ..Default::default()
    };
    let s = apply::install_script(&hp, "/var/lib/vpnctl/backup/abc");
    assert!(s.contains("install -D -m 0600 '/tmp/.vpnctl/etc/sing-box/config.json'"));
    assert!(s.contains(".new; install -D -m 0600 '/tmp/.vpnctl/etc/amnezia/amneziawg/awg1.conf'"));
    assert!(s.contains("rm -f '/var/www/sub/x/split'"));
    assert!(
        !s.contains("nft -f"),
        "неизменённый nftables не перечитываем"
    );
    let awg = s.find("awg-quick@awg1").unwrap();
    let caddy = s.find("reload caddy").unwrap();
    let engine = s.find("restart sing-box").unwrap();
    assert!(awg < caddy && caddy < engine, "сеть → веб → движок");
    assert!(!s.contains("sshd"));
    let hp2 = HostPlan {
        name: "ru".into(),
        files: vec![("/etc/systemd/system/mtg.service".into(), Status::Changed)],
        ..Default::default()
    };
    let s2 = apply::install_script(&hp2, "/b");
    assert!(
        s2.contains("daemon-reload") && s2.contains("restart mtg"),
        "{s2}"
    );
    let r = apply::rollback_script(&hp, "/var/lib/vpnctl/backup/abc");
    assert!(r.contains("xargs -r rm -f < /var/lib/vpnctl/backup/abc/.new"));
    assert!(r.contains("restart sing-box"));
    assert_eq!(
        apply::service_for("/etc/vpnctl/profiles/brat/awg.conf"),
        None
    );
    assert!(apply::probe_script(Role::Entry).contains("--socks5-hostname 127.0.0.1:1080"));
}

#[test]
fn guards_need_clean_main() {
    let tmp = tempfile::tempdir().unwrap();
    vpnctl::ops::init(tmp.path(), None).unwrap();
    let st = State::load(tmp.path()).unwrap();
    assert!(apply::guards(&st, &apply::Opts::default()).is_ok());
    std::fs::write(tmp.path().join("users.yaml"), "users: {}\n# правка\n").unwrap();
    let e = apply::guards(&st, &apply::Opts::default())
        .unwrap_err()
        .to_string();
    assert!(e.contains("грязное дерево"), "{e}");
}

#[test]
fn overrides_win_in_render() {
    let st = fixture();
    let tmp = tempfile::tempdir().unwrap();
    let mut copy = st.clone();
    copy.dir = tmp.path().to_path_buf();
    copy.save().unwrap();
    let p = tmp.path().join("overrides/ru-msk-1/etc/caddy/Caddyfile");
    std::fs::create_dir_all(p.parent().unwrap()).unwrap();
    std::fs::write(&p, "# руками\n").unwrap();
    let files = vpnctl::render::render_host(&copy, "ru-msk-1").unwrap();
    assert_eq!(files["/etc/caddy/Caddyfile"], "# руками\n");
}

#[test]
fn ssh_quote_keeps_script_whole() {
    let q = vpnctl::ssh::Ssh::quote(
        "cat /nonexistent 2>/dev/null || echo -; printf '%s|%s\\n' \"$a\" \"$b\"",
    );
    assert!(q.starts_with("sh -c '"));
    let out = std::process::Command::new("sh")
        .arg("-c")
        .arg(&q)
        .output()
        .unwrap();
    assert_eq!(String::from_utf8_lossy(&out.stdout), "-\n|\n");
}
