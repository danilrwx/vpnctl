use std::path::PathBuf;
use vpnctl::ops;
use vpnctl::state::*;
use vpnctl::validate::validate;

fn fixture() -> State {
    State::load(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/basic")).unwrap()
}

#[test]
fn fixture_is_valid() {
    let st = fixture();
    assert!(validate(&st).is_empty(), "{:?}", validate(&st));
    assert_eq!(st.hosts.len(), 2);
    assert_eq!(
        st.users["brat"].awg.as_ref().unwrap().ip.to_string(),
        "10.8.1.2"
    );
    assert_eq!(st.entry_of(&st.users["olga"]).as_deref(), Some("ru-msk-1"));
    assert_eq!(
        State::email("brat", Mode::Split, Proto::Reality),
        "brat-split-reality"
    );
}

#[test]
fn save_load_round_trip() {
    let st = fixture();
    let tmp = tempfile::tempdir().unwrap();
    let mut copy = st.clone();
    copy.dir = tmp.path().to_path_buf();
    copy.save().unwrap();
    let back = State::load(tmp.path()).unwrap();
    assert_eq!(
        back.secrets.users["brat"].uuid,
        st.secrets.users["brat"].uuid
    );
    assert_eq!(
        back.routing["ru-msk-1"].groups.split.len(),
        st.routing["ru-msk-1"].groups.split.len()
    );
    assert!(validate(&back).is_empty());
}

#[test]
fn init_and_ops_commit() {
    let tmp = tempfile::tempdir().unwrap();
    let init_msg = ops::init(tmp.path(), None).unwrap();
    vpnctl::git::init(tmp.path()).unwrap();
    vpnctl::git::commit(tmp.path(), &init_msg).unwrap();
    let mut st = State::load(tmp.path()).unwrap();
    let msg = ops::host_add(
        &mut st,
        "ru",
        Role::Entry,
        "root@10.0.0.1",
        None,
        Some("gw.test".into()),
        None,
        Engine::SingBox,
        true,
    )
    .unwrap();
    assert_eq!(msg, "hosts: + ru (вход)");
    ops::host_add(
        &mut st,
        "fi",
        Role::Exit,
        "root@10.0.0.2",
        None,
        None,
        Some("ru".into()),
        Engine::SingBox,
        true,
    )
    .unwrap();
    ops::link_add(&mut st, "ru", "fi", vec![], Policy::Auto, None).unwrap();
    let msg = ops::user_add(
        &mut st,
        "sergey",
        vec![Mode::Split, Mode::Full],
        vec![Proto::Reality],
        Some(Mode::Full),
        false,
        None,
        None,
        None,
    )
    .unwrap();
    assert_eq!(msg, "users: + sergey (split, full)");
    assert_eq!(
        st.users["sergey"].awg.as_ref().unwrap().ip.to_string(),
        "10.8.2.2"
    );
    assert!(st.users["sergey"].protocols.contains(&Proto::Awg));
    assert_eq!(st.routing["ru"].exit.prefer.as_deref(), Some("fi"));
    assert!(validate(&st).is_empty(), "{:?}", validate(&st));
    st.save().unwrap();
    vpnctl::git::commit(tmp.path(), &msg).unwrap();
    assert!(vpnctl::git::is_clean(tmp.path()).unwrap());
    assert_eq!(vpnctl::git::branch(tmp.path()).unwrap(), "main");
}

fn has(errs: &[String], needle: &str) -> bool {
    errs.iter().any(|e| e.contains(needle))
}

#[test]
fn via_cycle() {
    let mut st = fixture();
    st.hosts.get_mut("ru-msk-1").unwrap().via = Some("fi-hel-1".into());
    assert!(has(&validate(&st), "цикл в графе via"));
}

#[test]
fn link_without_receiver_inbound() {
    let mut st = fixture();
    st.inbounds
        .get_mut("fi-hel-1")
        .unwrap()
        .get_mut(&Proto::Reality)
        .unwrap()
        .enabled = false;
    let e = validate(&st);
    assert!(
        has(
            &e,
            "на приёмнике «fi-hel-1» нет включённого inbound reality"
        ),
        "{e:?}"
    );
}

#[test]
fn duplicate_domain_needs_retiring() {
    let mut st = fixture();
    st.hosts.get_mut("fi-hel-1").unwrap().domain = Some("gw.example.ru".into());
    assert!(has(&validate(&st), "только на время переезда"));
    st.hosts.get_mut("ru-msk-1").unwrap().retiring = true;
    assert!(!has(&validate(&st), "только на время переезда"));
}

#[test]
fn xray_cannot_hy2() {
    let mut st = fixture();
    st.hosts.get_mut("ru-msk-1").unwrap().engine = Engine::Xray;
    assert!(has(&validate(&st), "engine xray не умеет hysteria2"));
}

#[test]
fn awg_peer_checks() {
    let mut st = fixture();
    st.users.get_mut("brat").unwrap().awg = Some(AwgPeer {
        mode: Mode::Full,
        ip: "10.8.1.2".parse().unwrap(),
    });
    assert!(has(&validate(&st), "вне подсети режима full"));
    st.users.get_mut("brat").unwrap().awg = Some(AwgPeer {
        mode: Mode::Split,
        ip: "10.8.1.1".parse().unwrap(),
    });
    assert!(has(&validate(&st), "зарезервирован за шлюзом"));
    let mut st = fixture();
    st.users.get_mut("olga").unwrap().awg = Some(AwgPeer {
        mode: Mode::Split,
        ip: "10.8.1.2".parse().unwrap(),
    });
    st.users.get_mut("olga").unwrap().protocols.push(Proto::Awg);
    let e = validate(&st);
    assert!(has(&e, "адрес awg 10.8.1.2 и у"), "{e:?}");
    assert!(has(&e, "нет ключей awg"), "{e:?}");
}

#[test]
fn rules_and_sets() {
    let mut st = fixture();
    st.routing.get_mut("ru-msk-1").unwrap().groups.split[0]
        .match_
        .sets = vec!["nope".into()];
    assert!(has(&validate(&st), "набор «nope» не описан"));
    let mut st = fixture();
    let r = ops::rule_add(
        &mut st,
        "ru-msk-1",
        Group::Split,
        Some(1),
        Match::default(),
        "link:auto".parse().unwrap(),
        None,
    )
    .unwrap();
    assert!(r.contains("split #1"));
    assert!(has(&validate(&st), "правила ниже недостижимы"));
    let mut st = fixture();
    assert!(
        ops::rule_add(
            &mut st,
            "ru-msk-1",
            Group::Full,
            None,
            Match::default(),
            "link:nl".parse().unwrap(),
            None
        )
        .is_ok()
    );
    assert!(has(&validate(&st), "линка ru-msk-1->nl нет"));
    assert!(ops::rule_move(&mut st, "ru-msk-1", Group::Split, 1, 9).is_err());
}

#[test]
fn link_set_and_exit() {
    let mut st = fixture();
    assert!(
        ops::link_set(
            &mut st,
            "ru-msk-1",
            "fi-hel-1",
            None,
            Some(Policy::Fixed),
            vec![],
            vec![Proto::Awg, Proto::Reality, Proto::Hy2]
        )
        .is_ok()
    );
    assert!(has(&validate(&st), "нет протоколов"));
    let mut st = fixture();
    ops::exit_set(&mut st, "ru-msk-1", Some("nl".into()), None).unwrap();
    assert!(has(&validate(&st), "exit.prefer «nl»"));
    assert!(ops::exit_set(&mut st, "ru-msk-1", None, None).is_err());
}
