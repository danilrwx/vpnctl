//! Thin wrapper around the system git in the state directory.
use anyhow::{Context, Result, bail};
use std::path::Path;
use std::process::Command;

fn git(dir: &Path, args: &[&str]) -> Result<String> {
    let out = Command::new("git")
        .arg("-C")
        .arg(dir)
        .args(args)
        .output()
        .context("run git")?;
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

/// Lightweight tag: signing and annotation from the global config are not needed and may require a key.
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

/// Last tag with the prefix by name (names `applied/<UTC date>` sort like dates).
pub fn last_tag(dir: &Path, prefix: &str) -> Option<String> {
    git(dir, &["tag", "-l", &format!("{prefix}*")])
        .ok()?
        .lines()
        .max()
        .map(String::from)
}

/// HEAD: (short sha, message, commit epoch).
pub fn head_info(dir: &Path) -> Option<(String, String, i64)> {
    let out = git(dir, &["log", "-1", "--format=%h%x09%s%x09%ct"]).ok()?;
    let mut p = out.splitn(3, '\t');
    Some((p.next()?.into(), p.next()?.into(), p.next()?.parse().ok()?))
}

/// Number of commits in the `from..to` range.
pub fn count(dir: &Path, from: &str, to: &str) -> Option<usize> {
    git(dir, &["rev-list", "--count", &format!("{from}..{to}")])
        .ok()?
        .parse()
        .ok()
}

/// First commit that added the line `needle` to the file: (short sha, YYYY-MM-DD date).
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
