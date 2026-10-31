//! TUI: app state, event loop, keys and background jobs. Logic lives in `model` and in the crate;
//! drawing in `screens`. Long operations run as a child `vpnctl` or `ssh` with their output streamed.
pub mod model;
mod screens;
mod ui;

use crate::metrics::{self, Db, Status};
use crate::plan::HostPlan;
use crate::render::Rendered;
use crate::ssh::Ssh;
use crate::state::*;
use crate::{git, validate};
use anyhow::{Context, Result, bail};
use model::*;
use ratatui::crossterm::event::{self, Event, KeyCode, KeyEvent, KeyEventKind};
use std::collections::BTreeMap;
use std::io::{BufRead, BufReader};
use std::process::{Command, Stdio};
use std::sync::mpsc::{self, Receiver};
use std::time::{Duration, Instant};

pub const VERSION: &str = env!("CARGO_PKG_VERSION");
pub const SCREENS: [&str; 6] = [
    "1 Overview",
    "2 Users",
    "3 Host",
    "4 Plan",
    "5 Topology",
    "6 Routing",
];

enum Msg {
    Line(String),
    Done(i32),
}

/// Where a job's output goes.
#[derive(Clone, PartialEq)]
enum Sink {
    /// Screen 4.
    Plan,
    /// Journal tail of a host (screen 3).
    HostLog(String),
}

struct Job {
    rx: Receiver<Msg>,
    sink: Sink,
    name: String,
}

enum Confirm {
    Apply(Option<String>),
    UserRm(String),
    RuleRm(String, Group, usize),
    /// A command on a host over ssh, no commit: (host, name, command).
    Ssh(String, String, String),
}

/// A finished plan: hosts, apply order, rendered files.
pub struct PlanData {
    pub plans: Vec<HostPlan>,
    pub order: Vec<String>,
    pub rendered: Rendered,
}

/// Scrollable text overlay: title, lines, scroll.
pub struct TextView {
    pub title: String,
    pub lines: Vec<String>,
    pub scroll: u16,
}

pub struct App {
    pub st: State,
    pub db: Db,
    pub status: Status,
    pub now: i64,
    // git
    pub head: Option<String>,
    pub head_msg: String,
    pub head_ts: i64,
    pub branch: String,
    pub clean: bool,
    pub last_apply: Option<String>,
    pub since_apply: usize,
    pub reproducible: bool,
    pub behind: BTreeMap<String, usize>,
    pub added: BTreeMap<String, (String, String)>,
    pub resolved: BTreeMap<String, Vec<std::net::IpAddr>>,
    // navigation
    pub screen: usize,
    pub sel: [usize; 6],
    pub entry_sel: usize,
    pub filter: String,
    pub filtering: bool,
    pub help: bool,
    pub msg: String,
    // users
    pub qr: usize,
    pub big_qr: bool,
    pub sort_traffic: bool,
    // host
    pub host_sel: usize,
    pub host_log: BTreeMap<String, Vec<String>>,
    // plan
    pub plan: Option<Result<PlanData, String>>,
    plan_rx: Option<Receiver<Result<PlanData, String>>>,
    pub plan_sel: usize,
    // topology
    pub proto_sel: usize,
    // routing
    pub trace_addr: String,
    pub trace_input: bool,
    pub trace_user: usize,
    // overlays
    pub form: Option<Form>,
    pub preview: Option<Result<(String, Vec<Row>), String>>,
    confirm: Option<Confirm>,
    pub confirm_text: String,
    pub text: Option<TextView>,
    // jobs
    job: Option<Job>,
    pub job_name: String,
    pub job_ok: Option<bool>,
    pub job_start: Option<Instant>,
    pub job_elapsed: Duration,
    pub log: Vec<String>,
}

pub fn run(st: State) -> Result<()> {
    let mut app = App::load(st)?;
    let mut term = ratatui::try_init().context("terminal")?;
    let r = (|| -> Result<()> {
        loop {
            app.poll()?;
            term.draw(|f| app.draw(f))?;
            if event::poll(Duration::from_millis(250))?
                && let Event::Key(k) = event::read()?
                && k.kind == KeyEventKind::Press
                && app.key(k)?
            {
                return Ok(());
            }
        }
    })();
    ratatui::restore();
    r
}

impl App {
    fn load(st: State) -> Result<App> {
        let db = Db::open(&st.dir)?;
        let now = metrics::now();
        let status = metrics::status(&st, &db, now)?;
        let mut app = App {
            st,
            db,
            status,
            now,
            head: None,
            head_msg: String::new(),
            head_ts: 0,
            branch: "—".into(),
            clean: true,
            last_apply: None,
            since_apply: 0,
            reproducible: false,
            behind: BTreeMap::new(),
            added: BTreeMap::new(),
            resolved: BTreeMap::new(),
            screen: 0,
            sel: [0; 6],
            entry_sel: 0,
            filter: String::new(),
            filtering: false,
            help: false,
            msg: "? help".into(),
            qr: 0,
            big_qr: false,
            sort_traffic: false,
            host_sel: 0,
            host_log: BTreeMap::new(),
            plan: None,
            plan_rx: None,
            plan_sel: 0,
            proto_sel: 0,
            trace_addr: "www.youtube.com:443".into(),
            trace_input: false,
            trace_user: 0,
            form: None,
            preview: None,
            confirm: None,
            confirm_text: String::new(),
            text: None,
            job: None,
            job_name: String::new(),
            job_ok: None,
            job_start: None,
            job_elapsed: Duration::ZERO,
            log: vec![],
        };
        app.refresh_meta();
        Ok(app)
    }

    /// Git facts, render check, DNS of domains, "added" commits of users.
    fn refresh_meta(&mut self) {
        let dir = self.st.dir.clone();
        if let Some((h, m, ts)) = git::head_info(&dir) {
            self.head = Some(h);
            self.head_msg = m;
            self.head_ts = ts;
        }
        self.branch = git::branch(&dir).unwrap_or("—".into());
        self.clean = git::is_clean(&dir).unwrap_or(true);
        self.last_apply = git::last_tag(&dir, "applied/");
        self.since_apply = self
            .last_apply
            .as_deref()
            .and_then(|t| git::count(&dir, t, "HEAD"))
            .unwrap_or(0);
        self.reproducible = crate::render::check(&self.st).is_ok();
        self.behind.clear();
        for h in self.status.hosts.values() {
            if let Some(m) = h.last.as_ref().and_then(|l| l.marker.clone())
                && let Some(n) = git::count(&dir, &m, "HEAD")
            {
                self.behind.insert(m, n);
            }
        }
        self.added = self
            .st
            .users
            .keys()
            .filter_map(|u| {
                git::added(&dir, "users.yaml", &format!("  {u}:")).map(|a| (u.clone(), a))
            })
            .collect();
        // ponytail: a blocking resolver per domain at reload; a background thread if DNS ever stalls
        use std::net::ToSocketAddrs;
        self.resolved = self
            .st
            .hosts
            .iter()
            .filter_map(|(n, h)| {
                let d = h.domain.as_ref()?;
                let ips = (d.as_str(), 443)
                    .to_socket_addrs()
                    .map(|a| a.map(|s| s.ip()).collect())
                    .unwrap_or_default();
                Some((n.clone(), ips))
            })
            .collect();
    }

    fn reload(&mut self) -> Result<()> {
        self.st = State::load(&self.st.dir)?;
        self.now = metrics::now();
        self.status = metrics::status(&self.st, &self.db, self.now)?;
        self.refresh_meta();
        Ok(())
    }

    /// Operation → validate → save → commit; on error the state is re-read from disk.
    fn commit(&mut self, f: impl FnOnce(&mut State) -> Result<String>) -> Result<String> {
        let r = f(&mut self.st).and_then(|msg| {
            let errs = validate::validate(&self.st);
            if !errs.is_empty() {
                bail!("not written: {}", errs.join("; "));
            }
            self.st.save()?;
            git::commit(&self.st.dir, &msg)?;
            Ok(msg)
        });
        self.reload()?;
        r
    }

    fn act(&mut self, f: impl FnOnce(&mut State) -> Result<String>) -> Result<()> {
        self.msg = match self.commit(f) {
            Ok(m) => m,
            Err(e) => format!("✗ {e:#}"),
        };
        Ok(())
    }

    // ---------- background jobs ----------

    fn vpnctl(&self, args: &[&str]) -> Result<Command> {
        let exe = std::env::current_exe().context("path to vpnctl")?;
        let mut c = Command::new(exe);
        c.arg("-C").arg(&self.st.dir).args(args);
        Ok(c)
    }

    /// Child `vpnctl` with output on screen 4.
    fn start_vpnctl(&mut self, name: &str, args: &[&str]) {
        match self.vpnctl(args) {
            Ok(c) => self.start(name, c, Sink::Plan),
            Err(e) => self.msg = format!("✗ {e:#}"),
        }
    }

    fn start_ssh(&mut self, host: &str, name: &str, cmd: &str, sink: Sink) {
        match Ssh::for_host(&self.st, host) {
            Ok(s) => self.start(name, s.command(cmd), sink),
            Err(e) => self.msg = format!("✗ {e:#}"),
        }
    }

    fn start(&mut self, name: &str, mut cmd: Command, sink: Sink) {
        if self.job.is_some() {
            self.msg = "a job is already running".into();
            return;
        }
        let child = cmd
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .context("spawn");
        let mut child = match child {
            Ok(c) => c,
            Err(e) => {
                self.msg = format!("✗ {e:#}");
                return;
            }
        };
        let (tx, rx) = mpsc::channel();
        let (out, err) = (child.stdout.take(), child.stderr.take());
        let tx2 = tx.clone();
        std::thread::spawn(move || {
            for l in BufReader::new(err.unwrap()).lines().map_while(Result::ok) {
                tx2.send(Msg::Line(l)).ok();
            }
        });
        std::thread::spawn(move || {
            for l in BufReader::new(out.unwrap()).lines().map_while(Result::ok) {
                tx.send(Msg::Line(l)).ok();
            }
            let code = child.wait().map(|s| s.code().unwrap_or(-1)).unwrap_or(-1);
            tx.send(Msg::Done(code)).ok();
        });
        if sink == Sink::Plan {
            self.log.clear();
            self.job_name = name.into();
            self.job_ok = None;
            self.job_start = Some(Instant::now());
            self.screen = 3;
        } else if let Sink::HostLog(h) = &sink {
            self.host_log.insert(h.clone(), vec![]);
        }
        self.job = Some(Job {
            rx,
            sink,
            name: name.into(),
        });
        self.msg = format!("{name}: running");
    }

    /// Plan in a thread: render check + parallel probe of the hosts.
    fn start_plan(&mut self) {
        if self.plan_rx.is_some() {
            self.msg = "plan is already running".into();
            return;
        }
        let st = self.st.clone();
        let (tx, rx) = mpsc::channel();
        std::thread::spawn(move || {
            let r = crate::render::check(&st)
                .and_then(|rendered| {
                    let plans = crate::plan::plan(&st, &rendered)?;
                    let names: Vec<String> = plans.iter().map(|p| p.name.clone()).collect();
                    Ok(PlanData {
                        order: crate::apply::order(&st, &names),
                        plans,
                        rendered,
                    })
                })
                .map_err(|e| format!("{e:#}"));
            tx.send(r).ok();
        });
        self.plan_rx = Some(rx);
        self.plan_sel = 0;
        self.screen = 3;
        self.msg = "plan: probing hosts".into();
    }

    fn poll(&mut self) -> Result<()> {
        if let Some(rx) = &self.plan_rx
            && let Ok(r) = rx.try_recv()
        {
            self.msg = match &r {
                Ok(p) => format!("plan: {} hosts", p.plans.len()),
                Err(e) => format!("✗ plan: {e}"),
            };
            self.plan = Some(r);
            self.plan_rx = None;
        }
        let Some(job) = &self.job else {
            return Ok(());
        };
        if let Some(s) = self.job_start
            && self.job_ok.is_none()
        {
            self.job_elapsed = s.elapsed();
        }
        let sink = job.sink.clone();
        let name = job.name.clone();
        let msgs: Vec<Msg> = job.rx.try_iter().collect();
        for m in msgs {
            match m {
                Msg::Line(l) => match &sink {
                    Sink::Plan => self.log.push(l),
                    Sink::HostLog(h) => self.host_log.entry(h.clone()).or_default().push(l),
                },
                Msg::Done(code) => {
                    self.job = None;
                    if sink == Sink::Plan {
                        self.job_ok = Some(code == 0);
                    }
                    self.msg = format!("{name}: {}", if code == 0 { "done" } else { "failed" });
                    self.reload()?;
                }
            }
        }
        Ok(())
    }

    pub fn job_running(&self) -> bool {
        self.job.as_ref().is_some_and(|j| j.sink == Sink::Plan)
    }

    pub fn plan_running(&self) -> bool {
        self.plan_rx.is_some()
    }

    // ---------- screen data ----------

    pub fn entries(&self) -> Vec<String> {
        self.st.entries().map(|(n, _)| n.clone()).collect()
    }

    pub fn host_names(&self) -> Vec<String> {
        self.st.hosts.keys().cloned().collect()
    }

    pub fn host_name(&self) -> Option<String> {
        self.host_names().get(self.host_sel).cloned()
    }

    /// Users table rows after filter and sort, with the names.
    pub fn user_table(&self) -> Vec<(String, Row)> {
        let mut rows = user_rows(&self.st, &self.status, self.now);
        sort_users(&mut rows, self.sort_traffic);
        filter(&rows, &self.filter)
            .into_iter()
            .map(|i| (rows[i].cells[0].clone(), rows[i].clone()))
            .collect()
    }

    pub fn user_name(&self) -> Option<String> {
        self.user_table().get(self.sel[1]).map(|(n, _)| n.clone())
    }

    pub fn routing_host(&self) -> Option<String> {
        let e = self.entries();
        e.get(self.entry_sel.min(e.len().saturating_sub(1)))
            .cloned()
    }

    pub fn rule_at(&self) -> Option<(String, Group, usize)> {
        let h = self.routing_host()?;
        let (g, i, _) = rule_rows(&self.st, &h).into_iter().nth(self.sel[5])?;
        Some((h, g, i))
    }

    /// Rows of the "Inbounds and exits" panel: inbounds then exits.
    pub fn host_rows3(&self) -> (Vec<Row3>, Vec<Row3>) {
        match self.host_name() {
            Some(h) => (
                inbound_rows(&self.st, &h),
                exit_rows(&self.st, &self.status, &h),
            ),
            None => (vec![], vec![]),
        }
    }

    pub fn plan_rows(&self) -> Vec<PlanRow> {
        let Some(Ok(p)) = &self.plan else {
            return vec![];
        };
        let roles = self
            .st
            .hosts
            .keys()
            .map(|n| (n.clone(), role_text(&self.st, n)))
            .collect();
        plan_rows(&p.plans, &p.order, self.head.as_deref(), &roles)
    }

    /// Host and path of the selected plan file row.
    fn plan_file(&self) -> Option<(String, String)> {
        match self.plan_rows().get(self.plan_sel)? {
            PlanRow::File { host, path, .. } => Some((host.clone(), path.clone())),
            _ => None,
        }
    }

    fn plan_host(&self) -> Option<String> {
        let rows = self.plan_rows();
        let upto = self.plan_sel.min(rows.len().checked_sub(1)?);
        for r in rows[..=upto].iter().rev() {
            match r {
                PlanRow::Host(t, _) => return t.split(" · ").next().map(String::from),
                PlanRow::File { host, .. } => return Some(host.clone()),
                _ => {}
            }
        }
        None
    }

    pub fn matrix(&self) -> Vec<MatrixRow> {
        matrix_rows(&self.status)
    }

    pub fn trace_user_name(&self) -> Option<(String, Mode)> {
        let users: Vec<(&String, &User)> = self.st.users.iter().collect();
        let (n, u) = users.get(self.trace_user % users.len().max(1))?;
        Some((
            n.to_string(),
            u.modes.first().copied().unwrap_or(Mode::Split),
        ))
    }

    fn len(&self, screen: usize) -> usize {
        match screen {
            0 => self.st.hosts.len(),
            1 => self.user_table().len(),
            2 => {
                let (i, e) = self.host_rows3();
                i.len() + e.len()
            }
            3 => self.plan_rows().len(),
            4 => self.matrix().len(),
            5 => self
                .routing_host()
                .map(|h| rule_rows(&self.st, &h).len())
                .unwrap_or(0),
            _ => 0,
        }
    }

    fn open(&mut self, f: Form) {
        self.form = Some(f);
        self.refresh_preview();
    }

    fn refresh_preview(&mut self) {
        self.preview = self
            .form
            .as_ref()
            .map(|f| preview(f, &self.st).map_err(|e| format!("{e:#}")));
    }

    pub fn confirm_pending(&self) -> bool {
        self.confirm.is_some()
    }

    fn ask(&mut self, text: &str, c: Confirm) {
        self.confirm_text = text.into();
        self.confirm = Some(c);
    }

    fn show_text(&mut self, title: &str, text: &str) {
        self.text = Some(TextView {
            title: title.into(),
            lines: text.lines().map(String::from).collect(),
            scroll: 0,
        });
    }

    /// OSC 52: put text into the terminal clipboard.
    fn copy(&mut self, s: &str) {
        use base64::Engine as _;
        use std::io::Write;
        let b = base64::engine::general_purpose::STANDARD.encode(s);
        let mut out = std::io::stdout();
        write!(out, "\x1b]52;c;{b}\x07").ok();
        out.flush().ok();
        self.msg = "copied to the terminal clipboard (OSC 52)".into();
    }

    fn selected_host(&self, s: usize) -> Option<String> {
        if s == 0 {
            self.host_names().get(self.sel[0]).cloned()
        } else {
            self.host_name()
        }
    }

    // ---------- keys ----------

    /// true — quit.
    fn key(&mut self, k: KeyEvent) -> Result<bool> {
        if let Some(t) = &mut self.text {
            match k.code {
                KeyCode::Esc | KeyCode::Char('q') => self.text = None,
                KeyCode::Char('j') | KeyCode::Down => t.scroll = t.scroll.saturating_add(1),
                KeyCode::Char('k') | KeyCode::Up => t.scroll = t.scroll.saturating_sub(1),
                KeyCode::PageDown => t.scroll = t.scroll.saturating_add(20),
                KeyCode::PageUp => t.scroll = t.scroll.saturating_sub(20),
                _ => {}
            }
            return Ok(false);
        }
        if let Some(f) = &mut self.form {
            match k.code {
                KeyCode::Esc => self.form = None,
                KeyCode::Tab | KeyCode::Down => f.next(),
                KeyCode::BackTab | KeyCode::Up => f.prev(),
                KeyCode::Left => f.move_cursor(-1),
                KeyCode::Right => f.move_cursor(1),
                KeyCode::Char(' ') => {
                    f.space();
                    self.refresh_preview();
                }
                KeyCode::Backspace => {
                    f.backspace();
                    self.refresh_preview();
                }
                KeyCode::Char(c) => {
                    f.input(c);
                    self.refresh_preview();
                }
                KeyCode::Enter => {
                    let form = f.clone();
                    match self.commit(|st| submit(&form, st)) {
                        Ok(m) => {
                            self.msg = m;
                            self.form = None;
                        }
                        Err(e) => {
                            if let Some(f) = &mut self.form {
                                f.error = Some(format!("{e:#}"));
                            }
                        }
                    }
                }
                _ => {}
            }
            return Ok(false);
        }
        if let Some(c) = self.confirm.take() {
            if k.code == KeyCode::Char('y') {
                match c {
                    Confirm::Apply(None) => self.start_vpnctl("apply", &["apply"]),
                    Confirm::Apply(Some(h)) => {
                        self.start_vpnctl(&format!("apply {h}"), &["apply", "--host", &h])
                    }
                    Confirm::UserRm(n) => self.act(|st| crate::ops::user_rm(st, &n))?,
                    Confirm::RuleRm(h, g, i) => {
                        self.act(|st| crate::ops::rule_rm(st, &h, g, i))?;
                        self.sel[5] = step(self.sel[5], self.len(5), 0);
                    }
                    Confirm::Ssh(h, name, cmd) => self.start_ssh(&h, &name, &cmd, Sink::Plan),
                }
            } else {
                self.msg = "cancelled".into();
            }
            return Ok(false);
        }
        if self.filtering || self.trace_input {
            let filtering = self.filtering;
            let buf = if filtering {
                &mut self.filter
            } else {
                &mut self.trace_addr
            };
            match k.code {
                KeyCode::Esc => {
                    if filtering {
                        buf.clear();
                    }
                    self.filtering = false;
                    self.trace_input = false;
                }
                KeyCode::Enter => {
                    self.filtering = false;
                    self.trace_input = false;
                }
                KeyCode::Backspace => {
                    buf.pop();
                }
                KeyCode::Char(c) => buf.push(c),
                _ => {}
            }
            if filtering {
                self.sel[1] = 0;
            }
            return Ok(false);
        }
        if self.help {
            self.help = false;
            return Ok(false);
        }
        let s = self.screen;
        match k.code {
            KeyCode::Char('q') => return Ok(true),
            KeyCode::Char(d @ '1'..='6') => {
                self.screen = d as usize - '1' as usize;
                self.msg.clear();
            }
            KeyCode::Char('?') => self.help = true,
            KeyCode::Char('/') => {
                self.filtering = true;
                self.screen = 1;
            }
            KeyCode::Esc => {
                self.filter.clear();
                self.msg.clear();
                self.screen = 0;
            }
            KeyCode::Char('j') | KeyCode::Down => {
                let n = self.len(s);
                match s {
                    3 => self.plan_sel = step(self.plan_sel, n, 1),
                    _ => self.sel[s] = step(self.sel[s], n, 1),
                }
            }
            KeyCode::Char('k') | KeyCode::Up => {
                let n = self.len(s);
                match s {
                    3 => self.plan_sel = step(self.plan_sel, n, -1),
                    _ => self.sel[s] = step(self.sel[s], n, -1),
                }
            }
            KeyCode::Char('p') => self.start_plan(),
            KeyCode::Char('a') => self.ask(
                "apply the state to all hosts? y — yes",
                Confirm::Apply(None),
            ),
            KeyCode::Char('r') => self.start_vpnctl("collect", &["collect"]),
            KeyCode::Char('t') if s != 5 => {
                self.start_vpnctl("probe --matrix", &["probe", "--matrix"])
            }
            KeyCode::Char('V') => {
                let errs = validate::validate(&self.st);
                self.msg = if errs.is_empty() {
                    "✓ state is valid".into()
                } else {
                    format!("✗ {}", errs.join("; "))
                };
            }
            // --- overview
            KeyCode::Enter if s == 0 => {
                self.host_sel = self.sel[0];
                self.sel[2] = 0;
                self.screen = 2;
            }
            KeyCode::Char('o') if s == 0 || s == 2 => {
                match self
                    .selected_host(s)
                    .filter(|h| self.st.hosts[h].role == Role::Entry)
                {
                    Some(h) => self.open(exit_form(&self.st, &h)),
                    None => self.msg = "exit policy is set on an entry".into(),
                }
            }
            KeyCode::Char('B') if s == 0 || s == 2 => {
                if let Some(h) = self.selected_host(s) {
                    self.start_vpnctl(&format!("bootstrap {h}"), &["host", "bootstrap", &h]);
                }
            }
            // --- users
            KeyCode::Enter if s == 1 => self.qr += 1,
            KeyCode::Char('T') if s == 1 => {
                if let Some(n) = self.user_name()
                    && let Ok(links) = user_links(&self.st, &n)
                    && let Some(i) = links.iter().position(|(l, _)| l.starts_with("Telegram"))
                {
                    self.qr = i;
                }
            }
            KeyCode::Char('Q') if s == 1 => self.big_qr = !self.big_qr,
            KeyCode::Char('M') if s == 1 => {
                if let Some(n) = self.user_name()
                    && let Ok(msg) = share_message(&self.st, &n)
                {
                    self.copy(&msg);
                }
            }
            KeyCode::Char('y') if s == 1 => {
                if let Some(n) = self.user_name()
                    && let Ok(links) = user_links(&self.st, &n)
                    && !links.is_empty()
                {
                    let url = links[self.qr % links.len()].1.clone();
                    self.copy(&url);
                }
            }
            KeyCode::Char('s') if s == 1 => self.sort_traffic = !self.sort_traffic,
            KeyCode::Char('f') if s == 1 => {
                if let Some(n) = self.user_name() {
                    let u = &self.st.users[&n];
                    match self.st.entry_of(u) {
                        Some(e) if u.awg.is_some() => {
                            let p = format!("/etc/vpnctl/profiles/{n}/awg.conf");
                            match crate::render::render_host(&self.st, &e) {
                                Ok(files) => match files.get(&p) {
                                    Some(c) => self.show_text(&p, c),
                                    None => self.msg = "no awg profile rendered".into(),
                                },
                                Err(e) => self.msg = format!("✗ {e:#}"),
                            }
                        }
                        _ => self.msg = "the user has no awg peer — nothing to show".into(),
                    }
                }
            }
            KeyCode::Char('n') if s == 1 => self.open(user_form(&self.st, None)),
            KeyCode::Char('e') if s == 1 => {
                if let Some(n) = self.user_name() {
                    self.open(user_form(&self.st, Some(&n)));
                }
            }
            KeyCode::Char('x') if s == 1 => {
                if let Some(n) = self.user_name() {
                    let on = !self.st.users[&n].enabled;
                    self.act(|st| crate::ops::user_enable(st, &n, on))?;
                }
            }
            KeyCode::Char('D') if s == 1 => {
                if let Some(n) = self.user_name() {
                    self.ask(&format!("delete user {n}? y — yes"), Confirm::UserRm(n));
                }
            }
            // --- host
            KeyCode::Char('h') | KeyCode::Left if s == 2 => {
                self.host_sel = step(self.host_sel, self.st.hosts.len(), -1);
                self.sel[2] = 0;
            }
            KeyCode::Right if s == 2 => {
                self.host_sel = step(self.host_sel, self.st.hosts.len(), 1);
                self.sel[2] = 0;
            }
            KeyCode::Char('l') if s == 2 => {
                if let Some(h) = self.host_name() {
                    let unit = match self.st.hosts[&h].engine {
                        Engine::SingBox => "sing-box",
                        Engine::Xray => "xray",
                    };
                    self.start_ssh(
                        &h,
                        "journal",
                        &format!("journalctl -u {unit} -n 20 --no-pager"),
                        Sink::HostLog(h.clone()),
                    );
                }
            }
            KeyCode::Char('x') if s == 2 => {
                if let Some(h) = self.host_name() {
                    let protos: Vec<Proto> = self
                        .st
                        .inbounds
                        .get(&h)
                        .map(|m| m.keys().copied().collect())
                        .unwrap_or_default();
                    match protos.get(self.sel[2]) {
                        Some(p) => {
                            let p = *p;
                            self.act(|st| crate::ops::inbound_toggle(st, &h, p))?;
                        }
                        None => self.msg = "select an inbound row".into(),
                    }
                }
            }
            KeyCode::Char('U') if s == 2 => {
                if let Some(h) = self.host_name() {
                    self.ask(
                        &format!("upgrade OS packages on {h}? y — yes"),
                        Confirm::Ssh(
                            h.clone(),
                            format!("upgrade {h}"),
                            "DEBIAN_FRONTEND=noninteractive apt-get -y -o Dpkg::Options::=--force-confold upgrade".into(),
                        ),
                    );
                }
            }
            KeyCode::Char('R') if s == 2 => {
                if let Some(h) = self.host_name() {
                    self.ask(
                        &format!("reboot {h}? y — yes"),
                        Confirm::Ssh(h.clone(), format!("reboot {h}"), "systemctl reboot".into()),
                    );
                }
            }
            // --- plan
            KeyCode::Enter if s == 3 => self.ask(
                "apply the state to all hosts? y — yes",
                Confirm::Apply(None),
            ),
            KeyCode::Char('s') if s == 3 => match self.plan_host() {
                Some(h) => self.ask(
                    &format!("apply to {h} only? y — yes"),
                    Confirm::Apply(Some(h)),
                ),
                None => self.msg = "select a host in the plan".into(),
            },
            KeyCode::Char('d') if s == 3 => match (self.plan_file(), &self.plan) {
                (Some((h, p)), Some(Ok(data))) => {
                    match crate::plan::diff(&self.st, &data.rendered, &h, &p) {
                        Ok(d) => self.show_text(&format!("diff {h}:{p}"), &d),
                        Err(e) => self.msg = format!("✗ {e:#}"),
                    }
                }
                _ => self.msg = "select a file row in the plan".into(),
            },
            KeyCode::Char('m') if s == 3 => match self.plan_file() {
                Some((h, p)) => {
                    self.act(|st| crate::plan::adopt(st, &h, &p))?;
                    self.plan = None;
                }
                None => self.msg = "select a file row in the plan".into(),
            },
            // --- topology
            KeyCode::Char('t') if s == 4 => {
                self.start_vpnctl("probe --matrix", &["probe", "--matrix"])
            }
            KeyCode::Enter if s == 4 => self.proto_sel = (self.proto_sel + 1) % MATRIX_COLS.len(),
            KeyCode::Char('o') if s == 4 => {
                if let Some(((src, dst), _)) = self.matrix().get(self.sel[4]).cloned() {
                    if self.st.link(&src, &dst).is_some() {
                        self.open(link_form(&self.st, &src, &dst));
                    } else {
                        self.msg = "the selected edge is not a link".into();
                    }
                }
            }
            KeyCode::Char('N') if s == 4 => self.open(link_form(&self.st, "", "")),
            // --- routing
            KeyCode::Char('h') if s == 5 => {
                let n = self.entries().len();
                self.entry_sel = (self.entry_sel + 1) % n.max(1);
                self.sel[5] = 0;
            }
            KeyCode::Char('t') if s == 5 => self.trace_input = true,
            KeyCode::Char('u') if s == 5 => self.trace_user += 1,
            KeyCode::Char('v') if s == 5 => {
                if let Some(h) = self.routing_host() {
                    let r = match self.st.hosts[&h].engine {
                        Engine::SingBox => crate::render::singbox::config(&self.st, &h),
                        Engine::Xray => crate::render::xray::config(&self.st, &h),
                    };
                    match r {
                        Ok(c) => self.show_text(&format!("{h} · engine config"), &c),
                        Err(e) => self.msg = format!("✗ {e:#}"),
                    }
                }
            }
            KeyCode::Char('n') if s == 5 => {
                if let Some(h) = self.routing_host() {
                    let g = self.rule_at().map(|(_, g, _)| g).unwrap_or(Group::All);
                    self.open(rule_form(&self.st, &h, g, None));
                }
            }
            KeyCode::Char('e') if s == 5 => {
                if let Some((h, g, i)) = self.rule_at() {
                    self.open(rule_form(&self.st, &h, g, Some(i)));
                }
            }
            KeyCode::Char('x') if s == 5 => {
                if let Some((h, g, i)) = self.rule_at() {
                    self.act(|st| crate::ops::rule_toggle(st, &h, g, i))?;
                }
            }
            KeyCode::Char('D') if s == 5 => {
                if let Some((h, g, i)) = self.rule_at() {
                    self.ask(
                        &format!("delete rule #{i}? y — yes"),
                        Confirm::RuleRm(h, g, i),
                    );
                }
            }
            KeyCode::Char(c @ ('J' | 'K')) if s == 5 => {
                if let Some((h, g, i)) = self.rule_at() {
                    let to = if c == 'J' { i + 1 } else { i.saturating_sub(1) };
                    if to >= 1 {
                        self.act(|st| crate::ops::rule_move(st, &h, g, i, to))?;
                        if !self.msg.starts_with('✗') {
                            let d = if c == 'J' { 1 } else { -1 };
                            self.sel[5] = step(self.sel[5], self.len(5), d);
                        }
                    }
                }
            }
            _ => {}
        }
        Ok(false)
    }
}
