use anyhow::{Context, Result, bail};
use clap::{Args, Parser, Subcommand};
use std::path::PathBuf;
use vpnctl::ops;
use vpnctl::state::*;

#[derive(Parser)]
#[command(
    name = "vpnctl",
    version,
    about = "VPN-инфраструктура как состояние в git"
)]
struct Cli {
    /// Каталог состояния
    #[arg(short = 'C', long, global = true, default_value = ".")]
    dir: PathBuf,
    /// Не коммитить после изменения (пакетные правки)
    #[arg(long, global = true)]
    no_commit: bool,
    /// Не читать секреты, подставить заглушки (CI без ключа age; только validate/render)
    #[arg(long, global = true)]
    stub_secrets: bool,
    /// Без подкоманды — TUI
    #[command(subcommand)]
    cmd: Option<Cmd>,
}

#[derive(Subcommand)]
enum Cmd {
    /// Интерактивный интерфейс (по умолчанию)
    Tui,
    /// Создать пустое состояние и git-репозиторий
    Init {
        /// Получатель age для sops; без него секреты пишутся открытым текстом
        #[arg(long)]
        age: Option<String>,
    },
    /// Проверить состояние
    Validate,
    /// Отрендерить конфиги в каталог
    Render {
        #[arg(long)]
        host: Option<String>,
        #[arg(long, default_value = "out")]
        out: PathBuf,
        /// Два прогона, сравнение побайтно; ничего не пишет
        #[arg(long)]
        check: bool,
    },
    /// Что изменится на хостах: диф, дрейф, недоступные
    Plan {
        #[arg(long)]
        host: Option<String>,
        /// Показать диф файла: --diff <хост> <путь>
        #[arg(long, num_args = 2, value_names = ["ХОСТ", "ПУТЬ"])]
        diff: Option<Vec<String>>,
        /// Забрать файл с хоста в overrides/: --adopt <хост> <путь>
        #[arg(long, num_args = 2, value_names = ["ХОСТ", "ПУТЬ"])]
        adopt: Option<Vec<String>>,
    },
    /// Применить состояние на хосты по SSH
    Apply {
        #[arg(long)]
        host: Option<String>,
        /// Применять не с main
        #[arg(long)]
        branch: Option<String>,
        /// Не останавливаться на мёртвом линке и отсутствии сертификата
        #[arg(long)]
        force: bool,
    },
    /// Снять метрики и пробу цепочки с хостов в metrics.sqlite
    Collect,
    /// Проба цепочки с входов; --matrix — рёбра ноутбук→хост и вход→выход по протоколам
    Probe {
        #[arg(long)]
        matrix: bool,
    },
    /// Сводка из metrics.sqlite без обращения к хостам
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
        /// user@ip для ssh
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
    },
    Rm {
        name: String,
    },
    /// Подготовить хост: пакеты, бинарники по versions.yaml, фаервол, AWG, юниты, маркер
    Bootstrap {
        name: String,
        /// user@ip, если отличается от записанного
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
        /// AWG-пир в режиме split|full
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
    /// Ссылки на подписки, QR, tg-прокси, профиль awg
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
        /// awg,reality,hy2 (по умолчанию awg,reality)
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
    /// Политика выбора выхода для входа
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
        /// Позиция 1..n+1; по умолчанию перед правилом «всё остальное»
        #[arg(long)]
        pos: Option<usize>,
        #[command(flatten)]
        m: MatchArgs,
        /// direct | block | link:auto | link:<хост>
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
        _ => bail!("policy: auto, fixed или rr"),
    })
}

fn report(errors: &[String]) -> Result<()> {
    if errors.is_empty() {
        return Ok(());
    }
    for e in errors {
        eprintln!("  ✗ {e}");
    }
    bail!("{} ошибок в состоянии", errors.len())
}

fn main() -> Result<()> {
    let cli = Cli::parse();
    let cmd = cli.cmd.unwrap_or(Cmd::Tui);
    if let Cmd::Init { age } = &cmd {
        ops::init(&cli.dir, age.as_deref())?;
        println!("состояние создано в {}", cli.dir.display());
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
            "✓ состояние валидно: {} хостов, {} линков, {} пользователей",
            st.hosts.len(),
            st.links.len(),
            st.users.len()
        );
        return Ok(());
    }
    match &cmd {
        Cmd::Tui => {
            if cli.stub_secrets {
                bail!("--stub-secrets только для validate и render");
            }
            return vpnctl::tui::run(st);
        }
        Cmd::Render { host, out, check } => {
            report(&vpnctl::validate::validate(&st))?;
            let r = if *check {
                vpnctl::render::check(&st)?
            } else {
                vpnctl::render::render(&st, host.as_deref())?
            };
            if *check {
                println!(
                    "✓ рендер воспроизводим: {} хостов, {} файлов",
                    r.hosts.len(),
                    r.hosts.values().map(|f| f.len()).sum::<usize>()
                );
            } else {
                vpnctl::render::write(&r, out)?;
                for (h, files) in &r.hosts {
                    println!("{h}: {} файлов → {}", files.len(), out.join(h).display());
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
                    println!("{msg} (без коммита)");
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
                    "статическая проверка ✓ · {} хостов, {} линков · via без циклов, выходы, ключи, подсети",
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
                .with_context(|| format!("пользователь «{name}» не описан"))?;
            let entry = st.entry_of(u).context("у пользователя нет входа")?;
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
                    None => eprintln!("у пользователя нет awg-пира"),
                }
            }
            return Ok(());
        }
        _ => {}
    }
    if cli.stub_secrets {
        bail!("--stub-secrets только для validate и render");
    }
    let msg = match cmd {
        Cmd::Tui
        | Cmd::Init { .. }
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
        }) => {
            let role = match role.as_str() {
                "entry" => Role::Entry,
                "exit" => Role::Exit,
                _ => bail!("role: entry или exit"),
            };
            let engine = match engine.as_str() {
                "sing-box" => Engine::SingBox,
                "xray" => Engine::Xray,
                _ => bail!("engine: sing-box или xray"),
            };
            ops::host_add(&mut st, &name, role, &ssh, ip, domain, via, engine)?
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
                Some(_) => bail!("policy: auto или fixed"),
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
        eprintln!("изменение не записано:");
        report(&errs)?;
    }
    st.save()?;
    if cli.no_commit {
        println!("{msg} (без коммита)");
    } else {
        vpnctl::git::commit(&st.dir, &msg)?;
        println!("{msg}");
    }
    Ok(())
}
