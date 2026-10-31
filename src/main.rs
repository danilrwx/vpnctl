use anyhow::{Context, Result, bail};
use clap::{Args, Parser, Subcommand};
use std::path::PathBuf;
use vpnctl::ops;
use vpnctl::state::*;

#[derive(Parser)]
#[command(name = "vpnctl", version, about = "VPN infrastructure as state in git")]
struct Cli {
    /// State directory
    #[arg(short = 'C', long, global = true, default_value = ".")]
    dir: PathBuf,
    /// Do not commit after a change (batch edits)
    #[arg(long, global = true)]
    no_commit: bool,
    /// Do not read secrets, use stubs instead (CI without an age key; validate/render only)
    #[arg(long, global = true)]
    stub_secrets: bool,
    #[command(subcommand)]
    cmd: Cmd,
}

#[derive(Subcommand)]
enum Cmd {
    /// Create an empty state and a git repository
    Init {
        /// age recipient for sops; without it secrets are written as plaintext
        #[arg(long)]
        age: Option<String>,
    },
    /// Validate the state
    Validate,
    /// Render configs into a directory
    Render {
        #[arg(long)]
        host: Option<String>,
        #[arg(long, default_value = "out")]
        out: PathBuf,
        /// Two runs compared byte for byte; writes nothing
        #[arg(long)]
        check: bool,
    },
    /// What changes on the hosts: diff, drift, unreachable
    Plan {
        #[arg(long)]
        host: Option<String>,
        /// Show a file diff: --diff <host> <path>
        #[arg(long, num_args = 2, value_names = ["HOST", "PATH"])]
        diff: Option<Vec<String>>,
        /// Take a file from a host into overrides/: --adopt <host> <path>
        #[arg(long, num_args = 2, value_names = ["HOST", "PATH"])]
        adopt: Option<Vec<String>>,
    },
    /// Apply the state to the hosts over SSH
    Apply {
        #[arg(long)]
        host: Option<String>,
        /// Apply from a branch other than main
        #[arg(long)]
        branch: Option<String>,
        /// Do not stop on a dead link or a missing certificate
        #[arg(long)]
        force: bool,
    },
    /// Collect metrics and the chain probe into metrics.sqlite
    Collect,
    /// Chain probe from the entries; --matrix adds laptop->host and entry->exit edges per protocol
    Probe {
        #[arg(long)]
        matrix: bool,
    },
    /// Summary from metrics.sqlite without touching the hosts
    Status {
        #[arg(long)]
        json: bool,
    },
    #[command(subcommand)]
    Host(HostCmd),
    #[command(subcommand)]
    User(UserCmd),
    #[command(subcommand)]
    Link(LinkCmd),
    #[command(subcommand)]
    Exit(ExitCmd),
    #[command(subcommand)]
    Set(SetCmd),
    #[command(subcommand)]
    Rule(RuleCmd),
}

#[derive(Subcommand)]
enum HostCmd {
    Add {
        name: String,
        /// user@ip for ssh
        #[arg(long)]
        ssh: String,
        #[arg(long, default_value = "entry")]
        role: String,
        #[arg(long)]
        ip: Option<std::net::IpAddr>,
        #[arg(long)]
        domain: Option<String>,
        #[arg(long)]
        via: Option<String>,
        #[arg(long, default_value = "sing-box")]
        engine: String,
        /// Add an AmneziaWG inbound (not added by default)
        #[arg(long)]
        awg: bool,
    },
    Rm {
        name: String,
    },
    /// Prepare the host: packages, binaries from versions.yaml, firewall, AWG, units, marker
    Bootstrap {
        name: String,
        /// user@ip when it differs from the recorded one
        #[arg(long)]
        ssh: Option<String>,
    },
}

#[derive(Args, Clone, Default)]
struct UserFields {
    /// split,full
    #[arg(long, value_delimiter = ',')]
    modes: Option<Vec<Mode>>,
    /// reality,ws,hy2,awg
    #[arg(long, value_delimiter = ',')]
    protos: Option<Vec<Proto>>,
    /// YYYY-MM-DD
    #[arg(long)]
    expires: Option<String>,
    #[arg(long)]
    note: Option<String>,
}

#[derive(Subcommand)]
enum UserCmd {
    Add {
        name: String,
        #[command(flatten)]
        f: UserFields,
        /// AWG peer in split|full mode
        #[arg(long)]
        awg: Option<Mode>,
        #[arg(long)]
        tg: bool,
        #[arg(long)]
        entry: Option<String>,
    },
    Edit {
        name: String,
        #[command(flatten)]
        f: UserFields,
        #[arg(long)]
        no_expires: bool,
        #[arg(long)]
        tg: Option<bool>,
    },
    Disable {
        name: String,
    },
    Enable {
        name: String,
    },
    Rm {
        name: String,
    },
    /// Subscription links, QR, tg proxy, awg profile
    Link {
        name: String,
        #[arg(long)]
        qr: bool,
        #[arg(long)]
        tg: bool,
        #[arg(long)]
        awg: bool,
    },
}

#[derive(Subcommand)]
enum LinkCmd {
    Add {
        from: String,
        to: String,
        /// awg,reality,hy2 (defaults to awg,reality)
        #[arg(long, value_delimiter = ',')]
        protos: Vec<Proto>,
        #[arg(long, default_value = "auto")]
        policy: String,
        #[arg(long)]
        active: Option<Proto>,
    },
    Set {
        from: String,
        to: String,
        #[arg(long)]
        active: Option<Proto>,
        #[arg(long)]
        policy: Option<String>,
        #[arg(long, value_delimiter = ',')]
        add: Vec<Proto>,
        #[arg(long, value_delimiter = ',')]
        rm: Vec<Proto>,
    },
}

#[derive(Subcommand)]
enum ExitCmd {
    /// Exit selection policy for the entry
    Set {
        host: String,
        #[arg(long)]
        prefer: Option<String>,
        #[arg(long)]
        policy: Option<String>,
    },
}

#[derive(Subcommand)]
enum SetCmd {
    Edit {
        name: String,
        #[arg(long, value_delimiter = ',')]
        add: Vec<String>,
        #[arg(long, value_delimiter = ',')]
        rm: Vec<String>,
        #[arg(long, value_delimiter = ',')]
        geosite: Vec<String>,
        #[arg(long, value_delimiter = ',')]
        rm_geosite: Vec<String>,
        #[arg(long, value_delimiter = ',')]
        geoip: Vec<String>,
        #[arg(long, value_delimiter = ',')]
        rm_geoip: Vec<String>,
    },
    Rm {
        name: String,
    },
}

#[derive(Args, Clone, Default)]
struct MatchArgs {
    #[arg(long, value_delimiter = ',')]
    sets: Vec<String>,
    #[arg(long, value_delimiter = ',')]
    geosite: Vec<String>,
    #[arg(long, value_delimiter = ',')]
    geoip: Vec<String>,
    #[arg(long, value_delimiter = ',')]
    domains: Vec<String>,
    #[arg(long, value_delimiter = ',')]
    ip: Vec<ipnet::IpNet>,
    #[arg(long, value_delimiter = ',')]
    inbound: Vec<String>,
    #[arg(long, value_delimiter = ',')]
    port: Vec<u16>,
}

impl MatchArgs {
    fn into_match(self) -> Match {
        Match {
            sets: self.sets,
            geosite: self.geosite,
            geoip: self.geoip,
            domains: self.domains,
            ip: self.ip,
            inbound: self.inbound,
            port: self.port,
        }
    }
}

#[derive(Args, Clone)]
struct RuleRef {
    #[arg(long)]
    host: String,
    /// all | split | full
    #[arg(long)]
    group: Group,
}

#[derive(Subcommand)]
enum RuleCmd {
    Add {
        #[command(flatten)]
        r: RuleRef,
        /// Position 1..n+1; by default before the catch-all rule
        #[arg(long)]
        pos: Option<usize>,
        #[command(flatten)]
        m: MatchArgs,
        /// direct | block | link:auto | link:<host>
        #[arg(long)]
        action: Action,
        #[arg(long)]
        note: Option<String>,
    },
    Edit {
        #[command(flatten)]
        r: RuleRef,
        #[arg(long)]
        idx: usize,
        #[command(flatten)]
        m: MatchArgs,
        #[arg(long)]
        action: Option<Action>,
        #[arg(long)]
        note: Option<String>,
    },
    Rm {
        #[command(flatten)]
        r: RuleRef,
        #[arg(long)]
        idx: usize,
    },
    Move {
        #[command(flatten)]
        r: RuleRef,
        #[arg(long)]
        idx: usize,
        #[arg(long)]
        to: usize,
    },
    Toggle {
        #[command(flatten)]
        r: RuleRef,
        #[arg(long)]
        idx: usize,
    },
}

fn parse_policy(s: &str) -> Result<Policy> {
    Ok(match s {
        "auto" => Policy::Auto,
        "fixed" => Policy::Fixed,
        "rr" => Policy::Rr,
        _ => bail!("policy: auto, fixed or rr"),
    })
}

fn report(errors: &[String]) -> Result<()> {
    if errors.is_empty() {
        return Ok(());
    }
    for e in errors {
        eprintln!("  ✗ {e}");
    }
    bail!("{} errors in the state", errors.len())
}

fn main() -> Result<()> {
    let cli = Cli::parse();
    let cmd = cli.cmd;
    if let Cmd::Init { age } = &cmd {
        let msg = ops::init(&cli.dir, age.as_deref())?;
        vpnctl::git::init(&cli.dir)?;
        vpnctl::git::commit(&cli.dir, &msg)?;
        println!("state created in {}", cli.dir.display());
        return Ok(());
    }
    let mut st = if cli.stub_secrets {
        State::load_stub(&cli.dir)?
    } else {
        State::load(&cli.dir)?
    };
    if let Cmd::Validate = cmd {
        let errs = vpnctl::validate::validate(&st);
        report(&errs)?;
        println!(
            "✓ state is valid: {} hosts, {} links, {} users",
            st.hosts.len(),
            st.links.len(),
            st.users.len()
        );
        return Ok(());
    }
    match &cmd {
        Cmd::Render { host, out, check } => {
            report(&vpnctl::validate::validate(&st))?;
            let r = if *check {
                vpnctl::render::check(&st)?
            } else {
                vpnctl::render::render(&st, host.as_deref())?
            };
            if *check {
                println!(
                    "✓ render is reproducible: {} hosts, {} files",
                    r.hosts.len(),
                    r.hosts.values().map(|f| f.len()).sum::<usize>()
                );
            } else {
                vpnctl::render::write(&r, out)?;
                for (h, files) in &r.hosts {
                    println!("{h}: {} files → {}", files.len(), out.join(h).display());
                }
            }
            return Ok(());
        }
        Cmd::Host(HostCmd::Bootstrap { name, ssh }) => {
            report(&vpnctl::validate::validate(&st))?;
            vpnctl::bootstrap::run(&mut st, name, ssh.clone())?;
            return Ok(());
        }
        Cmd::Plan { host, diff, adopt } => {
            report(&vpnctl::validate::validate(&st))?;
            let r = vpnctl::render::check(&st)?;
            if let Some(a) = adopt {
                let msg = vpnctl::plan::adopt(&st, &a[0], &a[1])?;
                if cli.no_commit {
                    println!("{msg} (not committed)");
                } else {
                    vpnctl::git::commit(&st.dir, &msg)?;
                    println!("{msg}");
                }
                return Ok(());
            }
            if let Some(d) = diff {
                print!("{}", vpnctl::plan::diff(&st, &r, &d[0], &d[1])?);
                return Ok(());
            }
            let r = match host {
                Some(h) => vpnctl::render::render(&st, Some(h))?,
                None => r,
            };
            let plans = vpnctl::plan::plan(&st, &r)?;
            vpnctl::plan::print(&plans);
            return Ok(());
        }
        Cmd::Apply {
            host,
            branch,
            force,
        } => {
            report(&vpnctl::validate::validate(&st))?;
            vpnctl::apply::apply(
                &st,
                &vpnctl::apply::Opts {
                    host: host.clone(),
                    branch: branch.clone(),
                    force: *force,
                },
            )?;
            return Ok(());
        }
        Cmd::Collect => {
            report(&vpnctl::validate::validate(&st))?;
            vpnctl::metrics::collect(&st, &vpnctl::metrics::Db::open(&st.dir)?)?;
            return Ok(());
        }
        Cmd::Probe { matrix } => {
            use vpnctl::metrics;
            let db = metrics::Db::open(&st.dir)?;
            if !*matrix {
                return metrics::probe(&st, &db);
            }
            let errs = vpnctl::validate::validate(&st);
            if errs.is_empty() {
                println!(
                    "static check ✓ · {} hosts, {} links · acyclic via, exits, keys, subnets",
                    st.hosts.len(),
                    st.links.len()
                );
            } else {
                report(&errs)?;
            }
            print!("{}", metrics::table(&metrics::matrix(&st, &db)?));
            return Ok(());
        }
        Cmd::Status { json } => {
            use vpnctl::metrics;
            let s = metrics::status(&st, &metrics::Db::open(&st.dir)?, metrics::now())?;
            if *json {
                println!("{}", serde_json::to_string(&s)?);
            } else {
                metrics::print_status(&s);
            }
            return Ok(());
        }
        Cmd::User(UserCmd::Link { name, qr, tg, awg }) => {
            use vpnctl::render::subs;
            let u = st
                .users
                .get(name)
                .with_context(|| format!("user «{name}» is not described"))?;
            let entry = st.entry_of(u).context("the user has no entry")?;
            for m in &u.modes {
                let url = subs::sub_url(&st, &entry, name, *m)?;
                println!("{} · {}", m.name(), url);
                for l in subs::links(&st, &entry, name, u, *m)? {
                    println!("  {l}");
                }
                if *qr {
                    println!("{}", subs::qr(&url));
                }
            }
            if (u.tg || *tg)
                && let Some(t) = subs::tg_link(&st, &entry)
            {
                println!("telegram · {t}");
                if *qr {
                    println!("{}", subs::qr(&t));
                }
            }
            if *awg {
                let files = vpnctl::render::render_host(&st, &entry)?;
                let p = format!("/etc/vpnctl/profiles/{name}/awg.conf");
                match files.get(&p) {
                    Some(c) => print!("{c}"),
                    None => eprintln!("the user has no awg peer"),
                }
            }
            return Ok(());
        }
        _ => {}
    }
    if cli.stub_secrets {
        bail!("--stub-secrets is only valid for validate and render");
    }
    let msg = match cmd {
        Cmd::Init { .. }
        | Cmd::Validate
        | Cmd::Render { .. }
        | Cmd::Plan { .. }
        | Cmd::Apply { .. }
        | Cmd::Collect
        | Cmd::Probe { .. }
        | Cmd::Status { .. }
        | Cmd::User(UserCmd::Link { .. })
        | Cmd::Host(HostCmd::Bootstrap { .. }) => {
            unreachable!()
        }
        Cmd::Host(HostCmd::Add {
            name,
            ssh,
            role,
            ip,
            domain,
            via,
            engine,
            awg,
        }) => {
            let role = match role.as_str() {
                "entry" => Role::Entry,
                "exit" => Role::Exit,
                _ => bail!("role: entry or exit"),
            };
            let engine = match engine.as_str() {
                "sing-box" => Engine::SingBox,
                "xray" => Engine::Xray,
                _ => bail!("engine: sing-box or xray"),
            };
            ops::host_add(&mut st, &name, role, &ssh, ip, domain, via, engine, awg)?
        }
        Cmd::Host(HostCmd::Rm { name }) => ops::host_rm(&mut st, &name)?,
        Cmd::User(UserCmd::Add {
            name,
            f,
            awg,
            tg,
            entry,
        }) => ops::user_add(
            &mut st,
            &name,
            f.modes.unwrap_or_else(|| vec![Mode::Split]),
            f.protos
                .unwrap_or_else(|| vec![Proto::Reality, Proto::Ws, Proto::Hy2]),
            awg,
            tg,
            f.expires,
            f.note,
            entry,
        )?,
        Cmd::User(UserCmd::Edit {
            name,
            f,
            no_expires,
            tg,
        }) => ops::user_edit(
            &mut st,
            &name,
            ops::UserPatch {
                modes: f.modes,
                protocols: f.protos,
                expires: if no_expires {
                    Some(None)
                } else {
                    f.expires.map(Some)
                },
                note: f.note,
                tg,
            },
        )?,
        Cmd::User(UserCmd::Disable { name }) => ops::user_enable(&mut st, &name, false)?,
        Cmd::User(UserCmd::Enable { name }) => ops::user_enable(&mut st, &name, true)?,
        Cmd::User(UserCmd::Rm { name }) => ops::user_rm(&mut st, &name)?,
        Cmd::Link(LinkCmd::Add {
            from,
            to,
            protos,
            policy,
            active,
        }) => ops::link_add(&mut st, &from, &to, protos, parse_policy(&policy)?, active)?,
        Cmd::Link(LinkCmd::Set {
            from,
            to,
            active,
            policy,
            add,
            rm,
        }) => ops::link_set(
            &mut st,
            &from,
            &to,
            active,
            policy.as_deref().map(parse_policy).transpose()?,
            add,
            rm,
        )?,
        Cmd::Exit(ExitCmd::Set {
            host,
            prefer,
            policy,
        }) => {
            let policy = match policy.as_deref() {
                None => None,
                Some("auto") => Some(ExitPolicy::Auto),
                Some("fixed") => Some(ExitPolicy::Fixed),
                Some(_) => bail!("policy: auto or fixed"),
            };
            ops::exit_set(&mut st, &host, prefer, policy)?
        }
        Cmd::Set(SetCmd::Edit {
            name,
            add,
            rm,
            geosite,
            rm_geosite,
            geoip,
            rm_geoip,
        }) => ops::set_edit(
            &mut st,
            &name,
            (&add, &rm),
            (&geosite, &rm_geosite),
            (&geoip, &rm_geoip),
        )?,
        Cmd::Set(SetCmd::Rm { name }) => ops::set_rm(&mut st, &name)?,
        Cmd::Rule(RuleCmd::Add {
            r,
            pos,
            m,
            action,
            note,
        }) => ops::rule_add(&mut st, &r.host, r.group, pos, m.into_match(), action, note)?,
        Cmd::Rule(RuleCmd::Edit {
            r,
            idx,
            m,
            action,
            note,
        }) => {
            let m = m.into_match();
            ops::rule_edit(
                &mut st,
                &r.host,
                r.group,
                idx,
                (!m.is_empty()).then_some(m),
                action,
                note,
            )?
        }
        Cmd::Rule(RuleCmd::Rm { r, idx }) => ops::rule_rm(&mut st, &r.host, r.group, idx)?,
        Cmd::Rule(RuleCmd::Move { r, idx, to }) => {
            ops::rule_move(&mut st, &r.host, r.group, idx, to)?
        }
        Cmd::Rule(RuleCmd::Toggle { r, idx }) => ops::rule_toggle(&mut st, &r.host, r.group, idx)?,
    };
    let errs = vpnctl::validate::validate(&st);
    if !errs.is_empty() {
        eprintln!("change was not saved:");
        report(&errs)?;
    }
    st.save()?;
    if cli.no_commit {
        println!("{msg} (not committed)");
    } else {
        vpnctl::git::commit(&st.dir, &msg)?;
        println!("{msg}");
    }
    Ok(())
}
