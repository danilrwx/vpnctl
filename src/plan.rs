//! plan: compare the render with the hosts — pending changes, drift, reachability.
use crate::render::{self, HostFiles, Rendered};
use crate::ssh::Ssh;
use crate::state::State;
use anyhow::{Context, Result, bail};
use std::collections::BTreeMap;

pub const MARKER: &str = "/etc/vpnctl/applied-commit";
pub const BOOTSTRAP: &str = "/etc/vpnctl/bootstrap";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Status {
    Unchanged,
    Changed,
    New,
    /// The host has neither the applied nor the upcoming content
    Drift,
    Removed,
}

impl Status {
    pub fn mark(self) -> &'static str {
        match self {
            Status::Unchanged => "=",
            Status::Changed => "~",
            Status::New => "+",
            Status::Drift => "!",
            Status::Removed => "-",
        }
    }
}

#[derive(Debug, Default)]
pub struct HostPlan {
    pub name: String,
    pub unreachable: Option<String>,
    pub bootstrapped: bool,
    pub marker: Option<String>,
    pub files: Vec<(String, Status)>,
    pub notes: Vec<String>,
}

impl HostPlan {
    pub fn changes(&self) -> usize {
        self.files
            .iter()
            .filter(|(_, s)| *s != Status::Unchanged)
            .count()
    }
}

/// File status from three hashes: previously applied, upcoming, on the host.
pub fn classify(old: Option<&str>, new: Option<&str>, host: Option<&str>) -> Status {
    match (old, new, host) {
        (_, Some(n), Some(h)) if n == h => Status::Unchanged,
        (_, None, Some(_)) => Status::Removed,
        (_, Some(_), None) => Status::New,
        (Some(o), Some(_), Some(h)) if o == h => Status::Changed,
        (None, Some(_), Some(_)) => Status::Changed, // first apply: no history yet
        (_, Some(_), Some(_)) => Status::Drift,
        (_, None, None) => Status::Unchanged,
    }
}

/// Script: marker, bootstrap, hashes of the listed paths.
pub fn probe_script(paths: &[String]) -> String {
    let list = paths
        .iter()
        .map(|p| format!("'{p}'"))
        .collect::<Vec<_>>()
        .join(" ");
    format!(
        "cat {MARKER} 2>/dev/null || echo -; test -e {BOOTSTRAP} && echo yes || echo no; for f in {list}; do if [ -e \"$f\" ]; then printf '%s %s\\n' \"$(sha256sum \"$f\" | cut -d' ' -f1)\" \"$f\"; else printf 'missing %s\\n' \"$f\"; fi; done"
    )
}

pub struct Probe {
    pub marker: Option<String>,
    pub bootstrapped: bool,
    pub hashes: BTreeMap<String, Option<String>>,
}

pub fn parse_probe(out: &str) -> Probe {
    let mut lines = out.lines();
    let marker = lines.next().filter(|m| *m != "-").map(String::from);
    let bootstrapped = lines.next() == Some("yes");
    let hashes = lines
        .filter_map(|l| l.split_once(' '))
        .map(|(h, p)| (p.to_string(), (h != "missing").then(|| h.to_string())))
        .collect();
    Probe {
        marker,
        bootstrapped,
        hashes,
    }
}

/// State render at the marker commit (for drift). No commit — None.
pub fn render_at(st: &State, commit: &str, host: &str) -> Option<HostFiles> {
    let tmp = std::env::temp_dir().join(format!("vpnctl-at-{}-{commit}", std::process::id()));
    std::fs::create_dir_all(&tmp).ok()?;
    let ar = std::process::Command::new("git")
        .arg("-C")
        .arg(&st.dir)
        .args(["archive", commit])
        .output()
        .ok()?;
    if !ar.status.success() {
        return None;
    }
    let mut tar = std::process::Command::new("tar")
        .arg("-x")
        .arg("-C")
        .arg(&tmp)
        .stdin(std::process::Stdio::piped())
        .spawn()
        .ok()?;
    use std::io::Write;
    tar.stdin.take()?.write_all(&ar.stdout).ok()?;
    tar.wait().ok()?;
    let old = State::load(&tmp).ok()?;
    let r = render::render_host(&old, host).ok();
    std::fs::remove_dir_all(&tmp).ok();
    r
}

fn hash_map(f: &HostFiles) -> BTreeMap<String, String> {
    f.iter()
        .map(|(p, b)| (p.clone(), render::sha256(b)))
        .collect()
}

/// Queries hosts in parallel and builds the plan.
pub fn plan(st: &State, rendered: &Rendered) -> Result<Vec<HostPlan>> {
    let results: Vec<(String, Result<Probe, String>)> = std::thread::scope(|s| {
        let handles: Vec<_> = rendered
            .hosts
            .iter()
            .map(|(name, files)| {
                s.spawn(move || {
                    let paths: Vec<String> = files.keys().cloned().collect();
                    let r = (|| -> Result<Probe, String> {
                        let ssh = Ssh::for_host(st, name).map_err(|e| e.to_string())?;
                        ssh.reachable()?;
                        // plus old paths when the marker is known: they need hashing too
                        let out = ssh.sh(&probe_script(&paths)).map_err(|e| e.to_string())?;
                        let mut p = parse_probe(&out);
                        if let Some(m) = p.marker.clone()
                            && let Some(old) = render_at(st, &m, name)
                        {
                            let extra: Vec<String> = old
                                .keys()
                                .filter(|k| !files.contains_key(*k))
                                .cloned()
                                .collect();
                            if !extra.is_empty() {
                                let out2 =
                                    ssh.sh(&probe_script(&extra)).map_err(|e| e.to_string())?;
                                p.hashes.extend(parse_probe(&out2).hashes);
                            }
                        }
                        Ok(p)
                    })();
                    (name.clone(), r)
                })
            })
            .collect();
        handles.into_iter().map(|h| h.join().unwrap()).collect()
    });

    let mut plans = vec![];
    for (name, res) in results {
        let mut hp = HostPlan {
            name: name.clone(),
            ..Default::default()
        };
        let new = hash_map(&rendered.hosts[&name]);
        match res {
            Err(e) => {
                hp.unreachable = Some(e);
                hp.files = new.keys().map(|p| (p.clone(), Status::Changed)).collect();
            }
            Ok(p) => {
                hp.bootstrapped = p.bootstrapped;
                hp.marker = p.marker.clone();
                let old = p
                    .marker
                    .as_deref()
                    .and_then(|m| render_at(st, m, &name))
                    .map(|f| hash_map(&f));
                if p.marker.is_some() && old.is_none() {
                    hp.notes.push(format!(
                        "marker commit {} is not in the repository: drift cannot be determined",
                        p.marker.as_deref().unwrap()
                    ));
                }
                let mut paths: Vec<String> = new.keys().cloned().collect();
                if let Some(o) = &old {
                    paths.extend(o.keys().filter(|k| !new.contains_key(*k)).cloned());
                }
                for path in paths {
                    let s = classify(
                        old.as_ref().and_then(|o| o.get(&path)).map(String::as_str),
                        new.get(&path).map(String::as_str),
                        p.hashes.get(&path).and_then(|h| h.as_deref()),
                    );
                    hp.files.push((path, s));
                }
                if !p.bootstrapped {
                    hp.notes
                        .push("bootstrap not done: the host will be skipped".into());
                }
            }
        }
        plans.push(hp);
    }
    // reachability through the jump: recomputed once the jump is known
    let down: Vec<String> = plans
        .iter()
        .filter(|p| p.unreachable.is_some())
        .map(|p| p.name.clone())
        .collect();
    for p in &mut plans {
        if let (Some(_), Some(v)) = (&p.unreachable, &st.hosts[&p.name].via)
            && down.contains(v)
        {
            p.unreachable = Some(format!("unreachable via {v}"));
        }
    }
    Ok(plans)
}

pub fn print(plans: &[HostPlan]) {
    for p in plans {
        if let Some(e) = &p.unreachable {
            println!("{} · ✗ {e}", p.name);
            continue;
        }
        println!(
            "{} · {} · {} changes",
            p.name,
            p.marker
                .as_deref()
                .map(|m| format!("applied {m}"))
                .unwrap_or("not applied yet".into()),
            p.changes()
        );
        for (path, s) in &p.files {
            if *s != Status::Unchanged {
                println!(
                    "  {} {path}{}",
                    s.mark(),
                    if *s == Status::Drift {
                        "   drift: edited on the host, will be overwritten (--adopt to take it into the repo)"
                    } else {
                        ""
                    }
                );
            }
        }
        for n in &p.notes {
            println!("  ! {n}");
        }
    }
}

/// File from the host.
pub fn fetch(st: &State, host: &str, path: &str) -> Result<String> {
    Ssh::for_host(st, host)?.sh(&format!("cat '{path}'"))
}

/// Diff between the render and the file on the host.
pub fn diff(st: &State, rendered: &Rendered, host: &str, path: &str) -> Result<String> {
    let new = rendered
        .hosts
        .get(host)
        .and_then(|f| f.get(path))
        .with_context(|| format!("{host}: file {path} is not rendered"))?;
    let cur = fetch(st, host, path)?;
    let tmp = std::env::temp_dir().join(format!("vpnctl-diff-{}", std::process::id()));
    std::fs::create_dir_all(&tmp)?;
    std::fs::write(tmp.join("host"), &cur)?;
    std::fs::write(tmp.join("repo"), new)?;
    let out = std::process::Command::new("diff")
        .args([
            "-u",
            "--label",
            &format!("{host}:{path}"),
            "--label",
            "render",
        ])
        .arg(tmp.join("host"))
        .arg(tmp.join("repo"))
        .output()?;
    Ok(String::from_utf8_lossy(&out.stdout).to_string())
}

/// Moves a file from the host into the state's `overrides/<host>/<path>`: the render picks it up.
pub fn adopt(st: &State, host: &str, path: &str) -> Result<String> {
    if !path.starts_with('/') {
        bail!("the path must be absolute");
    }
    let cur = fetch(st, host, path)?;
    let dst = st
        .dir
        .join("overrides")
        .join(host)
        .join(path.trim_start_matches('/'));
    std::fs::create_dir_all(dst.parent().unwrap())?;
    std::fs::write(&dst, cur)?;
    Ok(format!("overrides({host}): {path} from the host"))
}
