//! State model: YAML files in the state directory plus decrypted secrets.
use anyhow::{Context, Result, bail};
use ipnet::Ipv4Net;
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::net::{IpAddr, Ipv4Addr};
use std::path::{Path, PathBuf};

pub type Map<T> = BTreeMap<String, T>;

fn t() -> bool {
    true
}
fn is_true(b: &bool) -> bool {
    *b
}
fn is_false(b: &bool) -> bool {
    !*b
}

#[derive(Serialize, Deserialize, Clone, Copy, PartialEq, Eq, Debug)]
#[serde(rename_all = "lowercase")]
pub enum Role {
    Entry,
    Exit,
}

#[derive(Serialize, Deserialize, Clone, Copy, PartialEq, Eq, Debug, Default)]
#[serde(rename_all = "kebab-case")]
pub enum Engine {
    #[default]
    SingBox,
    Xray,
}

#[derive(Serialize, Deserialize, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Debug, Hash)]
#[serde(rename_all = "lowercase")]
pub enum Proto {
    Reality,
    Ws,
    Hy2,
    Awg,
    Mtg,
}

impl Proto {
    pub const ALL: [Proto; 5] = [
        Proto::Reality,
        Proto::Ws,
        Proto::Hy2,
        Proto::Awg,
        Proto::Mtg,
    ];
    pub fn name(self) -> &'static str {
        match self {
            Proto::Reality => "reality",
            Proto::Ws => "ws",
            Proto::Hy2 => "hy2",
            Proto::Awg => "awg",
            Proto::Mtg => "mtg",
        }
    }
    /// Protocol that maps to an engine account (has an email), not an L3 peer or a separate service.
    pub fn is_engine_user(self) -> bool {
        matches!(self, Proto::Reality | Proto::Ws | Proto::Hy2)
    }
    /// Protocol that can carry a link between nodes.
    pub fn is_link(self) -> bool {
        matches!(self, Proto::Reality | Proto::Hy2 | Proto::Awg)
    }
}

impl std::str::FromStr for Proto {
    type Err = anyhow::Error;
    fn from_str(s: &str) -> Result<Self> {
        Proto::ALL
            .into_iter()
            .find(|p| p.name() == s)
            .with_context(|| format!("unknown protocol «{s}»"))
    }
}

#[derive(Serialize, Deserialize, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Debug)]
#[serde(rename_all = "lowercase")]
pub enum Mode {
    Split,
    Full,
}

impl Mode {
    pub const ALL: [Mode; 2] = [Mode::Split, Mode::Full];
    pub fn name(self) -> &'static str {
        match self {
            Mode::Split => "split",
            Mode::Full => "full",
        }
    }
}

impl std::str::FromStr for Mode {
    type Err = anyhow::Error;
    fn from_str(s: &str) -> Result<Self> {
        match s {
            "split" => Ok(Mode::Split),
            "full" => Ok(Mode::Full),
            _ => bail!("unknown mode «{s}», expected split or full"),
        }
    }
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct Host {
    pub role: Role,
    #[serde(default)]
    pub engine: Engine,
    pub ip: IpAddr,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub domain: Option<String>,
    #[serde(default)]
    pub os: String,
    /// `user@host` for ssh; usually root@ip.
    pub ssh: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub via: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub host_key: Option<String>,
    #[serde(default, skip_serializing_if = "is_false")]
    pub retiring: bool,
}

#[derive(Serialize, Deserialize, Clone, Copy, PartialEq, Eq, Debug, Default)]
#[serde(rename_all = "lowercase")]
pub enum Policy {
    #[default]
    Auto,
    Fixed,
    Rr,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq)]
pub struct UrlTest {
    #[serde(default = "UrlTest::d_interval")]
    pub interval_s: u32,
    #[serde(default = "UrlTest::d_tolerance")]
    pub tolerance_ms: u32,
    #[serde(default = "UrlTest::d_failures")]
    pub failures: u32,
}
impl UrlTest {
    fn d_interval() -> u32 {
        60
    }
    fn d_tolerance() -> u32 {
        300
    }
    fn d_failures() -> u32 {
        2
    }
}
impl Default for UrlTest {
    fn default() -> Self {
        Self {
            interval_s: 60,
            tolerance_ms: 300,
            failures: 2,
        }
    }
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct Link {
    pub from: String,
    pub to: String,
    pub protocols: Vec<Proto>,
    #[serde(default)]
    pub policy: Policy,
    /// Working protocol for fixed, preferred one for auto.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub active: Option<Proto>,
    /// AWG tunnel subnet between nodes (/30: .1 is from, .2 is to).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub tunnel: Option<Ipv4Net>,
    #[serde(default)]
    pub urltest: UrlTest,
}

impl Link {
    pub fn key(&self) -> String {
        format!("{}->{}", self.from, self.to)
    }
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct AwgSubnets {
    pub split: Ipv4Net,
    pub full: Ipv4Net,
}

impl AwgSubnets {
    pub fn for_mode(&self, m: Mode) -> Ipv4Net {
        match m {
            Mode::Split => self.split,
            Mode::Full => self.full,
        }
    }
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct Inbound {
    pub port: u16,
    #[serde(default = "t", skip_serializing_if = "is_true")]
    pub enabled: bool,
    /// Reality: cover site SNI; ws: path.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub sni: Option<String>,
    /// Reality: where the handshake goes, `host:port`; defaults to `sni:443` (or Caddy on the entry).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub handshake: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub path: Option<String>,
    /// AWG on the entry: client subnets per mode.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub subnets: Option<AwgSubnets>,
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct AwgPeer {
    pub mode: Mode,
    pub ip: Ipv4Addr,
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct User {
    pub modes: Vec<Mode>,
    pub protocols: Vec<Proto>,
    #[serde(default = "t", skip_serializing_if = "is_true")]
    pub enabled: bool,
    /// Date YYYY-MM-DD; absent means no expiry.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub expires: Option<String>,
    /// User entry; may be omitted when there is only one entry.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub entry: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub awg: Option<AwgPeer>,
    #[serde(default, skip_serializing_if = "is_false")]
    pub tg: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub note: Option<String>,
}

#[derive(Serialize, Deserialize, Clone, Debug, Default, PartialEq)]
pub struct Match {
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub sets: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub geosite: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub geoip: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub domains: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub ip: Vec<ipnet::IpNet>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub inbound: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub port: Vec<u16>,
}

impl Match {
    pub fn is_empty(&self) -> bool {
        *self == Match::default()
    }
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq)]
#[serde(rename_all = "lowercase")]
pub enum Simple {
    Direct,
    Block,
}

/// `direct` | `block` | `{link: <name>|auto}`
#[derive(Serialize, Deserialize, Clone, Debug, PartialEq)]
#[serde(untagged)]
pub enum Action {
    Simple(Simple),
    Link { link: String },
}

impl std::str::FromStr for Action {
    type Err = anyhow::Error;
    fn from_str(s: &str) -> Result<Self> {
        Ok(match s {
            "direct" => Action::Simple(Simple::Direct),
            "block" => Action::Simple(Simple::Block),
            l if l.starts_with("link:") => Action::Link {
                link: l[5..].to_string(),
            },
            "link" => Action::Link {
                link: "auto".into(),
            },
            _ => bail!("action «{s}»: expected direct, block, link:auto or link:<host>"),
        })
    }
}

impl std::fmt::Display for Action {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Action::Simple(Simple::Direct) => write!(f, "direct"),
            Action::Simple(Simple::Block) => write!(f, "block"),
            Action::Link { link } => write!(f, "link:{link}"),
        }
    }
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct Rule {
    #[serde(default, rename = "match", skip_serializing_if = "Match::is_empty")]
    pub match_: Match,
    pub action: Action,
    #[serde(default = "t", skip_serializing_if = "is_true")]
    pub enabled: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub note: Option<String>,
}

#[derive(Serialize, Deserialize, Clone, Copy, PartialEq, Eq, Debug)]
#[serde(rename_all = "lowercase")]
pub enum Group {
    All,
    Split,
    Full,
}

impl std::str::FromStr for Group {
    type Err = anyhow::Error;
    fn from_str(s: &str) -> Result<Self> {
        match s {
            "all" => Ok(Group::All),
            "split" => Ok(Group::Split),
            "full" => Ok(Group::Full),
            _ => bail!("group «{s}»: expected all, split or full"),
        }
    }
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct Groups {
    #[serde(default)]
    pub all: Vec<Rule>,
    #[serde(default)]
    pub split: Vec<Rule>,
    #[serde(default)]
    pub full: Vec<Rule>,
}

impl Groups {
    pub fn get_mut(&mut self, g: Group) -> &mut Vec<Rule> {
        match g {
            Group::All => &mut self.all,
            Group::Split => &mut self.split,
            Group::Full => &mut self.full,
        }
    }
}

#[derive(Serialize, Deserialize, Clone, Copy, PartialEq, Eq, Debug, Default)]
#[serde(rename_all = "lowercase")]
pub enum ExitPolicy {
    #[default]
    Auto,
    Fixed,
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct Exit {
    #[serde(default)]
    pub policy: ExitPolicy,
    /// Preferred (auto) or the only (fixed) exit host.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub prefer: Option<String>,
    #[serde(default)]
    pub urltest: UrlTest,
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct Dns {
    /// Resolvers for domains from the direct rule sets.
    pub direct: Vec<IpAddr>,
    /// DoH for everything else, via the link.
    pub remote: String,
}

impl Default for Dns {
    fn default() -> Self {
        Self {
            direct: vec!["77.88.8.8".parse().unwrap(), "1.1.1.1".parse().unwrap()],
            remote: "https://1.1.1.1/dns-query".into(),
        }
    }
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct Routing {
    #[serde(default)]
    pub exit: Exit,
    #[serde(default)]
    pub groups: Groups,
    #[serde(default)]
    pub dns: Dns,
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct Set {
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub geosite: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub geoip: Vec<String>,
    /// As in xray: `example.ru` (suffix), `full:`, `keyword:`, `regexp:`.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub domains: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub ips: Vec<ipnet::IpNet>,
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct Pin {
    pub version: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub sha256: Option<String>,
}

// ---------- secrets ----------

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct KeyPair {
    pub private: String,
    pub public: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct UserSecrets {
    pub uuid: String,
    pub hy2_password: String,
    pub sub_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub awg: Option<KeyPair>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub awg_preshared: Option<String>,
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct RealityKeys {
    pub private: String,
    pub public: String,
    pub short_ids: Vec<String>,
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct HostSecrets {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reality: Option<RealityKeys>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub awg: Option<KeyPair>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub mtg_secret: Option<String>,
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct LinkAwg {
    pub from: KeyPair,
    pub to: KeyPair,
    pub preshared: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct LinkReality {
    pub uuid: String,
    pub keys: RealityKeys,
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct LinkSecrets {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub awg: Option<LinkAwg>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reality: Option<LinkReality>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub hy2_password: Option<String>,
}

#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct Secrets {
    #[serde(default)]
    pub users: Map<UserSecrets>,
    #[serde(default)]
    pub hosts: Map<HostSecrets>,
    #[serde(default)]
    pub links: Map<LinkSecrets>,
}

// ---------- files ----------

#[derive(Serialize, Deserialize, Default)]
struct HostsFile {
    #[serde(default)]
    hosts: Map<Host>,
}
#[derive(Serialize, Deserialize, Default)]
struct LinksFile {
    #[serde(default)]
    links: Vec<Link>,
}
#[derive(Serialize, Deserialize, Default)]
struct ProtocolsFile {
    #[serde(default)]
    inbounds: Map<BTreeMap<Proto, Inbound>>,
}
#[derive(Serialize, Deserialize, Default)]
struct UsersFile {
    #[serde(default)]
    users: Map<User>,
}

#[derive(Clone, Debug, Default)]
pub struct State {
    pub dir: PathBuf,
    pub hosts: Map<Host>,
    pub links: Vec<Link>,
    pub inbounds: Map<BTreeMap<Proto, Inbound>>,
    pub users: Map<User>,
    pub routing: Map<Routing>,
    pub sets: Map<Set>,
    pub versions: Map<Pin>,
    pub secrets: Secrets,
    /// Secrets were decrypted by sops, not read as plaintext.
    pub secrets_encrypted: bool,
}

fn read_yaml<T: serde::de::DeserializeOwned + Default>(p: &Path) -> Result<T> {
    if !p.exists() {
        return Ok(T::default());
    }
    let s = std::fs::read_to_string(p).with_context(|| format!("read {}", p.display()))?;
    serde_yaml::from_str(&s).with_context(|| format!("parse {}", p.display()))
}

fn write_yaml<T: Serialize>(p: &Path, v: &T) -> Result<()> {
    if let Some(d) = p.parent() {
        std::fs::create_dir_all(d)?;
    }
    std::fs::write(p, serde_yaml::to_string(v)?).with_context(|| format!("write {}", p.display()))
}

impl State {
    pub const SECRETS: &'static str = "secrets.sops.yaml";

    /// Load without secrets: deterministic stubs instead (CI, `render --check`).
    pub fn load_stub(dir: impl AsRef<Path>) -> Result<State> {
        let mut st = Self::load_inner(dir, false)?;
        st.fill_stub_secrets();
        Ok(st)
    }

    pub fn load(dir: impl AsRef<Path>) -> Result<State> {
        Self::load_inner(dir, true)
    }

    fn fill_stub_secrets(&mut self) {
        let stub = |what: &str| format!("STUB-{what}");
        for (n, u) in &self.users {
            let s = self.secrets.users.entry(n.clone()).or_default();
            s.uuid = stub(&format!("uuid-{n}"));
            s.hy2_password = stub(&format!("hy2-{n}"));
            s.sub_id = stub(&format!("sub-{n}"));
            if u.awg.is_some() {
                s.awg = Some(KeyPair {
                    private: stub(&format!("awg-priv-{n}")),
                    public: stub(&format!("awg-pub-{n}")),
                });
                s.awg_preshared = Some(stub(&format!("awg-psk-{n}")));
            }
        }
        for n in self.hosts.keys() {
            let s = self.secrets.hosts.entry(n.clone()).or_default();
            s.reality = Some(RealityKeys {
                private: stub(&format!("reality-priv-{n}")),
                public: stub(&format!("reality-pub-{n}")),
                short_ids: vec!["0000000000000000".into()],
            });
            s.awg = Some(KeyPair {
                private: stub(&format!("awg-priv-{n}")),
                public: stub(&format!("awg-pub-{n}")),
            });
            s.mtg_secret = Some("ee00000000000000000000000000000000".into());
        }
        for l in &self.links {
            let k = l.key();
            let s = self.secrets.links.entry(k.clone()).or_default();
            s.awg = Some(LinkAwg {
                from: KeyPair {
                    private: stub(&format!("{k}-from-priv")),
                    public: stub(&format!("{k}-from-pub")),
                },
                to: KeyPair {
                    private: stub(&format!("{k}-to-priv")),
                    public: stub(&format!("{k}-to-pub")),
                },
                preshared: stub(&format!("{k}-psk")),
            });
            s.reality = Some(LinkReality {
                uuid: stub(&format!("{k}-uuid")),
                keys: RealityKeys {
                    private: stub(&format!("{k}-priv")),
                    public: stub(&format!("{k}-pub")),
                    short_ids: vec!["1111111111111111".into()],
                },
            });
            s.hy2_password = Some(stub(&format!("{k}-hy2")));
        }
    }

    fn load_inner(dir: impl AsRef<Path>, with_secrets: bool) -> Result<State> {
        let dir = dir.as_ref().to_path_buf();
        if !dir.join("hosts.yaml").exists() && !dir.join("users.yaml").exists() {
            bail!(
                "{} does not look like a vpnctl state directory (no hosts.yaml); run `vpnctl init`",
                dir.display()
            );
        }
        let mut st = State {
            dir: dir.clone(),
            hosts: read_yaml::<HostsFile>(&dir.join("hosts.yaml"))?.hosts,
            links: read_yaml::<LinksFile>(&dir.join("links.yaml"))?.links,
            inbounds: read_yaml::<ProtocolsFile>(&dir.join("protocols.yaml"))?.inbounds,
            users: read_yaml::<UsersFile>(&dir.join("users.yaml"))?.users,
            versions: read_yaml(&dir.join("versions.yaml"))?,
            ..Default::default()
        };
        for name in st.hosts.keys() {
            let p = dir.join("routing").join(format!("{name}.yaml"));
            st.routing.insert(name.clone(), read_yaml(&p)?);
        }
        if let Ok(rd) = std::fs::read_dir(dir.join("sets")) {
            for e in rd {
                let p = e?.path();
                if p.extension().is_some_and(|x| x == "yaml") {
                    let name = p.file_stem().unwrap().to_string_lossy().to_string();
                    st.sets.insert(name, read_yaml(&p)?);
                }
            }
        }
        let sp = dir.join(Self::SECRETS);
        if with_secrets && sp.exists() {
            let raw = std::fs::read_to_string(&sp)?;
            let plain: serde_yaml::Value =
                serde_yaml::from_str(&raw).context("parse secrets.sops.yaml")?;
            if plain.get("sops").is_some() {
                let out = crate::secrets_io::current().decrypt(&sp)?;
                st.secrets = serde_yaml::from_str(&out).context("parse decrypted secrets")?;
                st.secrets_encrypted = true;
            } else {
                st.secrets =
                    serde_yaml::from_value(plain).context("parse secrets.sops.yaml (plaintext)")?;
            }
        }
        Ok(st)
    }

    /// Writes all state files. Secrets go through `sops -e` when `.sops.yaml` is present,
    /// otherwise as plaintext (for tests and CI with stubs).
    pub fn save(&self) -> Result<()> {
        let d = &self.dir;
        write_yaml(
            &d.join("hosts.yaml"),
            &HostsFile {
                hosts: self.hosts.clone(),
            },
        )?;
        write_yaml(
            &d.join("links.yaml"),
            &LinksFile {
                links: self.links.clone(),
            },
        )?;
        write_yaml(
            &d.join("protocols.yaml"),
            &ProtocolsFile {
                inbounds: self.inbounds.clone(),
            },
        )?;
        write_yaml(
            &d.join("users.yaml"),
            &UsersFile {
                users: self.users.clone(),
            },
        )?;
        write_yaml(&d.join("versions.yaml"), &self.versions)?;
        for (h, r) in &self.routing {
            write_yaml(&d.join("routing").join(format!("{h}.yaml")), r)?;
        }
        std::fs::create_dir_all(d.join("sets"))?;
        for (n, s) in &self.sets {
            write_yaml(&d.join("sets").join(format!("{n}.yaml")), s)?;
        }
        self.save_secrets()
    }

    fn save_secrets(&self) -> Result<()> {
        let plain = serde_yaml::to_string(&self.secrets)?;
        let sp = self.dir.join(Self::SECRETS);
        if !self.dir.join(".sops.yaml").exists() {
            return std::fs::write(&sp, plain).context("write secrets");
        }
        let out = crate::secrets_io::current().encrypt(&sp, &plain)?;
        std::fs::write(&sp, out).context("write secrets")
    }

    pub fn host(&self, name: &str) -> Result<&Host> {
        self.hosts
            .get(name)
            .with_context(|| format!("host «{name}» is not described"))
    }

    pub fn entries(&self) -> impl Iterator<Item = (&String, &Host)> {
        self.hosts.iter().filter(|(_, h)| h.role == Role::Entry)
    }

    /// User entry: explicit or the only one.
    pub fn entry_of(&self, user: &User) -> Option<String> {
        user.entry.clone().or_else(|| {
            let mut it = self.entries();
            match (it.next(), it.next()) {
                (Some((n, _)), None) => Some(n.clone()),
                _ => None,
            }
        })
    }

    pub fn link(&self, from: &str, to: &str) -> Option<&Link> {
        self.links.iter().find(|l| l.from == from && l.to == to)
    }

    pub fn links_from<'a>(&'a self, from: &'a str) -> impl Iterator<Item = &'a Link> {
        self.links.iter().filter(move |l| l.from == from)
    }

    /// Engine account email.
    pub fn email(user: &str, mode: Mode, proto: Proto) -> String {
        format!("{user}-{}-{}", mode.name(), proto.name())
    }
}
