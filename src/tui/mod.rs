//! TUI: цикл событий и рендер ratatui. Вся логика — в `model` (чистые функции) и в крейте.
//! Долгие операции (plan/apply/collect/probe) — дочерний `vpnctl`, вывод построчно на экран 4.
pub mod model;

use crate::metrics::{self, Db, Status};
use crate::state::*;
use crate::{git, validate};
use anyhow::{Context, Result, bail};
use model::*;
use ratatui::Frame;
use ratatui::crossterm::event::{self, Event, KeyCode, KeyEvent, KeyEventKind};
use ratatui::layout::{Constraint, Layout, Rect};
use ratatui::style::{Color, Style, Stylize};
use ratatui::text::Line;
use ratatui::widgets::{Block, Cell, Clear, Paragraph, Row as TRow, Table, TableState, Tabs, Wrap};
use std::io::{BufRead, BufReader};
use std::process::{Command, Stdio};
use std::sync::mpsc::{self, Receiver};
use std::time::Duration;

const SCREENS: [&str; 6] = [
    "1 Обзор",
    "2 Пользователи",
    "3 Хост",
    "4 План",
    "5 Топология",
    "6 Маршрут",
];
const HELP: [&str; 6] = [
    "j/k строка · Enter карточка хоста · p план · a применить · r метрики · t матрица · V проверка",
    "j/k строка · / фильтр · Enter следующая ссылка в QR · n новый · e править · x вкл/выкл · D удалить",
    "j/k хост · h/l линк · o диалог линка · N новый линк",
    "p план · a применить · r метрики · t матрица · вывод появляется построчно",
    "t прогнать матрицу · N новый линк",
    "j/k правило · h другой вход · n новое · e править · x вкл/выкл · D удалить · J/K переставить",
];

enum Msg {
    Line(String),
    Done(i32),
}

enum Confirm {
    Apply,
    UserRm(String),
    RuleRm(String, Group, usize),
}

struct App {
    st: State,
    status: Status,
    head: Option<String>,
    branch: String,
    clean: bool,
    last_apply: Option<String>,
    /// Проверка формы на лету: сообщение коммита и строки группы, либо ошибка.
    preview: Option<Result<(String, Vec<Row>), String>>,
    now: i64,
    screen: usize,
    sel: [usize; 6],
    link_sel: usize,
    entry_sel: usize,
    qr: usize,
    filter: String,
    filtering: bool,
    help: bool,
    form: Option<Form>,
    confirm: Option<Confirm>,
    msg: String,
    job: Option<Receiver<Msg>>,
    job_name: String,
    job_ok: Option<bool>,
    log: Vec<String>,
}

fn color(l: Level) -> Color {
    match l {
        Level::Ok => Color::Green,
        Level::Warn => Color::Yellow,
        Level::Bad => Color::Red,
        Level::Off => Color::DarkGray,
    }
}

fn center(area: Rect, w: u16, h: u16) -> Rect {
    let w = w.min(area.width);
    let h = h.min(area.height);
    Rect::new(
        area.x + (area.width - w) / 2,
        area.y + (area.height - h) / 2,
        w,
        h,
    )
}

fn widths(header: &[&str], rows: &[Row]) -> Vec<Constraint> {
    (0..header.len())
        .map(|i| {
            let w = rows
                .iter()
                .map(|r| r.cells[i].chars().count())
                .chain([header[i].chars().count()])
                .max()
                .unwrap_or(1);
            Constraint::Length(w as u16)
        })
        .collect()
}

fn table<'a>(header: &[&'a str], rows: &'a [Row], title: String) -> Table<'a> {
    Table::new(
        rows.iter()
            .map(|r| TRow::new(r.cells.iter().map(|c| Cell::from(c.as_str()))).fg(color(r.level))),
        widths(header, rows),
    )
    .header(TRow::new(header.iter().copied()).bold())
    .row_highlight_style(Style::new().reversed())
    .block(Block::bordered().title(title))
}

fn para<'a>(title: &'a str, lines: Vec<Line<'a>>) -> Paragraph<'a> {
    Paragraph::new(lines)
        .block(Block::bordered().title(title))
        .wrap(Wrap { trim: false })
}

pub fn run(st: State) -> Result<()> {
    let mut app = App::load(st)?;
    let mut term = ratatui::try_init().context("терминал")?;
    let r = (|| -> Result<()> {
        loop {
            app.poll_job()?;
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
        let now = metrics::now();
        let status = metrics::status(&st, &Db::open(&st.dir)?, now)?;
        let mut app = App {
            st,
            status,
            head: None,
            branch: "—".into(),
            clean: true,
            last_apply: None,
            preview: None,
            now,
            screen: 0,
            sel: [0; 6],
            link_sel: 0,
            entry_sel: 0,
            qr: 0,
            filter: String::new(),
            filtering: false,
            help: false,
            form: None,
            confirm: None,
            msg: "? помощь".into(),
            job: None,
            job_name: String::new(),
            job_ok: None,
            log: vec![],
        };
        app.git();
        Ok(app)
    }

    fn git(&mut self) {
        self.head = git::head(&self.st.dir).ok();
        self.branch = git::branch(&self.st.dir).unwrap_or("—".into());
        self.clean = git::is_clean(&self.st.dir).unwrap_or(true);
        self.last_apply = git::last_tag(&self.st.dir, "applied/");
    }

    fn reload(&mut self) -> Result<()> {
        self.st = State::load(&self.st.dir)?;
        self.now = metrics::now();
        self.status = metrics::status(&self.st, &Db::open(&self.st.dir)?, self.now)?;
        self.git();
        Ok(())
    }

    /// Операция → validate → save → commit; при ошибке состояние перечитано с диска.
    fn commit(&mut self, f: impl FnOnce(&mut State) -> Result<String>) -> Result<String> {
        let r = f(&mut self.st).and_then(|msg| {
            let errs = validate::validate(&self.st);
            if !errs.is_empty() {
                bail!("не записано: {}", errs.join("; "));
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

    // ---------- фоновые операции ----------

    fn start(&mut self, name: &str, args: &[&str]) {
        if self.job.is_some() {
            self.msg = "операция уже выполняется".into();
            return;
        }
        let child = std::env::current_exe()
            .context("путь к vpnctl")
            .and_then(|exe| {
                Command::new(exe)
                    .arg("-C")
                    .arg(&self.st.dir)
                    .args(args)
                    .stdin(Stdio::null())
                    .stdout(Stdio::piped())
                    .stderr(Stdio::piped())
                    .spawn()
                    .context("запуск vpnctl")
            });
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
        self.job = Some(rx);
        self.job_name = name.into();
        self.job_ok = None;
        self.log.clear();
        self.screen = 3;
        self.msg = format!("{name}: выполняется");
    }

    fn poll_job(&mut self) -> Result<()> {
        let Some(rx) = &self.job else {
            return Ok(());
        };
        let msgs: Vec<Msg> = rx.try_iter().collect();
        for m in msgs {
            match m {
                Msg::Line(l) => self.log.push(l),
                Msg::Done(code) => {
                    self.job = None;
                    self.job_ok = Some(code == 0);
                    self.msg = format!(
                        "{}: {}",
                        self.job_name,
                        if code == 0 {
                            "готово"
                        } else {
                            "ошибка"
                        }
                    );
                    self.reload()?;
                }
            }
        }
        Ok(())
    }

    // ---------- данные экранов ----------

    fn entries(&self) -> Vec<String> {
        self.st.entries().map(|(n, _)| n.clone()).collect()
    }

    fn host_names(&self) -> Vec<String> {
        self.st.hosts.keys().cloned().collect()
    }

    fn user_visible(&self) -> Vec<usize> {
        filter(&user_rows(&self.st, &self.status, self.now), &self.filter)
    }

    fn user_name(&self) -> Option<String> {
        let names: Vec<&String> = self.st.users.keys().collect();
        self.user_visible()
            .get(self.sel[1])
            .map(|i| names[*i].clone())
    }

    fn routing_host(&self) -> Option<String> {
        let e = self.entries();
        e.get(self.entry_sel.min(e.len().saturating_sub(1)))
            .cloned()
    }

    fn rule_at(&self) -> Option<(String, Group, usize)> {
        let h = self.routing_host()?;
        let (g, i, _) = rule_rows(&self.st, &h).into_iter().nth(self.sel[5])?;
        Some((h, g, i))
    }

    fn host_link(&self) -> Option<(String, String)> {
        let h = self.host_names().get(self.sel[2]).cloned()?;
        let to = self
            .st
            .links_from(&h)
            .nth(self.link_sel)
            .map(|l| l.to.clone())?;
        Some((h, to))
    }

    fn len(&self, screen: usize) -> usize {
        match screen {
            0 | 2 => self.st.hosts.len(),
            1 => self.user_visible().len(),
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

    // ---------- клавиши ----------

    /// true — выйти.
    fn key(&mut self, k: KeyEvent) -> Result<bool> {
        if let Some(f) = &mut self.form {
            match k.code {
                KeyCode::Esc => self.form = None,
                KeyCode::Tab | KeyCode::Down => f.next(),
                KeyCode::BackTab | KeyCode::Up => f.prev(),
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
                    Confirm::Apply => self.start("apply", &["apply"]),
                    Confirm::UserRm(n) => self.act(|st| crate::ops::user_rm(st, &n))?,
                    Confirm::RuleRm(h, g, i) => {
                        self.act(|st| crate::ops::rule_rm(st, &h, g, i))?;
                        self.sel[5] = step(self.sel[5], self.len(5), 0);
                    }
                }
            } else {
                self.msg = "отменено".into();
            }
            return Ok(false);
        }
        if self.filtering {
            match k.code {
                KeyCode::Esc => {
                    self.filter.clear();
                    self.filtering = false;
                }
                KeyCode::Enter => self.filtering = false,
                KeyCode::Backspace => {
                    self.filter.pop();
                }
                KeyCode::Char(c) => self.filter.push(c),
                _ => {}
            }
            self.sel[1] = 0;
            return Ok(false);
        }
        if self.help {
            self.help = false;
            return Ok(false);
        }
        let s = self.screen;
        match k.code {
            KeyCode::Char('q') => return Ok(true),
            KeyCode::Char(d @ '1'..='6') => self.screen = d as usize - '1' as usize,
            KeyCode::Char('?') => self.help = true,
            KeyCode::Char('/') => {
                self.filtering = true;
                self.screen = 1;
            }
            KeyCode::Esc => {
                self.filter.clear();
                self.screen = 0;
            }
            KeyCode::Char('j') => self.sel[s] = step(self.sel[s], self.len(s), 1),
            KeyCode::Char('k') => self.sel[s] = step(self.sel[s], self.len(s), -1),
            KeyCode::Char('p') => self.start("plan", &["plan"]),
            KeyCode::Char('a') => self.confirm = Some(Confirm::Apply),
            KeyCode::Char('r') => self.start("collect", &["collect"]),
            KeyCode::Char('t') => self.start("probe --matrix", &["probe", "--matrix"]),
            KeyCode::Char('V') => {
                let errs = validate::validate(&self.st);
                self.msg = if errs.is_empty() {
                    "✓ состояние валидно".into()
                } else {
                    format!("✗ {}", errs.join("; "))
                };
            }
            KeyCode::Enter if s == 0 => {
                self.sel[2] = self.sel[0];
                self.screen = 2;
            }
            KeyCode::Enter if s == 1 => self.qr += 1,
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
                    self.msg = format!("удалить пользователя {n}? y — да");
                    self.confirm = Some(Confirm::UserRm(n));
                }
            }
            KeyCode::Char('h') if s == 2 => self.link_sel = self.link_sel.saturating_sub(1),
            KeyCode::Char('l') if s == 2 => self.link_sel += 1,
            KeyCode::Char('o') if s == 2 => {
                if let Some((from, to)) = self.host_link() {
                    self.open(link_form(&self.st, &from, &to));
                }
            }
            KeyCode::Char('N') if s == 2 || s == 4 => self.open(link_form(&self.st, "", "")),
            KeyCode::Char('h') if s == 5 => {
                let n = self.entries().len();
                self.entry_sel = (self.entry_sel + 1) % n.max(1);
                self.sel[5] = 0;
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
                    self.msg = format!("удалить правило #{i}? y — да");
                    self.confirm = Some(Confirm::RuleRm(h, g, i));
                }
            }
            KeyCode::Char(c @ ('J' | 'K')) if s == 5 => {
                if let Some((h, g, i)) = self.rule_at() {
                    let to = if c == 'J' { i + 1 } else { i.saturating_sub(1) };
                    if to >= 1
                        && self
                            .commit(|st| crate::ops::rule_move(st, &h, g, i, to))
                            .is_ok()
                    {
                        self.sel[5] = step(self.sel[5], self.len(5), if c == 'J' { 1 } else { -1 });
                    }
                }
            }
            _ => {}
        }
        Ok(false)
    }

    // ---------- рендер ----------

    fn draw(&mut self, f: &mut Frame) {
        let [tabs, body, bar] = Layout::vertical([
            Constraint::Length(1),
            Constraint::Min(1),
            Constraint::Length(1),
        ])
        .areas(f.area());
        f.render_widget(
            Tabs::new(SCREENS)
                .select(self.screen)
                .highlight_style(Style::new().bold().yellow()),
            tabs,
        );
        match self.screen {
            0 => self.overview(f, body),
            1 => self.users(f, body),
            2 => self.host(f, body),
            3 => self.plan(f, body),
            4 => self.topology(f, body),
            _ => self.routing(f, body),
        }
        let bar_text = if self.filtering {
            format!("/ {}_", self.filter)
        } else {
            format!("{} · {}", self.msg, HELP[self.screen])
        };
        f.render_widget(Line::from(bar_text).dark_gray(), bar);
        if self.help {
            let a = center(body, 90, 9);
            f.render_widget(Clear, a);
            f.render_widget(
                para(
                    "Клавиши",
                    vec![
                        Line::from(
                            "везде: 1–6 экраны · j/k · Enter · Esc · / фильтр · ? помощь · q выход",
                        ),
                        Line::from(
                            "p план · a применить · r обновить метрики · t матрица · V проверка",
                        ),
                        Line::from(""),
                        Line::from(format!("этот экран: {}", HELP[self.screen])),
                        Line::from(""),
                        Line::from(
                            "зелёный в норме · жёлтый внимание · красный сломано · серый неактивно",
                        ),
                    ],
                ),
                a,
            );
        }
        if let Some(form) = &self.form {
            let (pv, pv_rows): (Vec<Line>, Vec<Row>) = match &self.preview {
                Some(Ok((msg, rows))) => (
                    vec![Line::from(format!("коммит: {msg}")).green()],
                    rows.clone(),
                ),
                Some(Err(e)) => (vec![Line::from(format!("✗ {e}")).red()], vec![]),
                None => (vec![], vec![]),
            };
            let a = center(
                body,
                100,
                form.fields.len() as u16 + 6 + pv_rows.len().min(10) as u16,
            );
            f.render_widget(Clear, a);
            let mut lines: Vec<Line> = form
                .fields
                .iter()
                .enumerate()
                .map(|(i, (l, v))| {
                    let s = format!("{l:>24}: {v}{}", if i == form.cur { "▏" } else { "" });
                    if i == form.cur {
                        Line::from(s).bold()
                    } else {
                        Line::from(s)
                    }
                })
                .collect();
            lines.push(Line::from(""));
            lines.push(match &form.error {
                Some(e) => Line::from(format!("✗ {e}")).red(),
                None => {
                    Line::from("Enter сохранить и закоммитить · Esc отмена · Tab поле").dark_gray()
                }
            });
            lines.extend(pv);
            for r in pv_rows.iter().take(10) {
                lines.push(
                    Line::from(format!(
                        "  {} {} {} {} {}",
                        r.cells[0], r.cells[3], r.cells[1], r.cells[2], r.cells[4]
                    ))
                    .fg(color(r.level)),
                );
            }
            f.render_widget(para(&form.title, lines), a);
        }
        if let Some(c) = &self.confirm {
            let a = center(body, 60, 3);
            f.render_widget(Clear, a);
            let q = match c {
                Confirm::Apply => "применить состояние на хосты? y — да".to_string(),
                _ => self.msg.clone(),
            };
            f.render_widget(para("Подтверждение", vec![Line::from(q).yellow()]), a);
        }
    }

    fn overview(&mut self, f: &mut Frame, area: Rect) {
        let rows = host_rows(&self.st, &self.status, self.head.as_deref());
        let links = (self.st.links.len() + self.st.entries().count()) as u16;
        let [top, hosts, lnk, sum] = Layout::vertical([
            Constraint::Length(1),
            Constraint::Length(rows.len() as u16 + 3),
            Constraint::Length(links + 2),
            Constraint::Min(3),
        ])
        .areas(area);
        let collected = self
            .status
            .hosts
            .values()
            .filter_map(|h| h.last.as_ref().map(|l| l.ts))
            .max();
        f.render_widget(
            Line::from(format!(
                "репо {} · HEAD {} · дерево {} · последний apply {} · метрики {}",
                self.branch,
                self.head.as_deref().unwrap_or("—"),
                if self.clean {
                    "чистое"
                } else {
                    "не закоммичено"
                },
                self.last_apply
                    .as_deref()
                    .map(|t| t.trim_start_matches("applied/"))
                    .unwrap_or("—"),
                collected
                    .map(|t| ago(t, self.now))
                    .unwrap_or("не собирались (r)".into())
            )),
            top,
        );
        self.sel[0] = step(self.sel[0], rows.len(), 0);
        f.render_stateful_widget(
            table(&HOST_HEADER, &rows, "Хосты".into()),
            hosts,
            &mut TableState::default().with_selected(self.sel[0]),
        );
        let mut lines: Vec<Line> = self
            .st
            .links
            .iter()
            .map(|l| {
                let protos: Vec<String> = l
                    .protocols
                    .iter()
                    .map(|p| {
                        format!(
                            "{}{}{}",
                            p.name(),
                            link_ms(&self.status, &l.from, &l.to, *p)
                                .map(|m| format!(" {m}"))
                                .unwrap_or_default(),
                            if l.active == Some(*p) {
                                " активный"
                            } else {
                                ""
                            }
                        )
                    })
                    .collect();
                Line::from(format!(
                    "{} → {} · {} · {}/сутки",
                    l.from,
                    l.to,
                    protos.join(" · "),
                    gb(link_traffic(&self.status, &l.from, l))
                ))
            })
            .collect();
        for (n, r) in &self.st.routing {
            if self.st.hosts.get(n).is_some_and(|h| h.role == Role::Entry) {
                lines
                    .push(Line::from(format!("политика выхода {n}: {}", exit_text(r))).dark_gray());
            }
        }
        f.render_widget(para("Линки", lines), lnk);
        let users = &self.status.users;
        let active = users.values().filter(|u| u.enabled).count();
        let soon = self
            .st
            .users
            .iter()
            .filter(|(n, u)| user_level(&users[*n], u.expires.as_deref(), self.now) == Level::Warn)
            .count();
        let day: u64 = users.values().map(|u| u.day.up + u.day.down).sum();
        let (direct, exit) = self
            .st
            .entries()
            .map(|(n, _)| traffic_split(&self.st, &self.status, n))
            .fold((0, 0), |a, b| (a.0 + b.0, a.1 + b.1));
        let attention: Vec<String> = self
            .status
            .hosts
            .iter()
            .filter_map(|(n, h)| {
                let issues = host_issues(h, self.head.as_deref());
                (!issues.is_empty()).then(|| {
                    format!(
                        "{n}: {}",
                        issues
                            .into_iter()
                            .map(|(_, s)| s)
                            .collect::<Vec<_>>()
                            .join(", ")
                    )
                })
            })
            .collect();
        f.render_widget(
            para(
                "Сводка",
                vec![
                    Line::from(format!(
                        "пользователи {} · активных {active} · истекает {soon} за {EXPIRES_WARN_DAYS} дн",
                        users.len()
                    )),
                    Line::from(format!(
                        "трафик за сутки: пользователи {} · напрямую {} · через выход {}",
                        gb(day),
                        gb(direct),
                        gb(exit)
                    )),
                    Line::from(if attention.is_empty() {
                        "требует внимания: ничего".to_string()
                    } else {
                        format!("требует внимания: {}", attention.join(" · "))
                    })
                    .yellow(),
                ],
            ),
            sum,
        );
    }

    fn users(&mut self, f: &mut Frame, area: Rect) {
        let [left, right] =
            Layout::horizontal([Constraint::Percentage(55), Constraint::Percentage(45)])
                .areas(area);
        let all = user_rows(&self.st, &self.status, self.now);
        let vis = filter(&all, &self.filter);
        let rows: Vec<Row> = vis.iter().map(|i| all[*i].clone()).collect();
        self.sel[1] = step(self.sel[1], rows.len(), 0);
        f.render_stateful_widget(
            table(
                &USER_HEADER,
                &rows,
                format!(
                    "{} пользователей · фильтр: {}",
                    all.len(),
                    if self.filter.is_empty() {
                        "/"
                    } else {
                        &self.filter
                    }
                ),
            ),
            left,
            &mut TableState::default().with_selected(self.sel[1]),
        );
        let Some(name) = self.user_name() else {
            f.render_widget(para("Карточка", vec![]), right);
            return;
        };
        let u = &self.st.users[&name];
        let mut lines = vec![
            Line::from(format!(
                "вход {}",
                self.st.entry_of(u).unwrap_or("—".into())
            )),
            Line::from(format!(
                "протоколы {}",
                u.protocols
                    .iter()
                    .map(|p| p.name())
                    .collect::<Vec<_>>()
                    .join(" · ")
            )),
            Line::from(format!("заметка {}", u.note.as_deref().unwrap_or("—"))),
            Line::from(format!(
                "awg-пир {}",
                u.awg
                    .as_ref()
                    .map(|a| format!("{} {}", a.mode.name(), a.ip))
                    .unwrap_or("нет".into())
            )),
            Line::from(""),
        ];
        match user_links(&self.st, &name) {
            Err(e) => lines.push(Line::from(format!("ссылки: {e:#}")).red()),
            Ok(links) if links.is_empty() => lines.push(Line::from("ссылок нет").dark_gray()),
            Ok(links) => {
                let cur = self.qr % links.len();
                for (i, (l, url)) in links.iter().enumerate() {
                    let line = Line::from(format!("{l}: {url}"));
                    lines.push(if i == cur { line.bold() } else { line });
                }
                lines.push(Line::from(""));
                lines.extend(
                    crate::render::subs::qr(&links[cur].1)
                        .lines()
                        .map(|l| Line::from(l.to_string())),
                );
            }
        }
        f.render_widget(
            Paragraph::new(lines).block(Block::bordered().title(name)),
            right,
        );
    }

    fn host(&mut self, f: &mut Frame, area: Rect) {
        let names = self.host_names();
        self.sel[2] = step(self.sel[2], names.len(), 0);
        let Some(name) = names.get(self.sel[2]) else {
            return;
        };
        let info = host_info(&self.st, &self.status, name, self.head.as_deref(), self.now);
        let [left, right] =
            Layout::horizontal([Constraint::Percentage(50), Constraint::Percentage(50)])
                .areas(area);
        let [a, b] = Layout::vertical([Constraint::Length(8), Constraint::Min(3)]).areas(left);
        let [c, v, d, e] = Layout::vertical([
            Constraint::Length(4),
            Constraint::Length(3),
            Constraint::Length(info[4].1.len() as u16 + 2),
            Constraint::Min(3),
        ])
        .areas(right);
        for ((title, lines), area) in info.iter().zip([a, b, c, v, d]) {
            let lines = lines.iter().map(|l| Line::from(l.as_str())).collect();
            f.render_widget(
                para(title, lines).block(Block::bordered().title(format!("{name} · {title}"))),
                area,
            );
        }
        let links = host_links(&self.st, &self.status, name);
        self.link_sel = step(self.link_sel, links.len(), 0);
        let lines = links
            .iter()
            .enumerate()
            .map(|(i, l)| {
                if i == self.link_sel {
                    Line::from(l.as_str()).reversed()
                } else {
                    Line::from(l.as_str())
                }
            })
            .collect();
        f.render_widget(para("Линки (h/l, o)", lines), e);
    }

    fn plan(&mut self, f: &mut Frame, area: Rect) {
        let title = match (self.job.is_some(), self.job_ok) {
            _ if self.job_name.is_empty() => "p план · a применить · r метрики · t матрица".into(),
            (true, _) => format!("{} · выполняется", self.job_name),
            (_, Some(true)) => format!("{} · готово", self.job_name),
            _ => format!("{} · ошибка", self.job_name),
        };
        let guards = if self.branch == "main" && self.clean {
            Line::from("✓ ветка main, дерево чистое — apply пройдёт guard'ы").green()
        } else {
            Line::from(format!(
                "! ветка {}, дерево {} — apply откажет",
                self.branch,
                if self.clean {
                    "чистое"
                } else {
                    "не закоммичено"
                }
            ))
            .yellow()
        };
        let [g, area] = Layout::vertical([Constraint::Length(1), Constraint::Min(3)]).areas(area);
        f.render_widget(guards, g);
        let h = area.height.saturating_sub(2) as usize;
        let scroll = self.log.len().saturating_sub(h) as u16;
        let lines: Vec<Line> = self.log.iter().map(|l| Line::from(l.as_str())).collect();
        f.render_widget(
            Paragraph::new(lines)
                .block(Block::bordered().title(title))
                .scroll((scroll, 0)),
            area,
        );
    }

    fn topology(&mut self, f: &mut Frame, area: Rect) {
        let errs = validate::validate(&self.st);
        let check = if errs.is_empty() {
            Line::from(format!(
                "статическая проверка ✓ · {} хостов, {} линков · via без циклов, выходы, ключи, подсети",
                self.st.hosts.len(),
                self.st.links.len()
            ))
            .green()
        } else {
            Line::from(format!(
                "статическая проверка ✗ {} ошибок · V подробности",
                errs.len()
            ))
            .red()
        };
        let [c, area] = Layout::vertical([Constraint::Length(1), Constraint::Min(3)]).areas(area);
        f.render_widget(check, c);
        let (cols, rows) = matrix_rows(&self.status);
        if rows.is_empty() {
            f.render_widget(
                para(
                    "Топология",
                    vec![
                        Line::from("матрица ещё не прогонялась · t — прогнать матрицу").dark_gray(),
                    ],
                ),
                area,
            );
            return;
        }
        let header = TRow::new(["ребро"].into_iter().chain(cols.iter().copied())).bold();
        let trows = rows.iter().map(|(edge, cells)| {
            TRow::new(
                [Cell::from(edge.as_str())].into_iter().chain(
                    cells
                        .iter()
                        .map(|(t, l)| Cell::from(t.as_str()).fg(color(*l))),
                ),
            )
        });
        let mut w = vec![Constraint::Length(
            rows.iter()
                .map(|(e, _)| e.chars().count())
                .max()
                .unwrap_or(5) as u16,
        )];
        w.extend(std::iter::repeat_n(Constraint::Length(16), cols.len()));
        f.render_widget(
            Table::new(trows, w)
                .header(header)
                .block(Block::bordered().title(format!(
                "матрица · прогон {}",
                self.status
                    .matrix_ts
                    .map(|t| ago(t, self.now))
                    .unwrap_or("—".into())
            ))),
            area,
        );
    }

    fn routing(&mut self, f: &mut Frame, area: Rect) {
        let Some(host) = self.routing_host() else {
            f.render_widget(para("Маршрутизация", vec![Line::from("нет входов")]), area);
            return;
        };
        let rules = rule_rows(&self.st, &host);
        self.sel[5] = step(self.sel[5], rules.len(), 0);
        let sets = self.st.sets.len() as u16;
        let [top, dns, bottom] = Layout::vertical([
            Constraint::Min(3),
            Constraint::Length(5),
            Constraint::Length(sets + 2),
        ])
        .areas(area);
        let mut trows = vec![];
        let mut selected = 0;
        let mut last = None;
        for (i, (g, _, r)) in rules.iter().enumerate() {
            if last != Some(*g) {
                trows.push(TRow::new([
                    Cell::from(""),
                    Cell::from(group_name(*g)).bold().cyan(),
                ]));
                last = Some(*g);
            }
            if i == self.sel[5] {
                selected = trows.len();
            }
            trows
                .push(TRow::new(r.cells.iter().map(|c| Cell::from(c.as_str()))).fg(color(r.level)));
        }
        let plain: Vec<Row> = rules.iter().map(|(_, _, r)| r.clone()).collect();
        let mut w = widths(&["#", "условие", "действие", "", "заметка"], &plain);
        w[1] = Constraint::Length(match w[1] {
            Constraint::Length(n) => n.max(12),
            _ => 12,
        });
        f.render_stateful_widget(
            Table::new(trows, w)
                .row_highlight_style(Style::new().reversed())
                .block(Block::bordered().title(format!(
                    "{host} · routing.yaml · правила сверху вниз, первое совпавшее (h — другой вход)"
                ))),
            top,
            &mut TableState::default().with_selected(selected),
        );
        if let Some(r) = self.st.routing.get(&host) {
            f.render_widget(
                para(
                    "Если ничего не совпало · DNS",
                    vec![
                        Line::from("умолчание движка (direct), не правило · трафик awg попадает сюда через tproxy")
                            .dark_gray(),
                        Line::from(dns_text(r)),
                        Line::from(format!("выход: {}", exit_text(r))),
                    ],
                ),
                dns,
            );
        }
        let lines = self
            .st
            .sets
            .keys()
            .map(|n| Line::from(set_text(&self.st, n)))
            .collect();
        f.render_widget(para("Наборы", lines), bottom);
    }
}
