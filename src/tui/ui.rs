//! Style primitives shared by the screens: colours, panels, tables, key hints.
use super::model::Level;
use ratatui::layout::{Constraint, Rect};
use ratatui::style::{Color, Style, Stylize};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, BorderType, Cell, Padding, Paragraph, Row as TRow, Table, Wrap};

pub const BG_HEADER: Color = Color::Indexed(236);
pub const BG_SEL: Color = Color::Indexed(237);
pub const BG_CODE: Color = Color::Indexed(235);
pub const DIM: Color = Color::DarkGray;
pub const BLUE: Color = Color::Blue;
pub const LINK: Color = Color::LightBlue;

pub fn color(l: Level) -> Color {
    match l {
        Level::Ok => Color::Green,
        Level::Warn => Color::Yellow,
        Level::Bad => Color::Red,
        Level::Off => DIM,
    }
}

/// Rounded grey panel with one cell of horizontal padding.
pub fn panel() -> Block<'static> {
    Block::bordered()
        .border_type(BorderType::Rounded)
        .border_style(Style::new().fg(DIM))
        .padding(Padding::horizontal(1))
}

/// Panel title as the first inner line, grey bold (as in the mockups).
pub fn title(t: &str) -> Line<'static> {
    Line::from(Span::styled(t.to_string(), Style::new().fg(DIM).bold()))
}

/// Paragraph inside a panel with a title line first.
pub fn titled<'a>(t: &str, mut lines: Vec<Line<'a>>) -> Paragraph<'a> {
    lines.insert(0, title(t));
    Paragraph::new(lines)
        .block(panel())
        .wrap(Wrap { trim: false })
}

/// Grey text.
pub fn dim(s: impl Into<String>) -> Span<'static> {
    Span::styled(s.into(), Style::new().fg(DIM))
}

/// Coloured text by level (grey for Off).
pub fn lvl(s: impl Into<String>, l: Level) -> Span<'static> {
    Span::styled(s.into(), Style::new().fg(color(l)))
}

/// `key description` pair for footers and cards: bold key, grey description.
pub fn key(k: &str, d: &str) -> Vec<Span<'static>> {
    vec![
        Span::styled(k.to_string(), Style::new().bold()),
        Span::raw(" "),
        dim(d.to_string()),
    ]
}

/// Footer left part: key hints separated by two spaces.
pub fn keys(pairs: &[(&str, &str)]) -> Line<'static> {
    let mut v = vec![];
    for (i, (k, d)) in pairs.iter().enumerate() {
        if i > 0 {
            v.push(Span::raw("  "));
        }
        v.extend(key(k, d));
    }
    Line::from(v)
}

/// Key/value lines: grey key padded to `w`, value coloured by level.
pub fn kv(rows: &[(String, String, Level)], w: usize) -> Vec<Line<'static>> {
    rows.iter()
        .map(|(k, v, l)| {
            Line::from(vec![
                dim(format!("{k:<w$}")),
                Span::styled(
                    v.clone(),
                    if *l == Level::Off {
                        Style::new().fg(DIM)
                    } else if *l == Level::Ok && !v.starts_with('●') && !v.starts_with('✓') {
                        Style::new()
                    } else {
                        Style::new().fg(color(*l))
                    },
                ),
            ])
        })
        .collect()
}

/// Column widths from the content, at least the header width.
pub fn widths(header: &[&str], rows: &[Vec<String>]) -> Vec<Constraint> {
    (0..header.len())
        .map(|i| {
            let w = rows
                .iter()
                .map(|r| r.get(i).map(|c| c.chars().count()).unwrap_or(0))
                .chain([header[i].chars().count()])
                .max()
                .unwrap_or(1);
            Constraint::Length(w as u16)
        })
        .collect()
}

/// Table with a dark header line, selected row on a dark background with a blue `▎` marker.
pub fn table<'a>(
    header: &[&'a str],
    rows: Vec<TRow<'a>>,
    mut widths: Vec<Constraint>,
) -> Table<'a> {
    // the last column takes the rest so the header and selection bands reach the right border
    if let Some(last) = widths.last_mut()
        && let Constraint::Length(n) = *last
    {
        *last = Constraint::Min(n);
    }
    Table::new(rows, widths)
        .header(
            TRow::new(header.iter().map(|h| Cell::from(*h)))
                .style(Style::new().fg(DIM).bg(BG_HEADER).bold()),
        )
        .row_highlight_style(Style::new().bg(BG_SEL))
        .highlight_symbol(Span::styled("▎", Style::new().fg(BLUE)))
        .column_spacing(2)
}

/// A cell coloured by level; Ok keeps the default colour unless `tint`.
pub fn cell(s: &str, l: Level, tint: bool) -> Cell<'static> {
    let c = Cell::from(s.to_string());
    match l {
        Level::Ok if !tint => c,
        _ => c.fg(color(l)),
    }
}

/// Rectangle of `w`×`h` centred in `area`.
pub fn center(area: Rect, w: u16, h: u16) -> Rect {
    let w = w.min(area.width);
    let h = h.min(area.height);
    Rect::new(
        area.x + (area.width - w) / 2,
        area.y + (area.height - h) / 2,
        w,
        h,
    )
}

/// Link on a dark code background.
pub fn code(s: &str) -> Line<'static> {
    Line::from(Span::styled(
        format!(" {s} "),
        Style::new().fg(LINK).bg(BG_CODE),
    ))
}
