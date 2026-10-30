//! Тонкая обёртка над системным git в каталоге состояния.
use anyhow::{Context, Result, bail};
use std::path::Path;
use std::process::Command;

fn git(dir: &Path, args: &[&str]) -> Result<String> {
    let out = Command::new("git")
        .arg("-C")
        .arg(dir)
        .args(args)
        .output()
        .context("запуск git")?;
    if !out.status.success() {
        bail!(
            "git {}: {}",
            args.join(" "),
            String::from_utf8_lossy(&out.stderr).trim()
        );
    }
    Ok(String::from_utf8_lossy(&out.stdout).trim().to_string())
}

pub fn init(dir: &Path) -> Result<()> {
    if !dir.join(".git").exists() {
        git(dir, &["init", "-q", "-b", "main"])?;
    }
    Ok(())
}

pub fn commit(dir: &Path, msg: &str) -> Result<()> {
    git(dir, &["add", "-A"])?;
    let mut args = vec![];
    if git(dir, &["config", "user.email"])
        .unwrap_or_default()
        .is_empty()
    {
        args.extend([
            "-c",
            "user.name=vpnctl",
            "-c",
            "user.email=vpnctl@localhost",
        ]);
    }
    args.extend(["commit", "-q", "-m", msg]);
    git(dir, &args)?;
    Ok(())
}

pub fn is_clean(dir: &Path) -> Result<bool> {
    Ok(git(dir, &["status", "--porcelain"])?.is_empty())
}

pub fn branch(dir: &Path) -> Result<String> {
    git(dir, &["rev-parse", "--abbrev-ref", "HEAD"])
}

pub fn head(dir: &Path) -> Result<String> {
    git(dir, &["rev-parse", "--short", "HEAD"])
}

/// Лёгкий тег: подпись и аннотация из глобального конфига не нужны и могут требовать ключ.
pub fn tag(dir: &Path, name: &str) -> Result<()> {
    git(
        dir,
        &[
            "-c",
            "tag.gpgSign=false",
            "-c",
            "tag.forceSignAnnotated=false",
            "tag",
            name,
        ],
    )
    .map(|_| ())
}

/// Последний по имени тег с префиксом (имена `applied/<дата UTC>` сортируются как даты).
pub fn last_tag(dir: &Path, prefix: &str) -> Option<String> {
    git(dir, &["tag", "-l", &format!("{prefix}*")])
        .ok()?
        .lines()
        .max()
        .map(String::from)
}

/// HEAD: (короткий sha, сообщение, epoch коммита).
pub fn head_info(dir: &Path) -> Option<(String, String, i64)> {
    let out = git(dir, &["log", "-1", "--format=%h%x09%s%x09%ct"]).ok()?;
    let mut p = out.splitn(3, '\t');
    Some((p.next()?.into(), p.next()?.into(), p.next()?.parse().ok()?))
}

/// Число коммитов в диапазоне `from..to`.
pub fn count(dir: &Path, from: &str, to: &str) -> Option<usize> {
    git(dir, &["rev-list", "--count", &format!("{from}..{to}")])
        .ok()?
        .parse()
        .ok()
}

/// Первый коммит, добавивший строку `needle` в файл: (короткий sha, дата YYYY-MM-DD).
pub fn added(dir: &Path, file: &str, needle: &str) -> Option<(String, String)> {
    let out = git(
        dir,
        &[
            "log",
            "--reverse",
            "--format=%h %cs",
            "-S",
            needle,
            "--",
            file,
        ],
    )
    .ok()?;
    let line = out.lines().next()?;
    let (sha, date) = line.split_once(' ')?;
    Some((sha.into(), date.into()))
}
