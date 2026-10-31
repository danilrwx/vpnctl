//! Drawing of the six screens and the overlays. Data comes from `model` and `App`.
use super::model::*;
use super::ui::*;
use super::{App, SCREENS, VERSION};
use crate::metrics::LAPTOP;
use crate::state::*;
use ratatui::Frame;
use ratatui::layout::{Alignment, Constraint, Layout, Rect};
use ratatui::style::{Color, Style, Stylize};
use ratatui::text::{Line, Span};
use ratatui::widgets::{
    Cell, Clear, Paragraph, Row as TRow, Sparkline, SparklineBar, TableState, Wrap,
};

const FOOTER: [&[(&str, &str)]; 6] = [
    &[
        ("j/k", "rows"),
        ("Enter", "host card"),
        ("p", "plan"),
        ("o", "active exit"),
        ("a", "apply"),
        ("r", "refresh metrics"),
        ("B", "bootstrap"),
        ("?", "help"),
    ],
    &[
        ("n", "add"),
        ("/", "filter"),
        ("s", "sort"),
        ("e", "edit"),
        ("x", "disable"),
        ("D", "delete"),
        ("y", "copy"),
        ("Q", "big QR"),
        ("T", "tg QR"),
        ("f", "files"),
    ],
    &[
        ("x", "toggle inbound"),
        ("o", "exit"),
        ("U", "update OS"),
        ("R", "reboot"),
        ("l", "journal"),
        ("B", "bootstrap"),
        ("h/→", "host"),
    ],
    &[
        ("Enter", "apply all"),
        ("s", "selected host only"),
        ("d", "file diff"),
        ("m", "drift → repo"),
        ("p", "plan again"),
    ],
    &[
        ("t", "run matrix now"),
        ("Enter", "history protocol"),
        ("o", "link: protocols and policy"),
        ("V", "validate"),
        ("N", "new link"),
    ],
    &[
        ("n", "rule"),
        ("e", "edit"),
        ("J/K", "move"),
        ("x", "toggle"),
        ("D", "delete"),
        ("v", "engine JSON"),
        ("t", "trace address"),
        ("u", "user"),
        ("h", "host"),
    ],
];

fn node(s: &str) -> &str {
    if s == LAPTOP { "laptop" } else { s }
}

fn fit(s: &str, w: usize) -> String {
    if s.chars().count() <= w {
        s.to_string()
    } else {
        let mut t: String = s.chars().take(w.saturating_sub(1)).collect();
        t.push('…');
        t
    }
}

/// `applied/2026-10-30T17-07-11` → `30.10 17:07`.
fn tag_date(t: &str) -> String {
    let t = t.trim_start_matches("applied/");
    match (t.get(8..10), t.get(5..7), t.get(11..13), t.get(14..16)) {
        (Some(d), Some(m), Some(h), Some(mi)) => format!("{d}.{m} {h}:{mi}"),
        _ => t.to_string(),
    }
}

fn big_qr(text: &str) -> String {
    match qrcode::QrCode::new(text) {
        Ok(c) => c
            .render::<char>()
            .quiet_zone(true)
            .module_dimensions(2, 1)
            .build(),
        Err(_) => "(too long for a QR code)".into(),
    }
}

impl App {
    pub(super) fn draw(&mut self, f: &mut Frame) {
        let sub = u16::from(self.screen == 0);
        let [hdr, subh, body, foot] = Layout::vertical([
            Constraint::Length(1),
            Constraint::Length(sub),
            Constraint::Min(1),
            Constraint::Length(1),
        ])
        .areas(f.area());
        self.header(f, hdr);
        if sub == 1 {
            self.subheader(f, subh);
        }
        match self.screen {
            0 => self.overview(f, body),
            1 => self.users(f, body),
            2 => self.host(f, body),
            3 => self.plan(f, body),
            4 => self.topology(f, body),
            _ => self.routing(f, body),
        }
        self.footer(f, foot);
        self.overlays(f, body);
    }

    // ---------- chrome ----------

    fn header(&self, f: &mut Frame, area: Rect) {
        let mut v = vec![
            Span::styled(format!("vpnctl {VERSION}"), Style::new().bold()),
            Span::raw("  "),
        ];
        for (i, s) in SCREENS.iter().enumerate() {
            v.push(if i == self.screen {
                Span::styled(
                    format!(" {s} "),
                    Style::new().fg(Color::White).bg(BLUE).bold(),
                )
            } else {
                dim(format!(" {s} "))
            });
            v.push(Span::raw(" "));
        }
        f.render_widget(Line::from(v), area);
        let collected = self
            .status
            .hosts
            .values()
            .filter_map(|h| h.last.as_ref().map(|l| l.ts))
            .max();
        let ctx = match self.screen {
            0 => format!(
                "{} · updated {}",
                date_time(self.now),
                collected
                    .map(|t| ago(t, self.now))
                    .unwrap_or("never".into())
            ),
            1 => format!(
                "{} users · filter: /{}{}",
                self.st.users.len(),
                self.filter,
                if self.filtering { "_" } else { "" }
            ),
            2 => self
                .host_name()
                .map(|h| {
                    format!(
                        "{h} · {} · {}",
                        role_text(&self.st, &h),
                        self.st.hosts[&h].domain.as_deref().unwrap_or("by IP")
                    )
                })
                .unwrap_or_default(),
            3 => format!(
                "plan for {} @ {} · render {}",
                self.branch,
                self.head.as_deref().unwrap_or("—"),
                if self.reproducible { "✓" } else { "✗" }
            ),
            4 => format!(
                "matrix · run {}",
                self.status
                    .matrix_ts
                    .map(|t| ago(t, self.now))
                    .unwrap_or("never".into())
            ),
            _ => format!(
                "host {} · routing.yaml · rules top-down",
                self.routing_host().unwrap_or("—".into())
            ),
        };
        f.render_widget(
            Paragraph::new(Line::from(dim(ctx))).alignment(Alignment::Right),
            area,
        );
    }

    fn subheader(&self, f: &mut Frame, area: Rect) {
        let v = vec![
            dim("repo "),
            Span::raw(self.branch.clone()),
            dim(" · tree "),
            if self.clean {
                Span::styled("clean", Style::new().fg(Color::Green))
            } else {
                Span::styled("dirty", Style::new().fg(Color::Yellow))
            },
            dim(" · HEAD "),
            Span::raw(self.head.clone().unwrap_or("—".into())),
            dim(format!(
                " «{}» · {} · last apply ",
                fit(&self.head_msg, 24),
                ago(self.head_ts, self.now)
            )),
            Span::raw(
                self.last_apply
                    .as_deref()
                    .map(tag_date)
                    .unwrap_or("—".into()),
            ),
            dim(" · render "),
            if self.reproducible {
                Span::styled("✓", Style::new().fg(Color::Green))
            } else {
                Span::styled("✗ not reproducible", Style::new().fg(Color::Red))
            },
        ];
        f.render_widget(Line::from(v), area);
    }

    fn footer(&self, f: &mut Frame, area: Rect) {
        f.render_widget(keys(FOOTER[self.screen]), area);
        let right = match self.screen {
            1 => Line::from(Span::styled(
                if self.clean {
                    "uncommitted changes: 0".to_string()
                } else {
                    "uncommitted changes".into()
                },
                Style::new().fg(Color::Yellow),
            )),
            2 => Line::from(dim("x o — commit · U R — no commit")),
            3 if self.branch != "main" || !self.clean => Line::from(Span::styled(
                "not on main or dirty tree — refused",
                Style::new().fg(Color::Red),
            )),
            5 => Line::from(Span::styled(
                format!("changed: {} · p plan", self.since_apply),
                Style::new().fg(Color::Yellow),
            )),
            _ => keys(&[("q", "quit")]),
        };
        let msg = if self.msg.is_empty() || self.msg == "? help" {
            right
        } else {
            Line::from(Span::styled(
                self.msg.clone(),
                if self.msg.starts_with('✗') {
                    Style::new().fg(Color::Red)
                } else {
                    Style::new().fg(Color::Yellow)
                },
            ))
        };
        f.render_widget(Paragraph::new(msg).alignment(Alignment::Right), area);
    }

    // ---------- 1 overview ----------

    fn overview(&mut self, f: &mut Frame, area: Rect) {
        let behind = |m: &str| self.behind.get(m).copied();
        let rows = host_rows(&self.st, &self.status, self.head.as_deref(), &behind);
        let [top, bottom] = Layout::vertical([
            Constraint::Length(rows.len() as u16 + 3),
            Constraint::Min(5),
        ])
        .areas(area);
        self.sel[0] = step(self.sel[0], rows.len(), 0);
        let cells: Vec<Vec<String>> = rows.iter().map(|r| r.cells.clone()).collect();
        let trows = rows.iter().map(|r| {
            let l = r.level;
            let tint = |i: usize| (3..=8).contains(&i);
            TRow::new(r.cells.iter().enumerate().map(|(i, c)| {
                let lvl = match i {
                    0 | 1 => Level::Ok,
                    2 if c.starts_with('—') => Level::Off,
                    2 => Level::Ok,
                    4 if c.contains("= HEAD") => Level::Ok,
                    4 if c.contains('−') || c.contains('≠') => Level::Warn,
                    4 => Level::Off,
                    5 => Level::Ok,
                    7 => self
                        .status
                        .hosts
                        .get(&r.cells[0])
                        .and_then(|h| h.cert_days)
                        .map_or(Level::Off, |d| {
                            if d < CERT_WARN_DAYS {
                                Level::Warn
                            } else {
                                Level::Ok
                            }
                        }),
                    8 if c.contains('↻') => Level::Warn,
                    8 if c == "0" => Level::Ok,
                    8 => Level::Off,
                    _ if c.starts_with('●') => Level::Ok,
                    _ if c.starts_with('✗') => Level::Bad,
                    _ => Level::Off,
                };
                let lvl = if l == Level::Off && i > 0 {
                    Level::Off
                } else {
                    lvl
                };
                let c = cell(c, lvl, tint(i));
                if i == 0 { c.bold() } else { c }
            }))
        });
        f.render_stateful_widget(
            table(&HOST_HEADER, trows.collect(), widths(&HOST_HEADER, &cells)).block(panel()),
            top,
            &mut TableState::default().with_selected(self.sel[0]),
        );

        let [left, right] =
            Layout::horizontal([Constraint::Percentage(50), Constraint::Percentage(50)])
                .spacing(1)
                .areas(bottom);
        let kw = 22;
        let mut lines = vec![];
        for l in &self.st.links {
            let mut v = vec![dim(format!("{:<kw$}", format!("{} → {}", l.from, l.to)))];
            for (i, (t, lv)) in link_parts(&self.status, l).iter().enumerate() {
                if i > 0 {
                    v.push(dim(" · "));
                }
                v.push(lvl(t.clone(), *lv));
            }
            lines.push(Line::from(v));
        }
        for (n, r) in &self.st.routing {
            if self.st.hosts.get(n).is_some_and(|h| h.role == Role::Entry) {
                lines.push(Line::from(vec![
                    dim(format!("{:<kw$}", format!("exit policy {n}"))),
                    Span::raw(exit_text(r)),
                ]));
            }
        }
        lines.push(Line::from(""));
        lines.push(Line::from(dim(format!(
            "Last matrix run {}",
            self.status
                .matrix_ts
                .map(|t| ago(t, self.now))
                .unwrap_or("never · t to run".into())
        ))));
        f.render_widget(titled("Links between nodes", lines), left);

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
        let kw = 24;
        let mut lines = vec![Line::from(vec![
            dim(format!("{:<kw$}", "users")),
            Span::raw(format!("{} · active {active}", users.len())),
            if soon > 0 {
                Span::styled(
                    format!(" · {soon} expiring in {EXPIRES_WARN_DAYS} d"),
                    Style::new().fg(Color::Yellow),
                )
            } else {
                dim(" · none expiring")
            },
        ])];
        lines.push(Line::from(vec![
            dim(format!("{:<kw$}", "traffic per day")),
            Span::raw(format!(
                "{} · direct {} · via exit {}",
                gb(day),
                gb(direct),
                gb(exit)
            )),
        ]));
        for (n, _) in self.st.entries() {
            let h = &self.status.hosts[n];
            let hist = self.db.probe_history(n, self.now - DAY).unwrap_or_default();
            let ok = hist.iter().filter(|(_, c, _)| *c == Some(204)).count();
            let v = match h.last.as_ref().map(|l| (l.m.probe_code, l.m.probe_ms)) {
                Some((Some(c), ms)) => Span::styled(
                    format!(
                        "{} {c} in {:.2} s · {ok} of {} today",
                        if c == 204 { "●" } else { "✗" },
                        ms.unwrap_or(0) as f64 / 1000.0,
                        hist.len()
                    ),
                    Style::new().fg(if c == 204 { Color::Green } else { Color::Red }),
                ),
                Some((None, _)) => {
                    Span::styled("✗ no answer".to_string(), Style::new().fg(Color::Red))
                }
                None => dim("— not collected yet"),
            };
            lines.push(Line::from(vec![
                dim(format!("{:<kw$}", format!("chain probe from {n}"))),
                v,
            ]));
        }
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
        lines.push(Line::from(vec![
            dim(format!("{:<kw$}", "needs attention")),
            if attention.is_empty() {
                Span::styled("nothing", Style::new().fg(Color::Green))
            } else {
                Span::styled(attention.join(" · "), Style::new().fg(Color::Yellow))
            },
        ]));
        f.render_widget(titled("Summary", lines), right);
    }

    // ---------- 2 users ----------

    fn users(&mut self, f: &mut Frame, area: Rect) {
        let [left, right] =
            Layout::horizontal([Constraint::Percentage(60), Constraint::Percentage(40)])
                .spacing(1)
                .areas(area);
        let table_rows = self.user_table();
        self.sel[1] = step(self.sel[1], table_rows.len(), 0);
        let visible = left.height.saturating_sub(4) as usize;
        let more = table_rows.len().saturating_sub(visible.max(1));
        let [tbl, note] =
            Layout::vertical([Constraint::Min(3), Constraint::Length(u16::from(more > 0))])
                .areas(left);
        let cells: Vec<Vec<String>> = table_rows.iter().map(|(_, r)| r.cells.clone()).collect();
        let trows = table_rows.iter().map(|(_, r)| {
            TRow::new(r.cells.iter().enumerate().map(|(i, c)| {
                let lvl = match (i, r.level) {
                    (_, Level::Off) => Level::Off,
                    (2, l) => l,
                    (3, Level::Warn) | (3, Level::Bad) => r.level,
                    (3, _) if c.starts_with("no") => Level::Off,
                    (7, _) => Level::Off,
                    _ => Level::Ok,
                };
                let c = cell(c, lvl, i == 2 || i == 3);
                if i == 0 && r.level != Level::Off {
                    c.bold()
                } else {
                    c
                }
            }))
        });
        f.render_stateful_widget(
            table(&USER_HEADER, trows.collect(), widths(&USER_HEADER, &cells)).block(panel()),
            tbl,
            &mut TableState::default().with_selected(self.sel[1]),
        );
        if more > 0 {
            f.render_widget(Line::from(dim(format!("  … {more} more"))), note);
        }

        let Some(name) = self.user_name() else {
            f.render_widget(titled("no users · n to add", vec![]), right);
            return;
        };
        let u = &self.st.users[&name];
        let added = self
            .added
            .get(&name)
            .map(|(sha, date)| {
                format!(
                    " · added {}, commit {sha}",
                    date_epoch(date).map(short_date).unwrap_or(date.clone())
                )
            })
            .unwrap_or_default();
        let kw = 11;
        let entry = self.st.entry_of(u);
        let mut lines = kv(
            &[
                (
                    "modes".into(),
                    u.modes
                        .iter()
                        .map(|m| m.name())
                        .collect::<Vec<_>>()
                        .join(" "),
                    Level::Ok,
                ),
                (
                    "entry".into(),
                    match &entry {
                        Some(e) => format!(
                            "{e} · {}",
                            self.st
                                .hosts
                                .get(e)
                                .and_then(|h| h.domain.clone())
                                .unwrap_or("by IP".into())
                        ),
                        None => "— no entry".into(),
                    },
                    Level::Ok,
                ),
                (
                    "protocols".into(),
                    u.protocols
                        .iter()
                        .map(|p| p.name())
                        .collect::<Vec<_>>()
                        .join(" · "),
                    Level::Ok,
                ),
                (
                    "note".into(),
                    u.note.clone().unwrap_or("—".into()),
                    if u.note.is_some() {
                        Level::Ok
                    } else {
                        Level::Off
                    },
                ),
                (
                    "expiry".into(),
                    expiry_text(u.expires.as_deref(), self.now),
                    Level::Ok,
                ),
            ],
            kw,
        );
        lines.push(Line::from(""));
        match user_links(&self.st, &name) {
            Err(e) => lines.push(Line::from(Span::styled(
                format!("links: {e:#}"),
                Style::new().fg(Color::Red),
            ))),
            Ok(links) if links.is_empty() => lines.push(Line::from(dim("no links"))),
            Ok(links) => {
                let cur = self.qr % links.len();
                let w = right.width.saturating_sub(6) as usize;
                for (i, (l, url)) in links.iter().enumerate() {
                    if l.starts_with("Telegram") {
                        continue;
                    }
                    lines.push(Line::from(vec![
                        dim(format!("{l} (Karing / Happ)")),
                        if i == cur {
                            dim(" · in QR")
                        } else {
                            Span::raw("")
                        },
                    ]));
                    lines.push(code(&fit(url, w)));
                }
                lines.push(Line::from(""));
                let qr = if self.big_qr {
                    big_qr(&links[cur].1)
                } else {
                    crate::render::subs::qr(&links[cur].1)
                };
                lines.extend(
                    qr.lines()
                        .map(|l| Line::from(fit(l, w + 2)).fg(Color::White)),
                );
                lines.push(Line::from(dim(format!(
                    "QR of {} · Enter next · Q big · for the phone nearby",
                    links[cur].0
                ))));
                lines.push(Line::from(""));
                if let Some((_, t)) = links.iter().find(|(l, _)| l.starts_with("Telegram")) {
                    lines.push(Line::from(dim("Telegram proxy (MTProto, shared secret)")));
                    lines.push(code(&fit(t, w)));
                } else {
                    lines.push(Line::from(dim("Telegram proxy: off (e to enable)")));
                }
            }
        }
        lines.push(Line::from(""));
        for (k, d) in [
            ("y", "copy link · Q big QR · T QR for tg"),
            (
                "e",
                "edit (modes, protocols, expiry) · x disable · D delete",
            ),
            ("f", "profile files (awg.conf)"),
        ] {
            lines.push(Line::from(key(k, d)));
        }
        let mut p = titled(&format!("{name}{added}"), lines);
        p = p.wrap(Wrap { trim: false });
        f.render_widget(p, right);
    }

    // ---------- 3 host ----------

    fn host(&mut self, f: &mut Frame, area: Rect) {
        let names = self.host_names();
        self.host_sel = step(self.host_sel, names.len(), 0);
        let Some(name) = names.get(self.host_sel).cloned() else {
            f.render_widget(titled("no hosts", vec![]), area);
            return;
        };
        let state = host_state(&self.st, &self.status, &name, self.head.as_deref());
        let machine = host_machine(&self.status, &name, self.now);
        let domain = host_domain(
            &self.st,
            &self.status,
            &name,
            self.resolved.get(&name).map(|v| v.as_slice()),
        );
        let h = state.len().max(machine.len()).max(domain.len()) as u16 + 3;
        let [top, bottom] =
            Layout::vertical([Constraint::Length(h), Constraint::Min(5)]).areas(area);
        let [a, b, c] = Layout::horizontal([
            Constraint::Percentage(33),
            Constraint::Percentage(34),
            Constraint::Percentage(33),
        ])
        .spacing(1)
        .areas(top);
        f.render_widget(titled("State", kv(&state, 12)), a);
        f.render_widget(titled("Machine", kv(&machine, 12)), b);
        f.render_widget(titled("Domain and certificate", kv(&domain, 12)), c);

        let [left, right] =
            Layout::horizontal([Constraint::Percentage(50), Constraint::Percentage(50)])
                .spacing(1)
                .areas(bottom);
        let (inb, exits) = self.host_rows3();
        self.sel[2] = step(self.sel[2], inb.len() + exits.len(), 0);
        let mut lines = vec![];
        let row3 = |(n, mid, status, l): &Row3, selected: bool| {
            let mut line = Line::from(vec![
                Span::styled(
                    format!("{n:<16}"),
                    Style::new().fg(if *l == Level::Off { DIM } else { Color::White }),
                ),
                Span::raw(format!("{mid:<16}")),
                lvl(status.clone(), *l),
            ]);
            if selected {
                line = line.bg(BG_SEL);
            }
            line
        };
        for (i, r) in inb.iter().enumerate() {
            lines.push(row3(r, i == self.sel[2]));
        }
        if !exits.is_empty() {
            lines.push(Line::from(dim(
                "─".repeat(left.width.saturating_sub(4) as usize)
            )));
            for (i, r) in exits.iter().enumerate() {
                lines.push(row3(r, i + inb.len() == self.sel[2]));
            }
        }
        f.render_widget(titled("Inbounds and exits", lines), left);

        let [probe, journal] =
            Layout::vertical([Constraint::Length(11), Constraint::Min(3)]).areas(right);
        let exit = preferred_exit(&self.st, &name);
        let hist = self
            .db
            .probe_history(&name, self.now - DAY)
            .unwrap_or_default();
        let inner = probe.inner(ratatui::layout::Margin::new(2, 1));
        f.render_widget(
            titled(
                &format!(
                    "Chain probe over 24 h · generate_204{}",
                    exit.map(|e| format!(" via {e}")).unwrap_or_default()
                ),
                vec![],
            ),
            probe,
        );
        if hist.is_empty() {
            f.render_widget(
                Line::from(dim("no probes in the last 24 h · r to collect")),
                Rect::new(inner.x, inner.y + 2, inner.width, 1),
            );
        } else {
            let w = inner.width as usize;
            let data: Vec<_> = hist.iter().rev().take(w).rev().collect();
            let rep = (w / data.len().max(1)).clamp(1, 4);
            let max = data
                .iter()
                .filter_map(|(_, _, ms)| *ms)
                .max()
                .unwrap_or(1000)
                .max(1000);
            let bars: Vec<SparklineBar> = data
                .iter()
                .flat_map(|p| std::iter::repeat_n(p, rep))
                .map(|(_, code, ms)| {
                    let v = if *code == Some(204) { *ms } else { Some(max) };
                    SparklineBar::from(v).style(Some(Style::new().fg(match (code, ms) {
                        (Some(204), Some(m)) if *m > PROBE_SLOW_MS => Color::Yellow,
                        (Some(204), _) => Color::Green,
                        _ => Color::Red,
                    })))
                })
                .collect();
            f.render_widget(
                Sparkline::default().data(bars).max(max),
                Rect::new(inner.x, inner.y + 2, inner.width, 6),
            );
            let axis = Rect::new(inner.x, inner.y + 8, inner.width, 1);
            let left = format!("{:<10}", ago(data[0].0, self.now));
            let mid = format!(
                "0 — {:.1} s · >{:.1} s yellow · failed red",
                max as f64 / 1000.0,
                PROBE_SLOW_MS as f64 / 1000.0
            );
            let pad = (inner.width as usize).saturating_sub(left.len() + mid.chars().count() + 3);
            f.render_widget(
                Line::from(dim(format!("{left}{mid}{}now", " ".repeat(pad.max(1))))),
                axis,
            );
        }
        let unit = match self.st.hosts[&name].engine {
            Engine::SingBox => "sing-box",
            Engine::Xray => "xray",
        };
        let log = match self.host_log.get(&name) {
            Some(l) if l.is_empty() => vec![Line::from(dim("loading…"))],
            Some(l) => l.iter().map(|s| Line::from(dim(s.clone()))).collect(),
            None => vec![Line::from(dim("press l to load the log over ssh"))],
        };
        f.render_widget(
            titled(&format!("journalctl -u {unit} · tail"), log),
            journal,
        );
    }

    // ---------- 4 plan ----------

    fn plan(&mut self, f: &mut Frame, area: Rect) {
        let [left, right] =
            Layout::horizontal([Constraint::Percentage(50), Constraint::Percentage(50)])
                .spacing(1)
                .areas(area);
        let rows = self.plan_rows();
        self.plan_sel = step(self.plan_sel, rows.len(), 0);
        let (hosts, files) = match &self.plan {
            Some(Ok(p)) => (
                p.plans.len(),
                p.plans.iter().map(|h| h.changes()).sum::<usize>(),
            ),
            _ => (0, 0),
        };
        let checks: Vec<(Level, String)> = match &self.plan {
            Some(Ok(p)) => plan_checks(
                &self.st,
                &self.status,
                &p.plans,
                &self.branch,
                self.clean,
                self.reproducible,
            ),
            _ => vec![],
        };
        let [what, chk] = Layout::vertical([
            Constraint::Min(5),
            Constraint::Length(checks.len() as u16 + 3),
        ])
        .areas(left);
        let mut lines = vec![];
        match &self.plan {
            None if self.plan_running() => lines.push(Line::from(dim("probing hosts over ssh…"))),
            None => lines.push(Line::from(dim(
                "p — build the plan: render, probe hosts, diff and drift",
            ))),
            Some(Err(e)) => lines.push(Line::from(Span::styled(
                format!("✗ {e}"),
                Style::new().fg(Color::Red),
            ))),
            Some(Ok(_)) => {
                for (i, r) in rows.iter().enumerate() {
                    let mut line = match r {
                        PlanRow::Host(t, l) => {
                            let (name, rest) = t.split_once(" · ").unwrap_or((t, ""));
                            Line::from(vec![
                                Span::styled(
                                    name.to_string(),
                                    Style::new().bold().fg(if *l == Level::Off {
                                        DIM
                                    } else {
                                        Color::White
                                    }),
                                ),
                                dim(format!(" · {rest}")),
                            ])
                        }
                        PlanRow::File {
                            path,
                            status,
                            level,
                            ..
                        } => Line::from(vec![
                            dim(format!("  {} ", status.mark())),
                            lvl(path.clone(), *level),
                            dim(match status {
                                crate::plan::Status::Drift => " differs from the applied version",
                                crate::plan::Status::New => " new",
                                crate::plan::Status::Removed => " removed",
                                _ => "",
                            }),
                        ]),
                        PlanRow::Note(t, l) => {
                            Line::from(vec![Span::raw("    "), lvl(t.clone(), *l)])
                        }
                    };
                    if i == self.plan_sel {
                        line = line.bg(BG_SEL);
                    }
                    lines.push(line);
                }
            }
        }
        f.render_widget(
            titled(
                &format!("What will change · {hosts} hosts, {files} files"),
                lines,
            ),
            what,
        );
        let lines = checks
            .iter()
            .map(|(l, t)| {
                Line::from(vec![
                    lvl(
                        match l {
                            Level::Ok => "✓ ",
                            Level::Warn => "! ",
                            _ => "✗ ",
                        },
                        *l,
                    ),
                    Span::raw(t.clone()),
                ])
            })
            .collect();
        f.render_widget(titled("Checks before apply", lines), chk);

        let [log_area, status_area] = Layout::vertical([Constraint::Min(3), Constraint::Length(1)])
            .areas(right.inner(ratatui::layout::Margin::new(0, 0)));
        let mut lines: Vec<Line> = self
            .log
            .iter()
            .map(|l| {
                let t = l.trim_start();
                if l.starts_with('▸') || t.starts_with("план для") || t.starts_with("plan for")
                {
                    Line::from(Span::styled(l.clone(), Style::new().bold()))
                } else if t.starts_with('✓') {
                    Line::from(Span::styled(l.clone(), Style::new().fg(Color::Green)))
                } else if t.starts_with('–') || t.starts_with('-') {
                    Line::from(dim(l.clone()))
                } else if t.starts_with('!') {
                    Line::from(Span::styled(l.clone(), Style::new().fg(Color::Yellow)))
                } else if t.starts_with('✗') || t.starts_with("Error") || t.starts_with("error") {
                    Line::from(Span::styled(l.clone(), Style::new().fg(Color::Red)))
                } else {
                    Line::from(l.clone())
                }
            })
            .collect();
        if self.job_running() {
            let spin = ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧']
                [(self.job_elapsed.as_millis() / 125 % 8) as usize];
            lines.push(Line::from(Span::styled(
                format!("{spin} {} …", self.job_name),
                Style::new().fg(BLUE),
            )));
        } else if self.log.is_empty() {
            lines.push(Line::from(dim(
                "Enter — apply all · s — the selected host · output streams here",
            )));
        }
        let h = log_area.height.saturating_sub(3) as usize;
        let scroll = lines.len().saturating_sub(h) as u16;
        f.render_widget(
            titled("Apply · sequential, stop at the first error", lines).scroll((scroll, 0)),
            log_area,
        );
        let done = self
            .log
            .iter()
            .filter(|l| l.contains("маркер") || l.contains("marker"))
            .count();
        let el = self.job_elapsed.as_secs();
        let status = Line::from(vec![
            dim("elapsed "),
            Span::raw(format!("{}:{:02}", el / 60, el % 60)),
            dim("   done "),
            Span::raw(format!("{done} of {hosts}")),
            dim("   tag applied/* on finish"),
            match self.job_ok {
                Some(true) => Span::styled("   ✓ done", Style::new().fg(Color::Green)),
                Some(false) => Span::styled("   ✗ failed", Style::new().fg(Color::Red)),
                None => Span::raw(""),
            },
        ]);
        f.render_widget(
            Paragraph::new(status).block(
                ratatui::widgets::Block::new().padding(ratatui::widgets::Padding::horizontal(2)),
            ),
            status_area,
        );
    }

    // ---------- 5 topology ----------

    fn topology(&mut self, f: &mut Frame, area: Rect) {
        let [graph, matrix, hist] = Layout::vertical([
            Constraint::Length(8),
            Constraint::Min(5),
            Constraint::Length(5),
        ])
        .areas(area);
        let nodes = graph_nodes(&self.st, &self.status, self.head.as_deref());
        let mut rows: [Vec<Span>; 4] = Default::default();
        for (i, (name, sub, level)) in nodes.iter().enumerate() {
            let w = node(name).chars().count().max(sub.chars().count()) + 2;
            let c = Style::new().fg(color(*level));
            rows[0].push(Span::styled(format!("╭{}╮", "─".repeat(w)), c));
            rows[1].push(Span::styled("│ ".to_string(), c));
            rows[1].push(Span::styled(
                format!("{:<w$}", node(name), w = w - 1),
                Style::new().bold(),
            ));
            rows[1].push(Span::styled("│".to_string(), c));
            rows[2].push(Span::styled("│ ".to_string(), c));
            rows[2].push(dim(format!("{:<w$}", sub, w = w - 1)));
            rows[2].push(Span::styled("│".to_string(), c));
            rows[3].push(Span::styled(format!("╰{}╯", "─".repeat(w)), c));
            if let Some((next, _, _)) = nodes.get(i + 1) {
                let edges: Vec<&crate::metrics::Edge> = self
                    .status
                    .matrix
                    .iter()
                    .filter(|e| e.src == *name && e.dst == *next)
                    .collect();
                let (sym, lv) = if edges.is_empty() {
                    ("─────", Level::Off)
                } else if edges.iter().any(|e| e.ok) {
                    ("──●──", Level::Ok)
                } else {
                    ("──✗──", Level::Bad)
                };
                rows[0].push(Span::raw("     "));
                rows[1].push(lvl(sym, lv));
                rows[2].push(Span::raw("     "));
                rows[3].push(Span::raw("     "));
            }
        }
        let errs = crate::validate::validate(&self.st);
        let check = if errs.is_empty() {
            Line::from(dim(
                "static check ✓ · via graph acyclic · every entry has an exit · link keys match · awg subnets disjoint",
            ))
        } else {
            Line::from(Span::styled(
                format!("static check: ✗ {} errors · V for details", errs.len()),
                Style::new().fg(Color::Red),
            ))
        };
        let lines = rows.into_iter().map(Line::from).chain([check]).collect();
        f.render_widget(titled("Graph", lines), graph);

        let m = self.matrix();
        self.sel[4] = step(self.sel[4], m.len(), 0);
        let header = [
            "edge",
            "reality",
            "ws+tls",
            "hysteria2",
            "awg",
            "ssh",
            "note",
        ];
        // ponytail: the mtg column is left out of the table (laptop → entry only); Enter cycles it in the history
        let keep = [0usize, 1, 2, 3, 5];
        let cells: Vec<Vec<String>> = m
            .iter()
            .map(|((s, d), c)| {
                let mut v = vec![format!("{} → {}", node(s), node(d))];
                v.extend(keep.iter().map(|i| c[*i].0.clone()));
                v.push(edge_note(&self.st, &self.status, s, d));
                v
            })
            .collect();
        let trows = m.iter().zip(&cells).map(|(((_, _), c), row)| {
            let mut v = vec![Cell::from(row[0].clone()).bold()];
            v.extend(keep.iter().map(|i| cell(&c[*i].0, c[*i].1, true)));
            v.push(Cell::from(row[6].clone()).fg(DIM));
            TRow::new(v)
        });
        if m.is_empty() {
            f.render_widget(
                titled(
                    "Matrix",
                    vec![Line::from(dim(
                        "the matrix has not run yet · t — run it now",
                    ))],
                ),
                matrix,
            );
        } else {
            let mut w = widths(&header, &cells);
            w[6] = Constraint::Min(10);
            f.render_stateful_widget(
                table(&header, trows.collect(), w).block(panel()),
                matrix,
                &mut TableState::default().with_selected(self.sel[4]),
            );
        }

        let proto = MATRIX_COLS[self.proto_sel];
        let (title, lines) = match m.get(self.sel[4]) {
            None => (
                "History of selected edge".to_string(),
                vec![Line::from(dim("no data yet"))],
            ),
            Some(((s, d), _)) => {
                let h = self
                    .db
                    .edge_history(s, d, proto, self.now - 14 * DAY)
                    .unwrap_or_default();
                let days = day_buckets(&h, self.now, 14);
                let mut squares = vec![];
                for b in &days {
                    squares.push(match b {
                        Some(l) => Span::styled("■■ ", Style::new().fg(color(*l))),
                        None => dim("□□ "),
                    });
                }
                let bad = days.iter().filter(|b| **b == Some(Level::Bad)).count();
                let with = days.iter().filter(|b| b.is_some()).count();
                squares.push(dim(format!(
                    "  {} — {} · {}",
                    short_date(self.now - 13 * DAY),
                    short_date(self.now),
                    if with == 0 {
                        "no data yet".to_string()
                    } else if bad == 0 {
                        format!("fine on all {with} day(s) with data")
                    } else {
                        format!("failed on {bad} of {with} day(s) with data")
                    }
                )));
                (
                    format!(
                        "History of selected edge · {} → {} · {proto} · 14 days",
                        node(s),
                        node(d)
                    ),
                    vec![Line::from(squares)],
                )
            }
        };
        f.render_widget(titled(&title, lines), hist);
    }

    // ---------- 6 routing ----------

    fn routing(&mut self, f: &mut Frame, area: Rect) {
        let Some(host) = self.routing_host() else {
            f.render_widget(titled("Routing", vec![Line::from(dim("no entries"))]), area);
            return;
        };
        let [left, right] =
            Layout::horizontal([Constraint::Percentage(64), Constraint::Percentage(36)])
                .spacing(1)
                .areas(area);
        let sets = self.st.sets.len() as u16;
        let [rules_a, dns_a] =
            Layout::vertical([Constraint::Min(5), Constraint::Length(6)]).areas(left);
        let [sets_a, trace_a] =
            Layout::vertical([Constraint::Length(sets * 2 + 4), Constraint::Min(6)]).areas(right);

        let rules = rule_rows(&self.st, &host);
        self.sel[5] = step(self.sel[5], rules.len(), 0);
        let cond_w = rules_a.width.saturating_sub(4 + 5 + 18 + 3) as usize;
        let mut lines = vec![Line::from(vec![
            dim(
                "group — whose traffic · rules top-down inside a group, first match wins · action: ",
            ),
            Span::styled("direct", Style::new().fg(Color::Green)),
            dim(" = from RU directly, "),
            Span::styled("link", Style::new().fg(LINK)),
            dim(" = via exit, "),
            Span::styled("block", Style::new().fg(Color::Red)),
        ])];
        let mut last = None;
        for (i, (g, n, r)) in rules.iter().enumerate() {
            if last != Some(*g) {
                lines.push(Line::from(""));
                lines.push(Line::from(vec![
                    Span::styled(group_name(*g).to_string(), Style::new().bold()),
                    dim(format!(" · {}", group_note(&self.st, &host, *g))),
                ]));
                last = Some(*g);
            }
            let action = &r.cells[2];
            let ac = if action.starts_with("→") {
                LINK
            } else if action == "block" {
                Color::Red
            } else {
                Color::Green
            };
            let mut line = Line::from(vec![
                dim(format!("  {n:<3}")),
                Span::styled(
                    format!("{:<cond_w$}", fit(&r.cells[1], cond_w)),
                    if r.level == Level::Off {
                        Style::new().fg(DIM)
                    } else {
                        Style::new()
                    },
                ),
                Span::styled(
                    format!(" {action:<17}"),
                    Style::new().fg(if r.level == Level::Off { DIM } else { ac }),
                ),
                lvl(r.cells[3].clone(), r.level),
            ]);
            if i == self.sel[5] {
                line = line.bg(BG_SEL);
            }
            lines.push(line);
        }
        lines.push(Line::from(""));
        lines.push(Line::from(vec![
            Span::styled("If nothing matched", Style::new().bold()),
            dim(" · engine default, not a rule"),
        ]));
        lines.push(Line::from(vec![
            dim("  —  "),
            dim(format!(
                "{:<cond_w$}",
                "e.g. traffic from an undescribed inbound"
            )),
            Span::styled("direct", Style::new().fg(Color::Green)),
        ]));
        lines.push(Line::from(dim(
            "awg traffic lands here via tproxy from awg0; sniffing tls/http/quic restores domains",
        )));
        f.render_widget(titled("Rules", lines), rules_a);

        let dns = self
            .st
            .routing
            .get(&host)
            .map(|r| {
                let mut v: Vec<Line> = dns_text(r).into_iter().map(Line::from).collect();
                v.push(Line::from(dim("modes = account sets: split → all *-split-* · full → all *-full-* · for awg the mode sets the peer subnet")));
                v
            })
            .unwrap_or_default();
        f.render_widget(titled("DNS on the entry", dns), dns_a);

        let mut lines = vec![];
        for n in self.st.sets.keys() {
            lines.push(Line::from(vec![
                Span::styled(format!("{n:<12}"), Style::new().bold()),
                Span::raw(set_members(&self.st, n)),
            ]));
            let ex = set_examples(&self.st, n, 4);
            lines.push(Line::from(dim(format!("{:<12}{ex}", ""))));
        }
        lines.push(Line::from(vec![
            dim(format!("{:<12}", "geodata")),
            Span::raw(
                self.st
                    .versions
                    .get("geodata")
                    .map(|p| {
                        format!(
                            "{}{}",
                            p.version,
                            if p.sha256.is_some() {
                                " · sha ✓"
                            } else {
                                ""
                            }
                        )
                    })
                    .unwrap_or("— not pinned".into()),
            ),
        ]));
        f.render_widget(titled("Domain and IP sets", lines), sets_a);

        let mut lines = vec![];
        match self.trace_user_name() {
            None => lines.push(Line::from(dim("no users to trace"))),
            Some((user, mode)) => {
                lines.push(Line::from(vec![
                    dim(format!("{:<9}", "user")),
                    Span::styled(user.clone(), Style::new().bold()),
                    dim(" · "),
                    Span::styled(mode.name().to_string(), Style::new().fg(LINK)),
                ]));
                lines.push(Line::from(vec![
                    dim(format!("{:<9}", "address")),
                    Span::styled(
                        format!(
                            " {}{} ",
                            self.trace_addr,
                            if self.trace_input { "_" } else { "" }
                        ),
                        Style::new().bg(BG_CODE).fg(Color::White),
                    ),
                ]));
                lines.push(Line::from(""));
                let (steps, result) = trace(&self.st, &host, mode, &self.trace_addr);
                for s in &steps {
                    let g = match s.group {
                        Group::All => "for all",
                        Group::Split => "split",
                        Group::Full => "full",
                    };
                    let t = format!("{g} #{} — {}", s.idx, s.text);
                    lines.push(Line::from(if s.matched {
                        Span::styled(t, Style::new().fg(Color::Green))
                    } else {
                        dim(t)
                    }));
                }
                lines.push(Line::from(""));
                lines.push(Line::from(Span::styled(
                    format!(" {result} "),
                    Style::new().bg(BG_CODE).fg(Color::White).bold(),
                )));
                lines.push(Line::from(dim(
                    "static trace by the state's rules · geosite/geoip are not evaluated offline",
                )));
                lines.push(Line::from(""));
                lines.push(Line::from(key("t", "another address · u another user")));
            }
        }
        f.render_widget(titled("Where does it go? · trace by rules", lines), trace_a);
    }

    // ---------- overlays ----------

    fn overlays(&self, f: &mut Frame, body: Rect) {
        if self.help {
            let a = center(body, 96, 10);
            f.render_widget(Clear, a);
            f.render_widget(
                titled(
                    "Keys",
                    vec![
                        Line::from("everywhere: 1–6 screens · j/k rows · Enter open · Esc back · / filter · ? help · q quit"),
                        Line::from("p plan · a apply · r collect metrics · t run matrix · V validate"),
                        Line::from(""),
                        Line::from(vec![dim("this screen: "), Span::raw("")]).spans.into_iter().chain(keys(FOOTER[self.screen]).spans).collect::<Vec<_>>().into(),
                        Line::from(""),
                        Line::from(vec![
                            Span::styled("green", Style::new().fg(Color::Green)),
                            dim(" fine · "),
                            Span::styled("yellow", Style::new().fg(Color::Yellow)),
                            dim(" needs attention · "),
                            Span::styled("red", Style::new().fg(Color::Red)),
                            dim(" broken · "),
                            dim("grey inactive"),
                        ]),
                    ],
                ),
                a,
            );
        }
        if let Some(form) = &self.form {
            let (pv, pv_rows): (Vec<Line>, Vec<Row>) = match &self.preview {
                Some(Ok((msg, rows))) => (
                    vec![Line::from(vec![
                        dim("commit: "),
                        Span::styled(msg.clone(), Style::new().fg(Color::Green)),
                    ])],
                    rows.clone(),
                ),
                Some(Err(e)) => (
                    vec![Line::from(Span::styled(
                        format!("✗ {e}"),
                        Style::new().fg(Color::Red),
                    ))],
                    vec![],
                ),
                None => (vec![], vec![]),
            };
            let a = center(
                body,
                100,
                form.fields.len() as u16
                    + 7
                    + pv_rows.len().min(10) as u16
                    + u16::from(!pv_rows.is_empty()),
            );
            f.render_widget(Clear, a);
            let mut lines: Vec<Line> = form
                .fields
                .iter()
                .enumerate()
                .map(|(i, fld)| {
                    let cur = i == form.cur;
                    let hot = if cur {
                        Style::new().bg(BG_CODE).fg(Color::White).bold()
                    } else {
                        Style::new()
                    };
                    let mut spans = vec![dim(format!("{:>24}  ", fld.label))];
                    match &fld.kind {
                        FieldKind::Text => spans.push(Span::styled(
                            format!(" {}{} ", fld.value, if cur { "▏" } else { "" }),
                            hot,
                        )),
                        FieldKind::Bool => spans.push(Span::styled(
                            format!(
                                " {} ",
                                if fld.value == "yes" {
                                    "[x] yes"
                                } else {
                                    "[ ] no"
                                }
                            ),
                            hot,
                        )),
                        FieldKind::Multi { options, cursor } => {
                            for (j, o) in options.iter().enumerate() {
                                let on = fld.marked(o);
                                let text = format!("{} {}  ", if on { "[x]" } else { "[ ]" }, o);
                                spans.push(Span::styled(
                                    text,
                                    if cur && j == *cursor {
                                        hot
                                    } else if on {
                                        Style::new().fg(Color::Green)
                                    } else {
                                        Style::new()
                                    },
                                ));
                            }
                        }
                    }
                    Line::from(spans)
                })
                .collect();
            lines.push(Line::from(""));
            lines.push(match &form.error {
                Some(e) => Line::from(Span::styled(format!("✗ {e}"), Style::new().fg(Color::Red))),
                None => keys(&[
                    ("Enter", "save and commit"),
                    ("Esc", "cancel"),
                    ("Tab", "next field"),
                    ("Space", "toggle"),
                    ("←→", "pick option"),
                ]),
            });
            lines.extend(pv);
            if !pv_rows.is_empty() {
                lines.push(Line::from(dim("How it will land in the list:")));
                for r in pv_rows.iter().take(10) {
                    lines.push(Line::from(vec![
                        dim(format!("  {:<3}", r.cells[0])),
                        lvl(r.cells[3].clone(), r.level),
                        Span::raw(format!(" {} ", fit(&r.cells[1], 50))),
                        Span::styled(
                            r.cells[2].clone(),
                            Style::new().fg(if r.cells[2].starts_with('→') {
                                LINK
                            } else if r.cells[2] == "block" {
                                Color::Red
                            } else {
                                Color::Green
                            }),
                        ),
                    ]));
                }
            }
            f.render_widget(titled(&form.title, lines), a);
        }
        if !self.confirm_text.is_empty() && self.confirm_pending() {
            let a = center(body, 70, 4);
            f.render_widget(Clear, a);
            f.render_widget(
                titled(
                    "Confirm",
                    vec![Line::from(Span::styled(
                        self.confirm_text.clone(),
                        Style::new().fg(Color::Yellow),
                    ))],
                ),
                a,
            );
        }
        if let Some(t) = &self.text {
            let a = body.inner(ratatui::layout::Margin::new(2, 1));
            f.render_widget(Clear, a);
            let lines = t.lines.iter().map(|l| Line::from(l.as_str())).collect();
            f.render_widget(
                titled(&format!("{} · j/k scroll · Esc close", t.title), lines)
                    .wrap(Wrap { trim: false })
                    .scroll((t.scroll, 0)),
                a,
            );
        }
    }
}
