//! apply: guards -> copy over stdin -> config check -> atomic swap -> probe -> marker.
//! Hosts are ordered leaves first; rollback and series stop on failure.
use crate::plan::{self, HostPlan, MARKER, Status};
use crate::render::{self, Rendered};
use crate::ssh::Ssh;
use crate::state::*;
use anyhow::{Context, Result, bail};

#[derive(Default)]
pub struct Opts {
    pub host: Option<String>,
    pub branch: Option<String>,
    pub force: bool,
}

/// Hosts from leaves to the jump: the longer the via chain, the earlier.
pub fn order(st: &State, names: &[String]) -> Vec<String> {
    let mut v: Vec<(usize, String)> = names
        .iter()
        .map(|n| {
            (
                Ssh::jump_chain(st, n).map(|c| c.len()).unwrap_or(0),
                n.clone(),
            )
        })
        .collect();
    v.sort_by(|a, b| b.0.cmp(&a.0).then(a.1.cmp(&b.1)));
    v.into_iter().map(|(_, n)| n).collect()
}

pub fn guards(st: &State, opts: &Opts) -> Result<()> {
    if !crate::git::is_clean(&st.dir)? {
        bail!("dirty worktree: commit your changes before apply");
    }
    let branch = crate::git::branch(&st.dir)?;
    let want = opts.branch.as_deref().unwrap_or("main");
    if branch != want {
        bail!("branch {branch}, only {want} is applied (or pass --branch {branch} explicitly)");
    }
    Ok(())
}

/// Services to reload after a path changes.
pub fn service_for(path: &str) -> Option<String> {
    let p = path;
    if p.starts_with("/etc/sing-box/") {
        Some("sing-box".into())
    } else if p.starts_with("/etc/xray/") {
        Some("xray".into())
    } else if p == "/etc/caddy/Caddyfile" {
        Some("caddy".into())
    } else if p.starts_with("/etc/mtg/") {
        Some("mtg".into())
    } else if let Some(f) = p.strip_prefix("/etc/amnezia/amneziawg/") {
        Some(format!("awg-quick@{}", f.trim_end_matches(".conf")))
    } else if p == "/etc/nftables.conf" {
        Some("nftables".into())
    } else if p.starts_with("/etc/sysctl.d/") {
        Some("sysctl".into())
    } else if p.starts_with("/etc/systemd/system/") {
        Some("systemd".into())
    } else {
        None
    }
}

fn reload_cmd(svc: &str) -> String {
    match svc {
        "nftables" => "nft -f /etc/nftables.conf".into(),
        "sysctl" => "sysctl --system >/dev/null".into(),
        "systemd" => "systemctl daemon-reload".into(),
        "caddy" => {
            "systemctl enable -q caddy; systemctl reload caddy || systemctl restart caddy".into()
        }
        s => format!("systemctl enable -q {s}; systemctl restart {s}"),
    }
}

fn check_cmd(engine: Engine, path: &str) -> Option<String> {
    let tmp = format!("/tmp/.vpnctl{path}");
    match path {
        p if p == render::engine_config_path(Engine::SingBox) && engine == Engine::SingBox => {
            // rule sets are relative: ours are already in tmp, downloaded .srs come from the host
            Some(format!(
                "mkdir -p /tmp/.vpnctl/etc/sing-box/rules && cp -n /etc/sing-box/rules/*.srs /tmp/.vpnctl/etc/sing-box/rules/ 2>/dev/null; sing-box check -D /tmp/.vpnctl/etc/sing-box -c {tmp}"
            ))
        }
        p if p == render::engine_config_path(Engine::Xray) && engine == Engine::Xray => {
            Some(format!("xray run -test -c {tmp}"))
        }
        "/etc/nftables.conf" => Some(format!("nft -c -f {tmp}")),
        "/etc/caddy/Caddyfile" => Some(format!(
            "caddy validate --config {tmp} --adapter caddyfile >/dev/null"
        )),
        _ => None,
    }
}

/// tar with all host files; paths are relative to the root.
fn tarball(files: &render::HostFiles) -> Result<Vec<u8>> {
    let tmp = std::env::temp_dir().join(format!("vpnctl-tar-{}", std::process::id()));
    let _ = std::fs::remove_dir_all(&tmp);
    for (p, b) in files {
        let f = tmp.join(p.trim_start_matches('/'));
        std::fs::create_dir_all(f.parent().unwrap())?;
        std::fs::write(f, b)?;
    }
    let out = std::process::Command::new("tar")
        .arg("-C")
        .arg(&tmp)
        .args([
            "--sort=name",
            "--owner=0",
            "--group=0",
            "--mtime=@0",
            "-cf",
            "-",
            ".",
        ])
        .output()?;
    std::fs::remove_dir_all(&tmp)?;
    Ok(out.stdout)
}

fn mode_for(path: &str) -> &'static str {
    if path.starts_with("/etc/amnezia/")
        || path.starts_with("/etc/vpnctl/profiles/")
        || path.starts_with("/etc/sing-box/")
        || path.starts_with("/etc/xray/")
        || path.starts_with("/etc/mtg/")
    {
        "0600"
    } else {
        "0644"
    }
}

/// Install script: backup, swap, remove extras, restart affected services.
pub fn install_script(hp: &HostPlan, backup: &str) -> String {
    let mut s = String::from("set -e\n");
    s.push_str(&format!("mkdir -p {backup}\n"));
    let mut services = vec![];
    for (path, st) in &hp.files {
        match st {
            Status::Unchanged => continue,
            Status::Removed => {
                s.push_str(&format!(
                    "cp --parents -a '{path}' {backup} 2>/dev/null || true; rm -f '{path}'\n"
                ));
                if let Some(svc) = service_for(path).filter(|s| s.starts_with("awg-quick@")) {
                    s.push_str(&format!("systemctl disable --now -q {svc} || true\n"));
                    continue;
                }
            }
            Status::New => {
                s.push_str(&format!("echo '{path}' >> {backup}/.new; install -D -m {} '/tmp/.vpnctl{path}' '{path}'\n", mode_for(path)));
            }
            Status::Changed | Status::Drift => {
                s.push_str(&format!("cp --parents -a '{path}' {backup}; install -D -m {} '/tmp/.vpnctl{path}' '{path}'\n", mode_for(path)));
            }
        }
        let mut affected = service_for(path).into_iter().collect::<Vec<_>>();
        // a changed unit restarts its service, not just daemon-reload
        if let Some(unit) = path
            .strip_prefix("/etc/systemd/system/")
            .and_then(|u| u.strip_suffix(".service"))
        {
            affected.push(unit.to_string());
        }
        for svc in affected {
            if !services.contains(&svc) {
                services.push(svc);
            }
        }
    }
    // systemd and network before services, engine last
    services.sort_by_key(|s| match s.as_str() {
        "systemd" => 0,
        "sysctl" => 1,
        "nftables" => 2,
        x if x.starts_with("awg-quick@") => 3,
        "caddy" => 4,
        "mtg" => 6,
        _ => 5,
    });
    for svc in services {
        s.push_str(&reload_cmd(&svc));
        s.push('\n');
    }
    s.push_str("rm -rf /tmp/.vpnctl\necho ok\n");
    s
}

/// Rollback: restore the backup, drop new files, reload the same services.
pub fn rollback_script(hp: &HostPlan, backup: &str) -> String {
    let mut s = String::from("set +e\n");
    s.push_str(&format!(
        "if [ -f {backup}/.new ]; then xargs -r rm -f < {backup}/.new; fi\n"
    ));
    s.push_str(&format!("(cd {backup} && find . -type f ! -name .new | while read f; do install -D -m 0600 \"$f\" \"/${{f#./}}\"; done)\n"));
    let mut services: Vec<String> = hp
        .files
        .iter()
        .filter(|(_, st)| *st != Status::Unchanged)
        .filter_map(|(p, _)| service_for(p))
        .collect();
    services.sort();
    services.dedup();
    for svc in services {
        s.push_str(&reload_cmd(&svc));
        s.push('\n');
    }
    s
}

pub fn probe_script(role: Role) -> String {
    match role {
        Role::Entry => format!(
            "curl -sS -o /dev/null -w '%{{http_code}}' --max-time 10 --socks5-hostname 127.0.0.1:{} {}",
            render::PROBE_PORT,
            render::PROBE_URL
        ),
        // on an exit the engine is not in the probe path, so require it to be alive
        Role::Exit => format!(
            "systemctl is-active --quiet sing-box 2>/dev/null || systemctl is-active --quiet xray 2>/dev/null || exit 1; curl -sS -o /dev/null -w '%{{http_code}}' --max-time 10 {}",
            render::PROBE_URL
        ),
    }
}

/// Pre-swap checks on the host: certificate present, active link protocol alive.
fn preflight(
    st: &State,
    ssh: &Ssh,
    name: &str,
    files: &render::HostFiles,
    force: bool,
) -> Result<Vec<String>> {
    let mut notes = vec![];
    let (cert, _) = render::cert_paths(st, name);
    if files.values().any(|b| b.contains(&cert)) {
        let ok = ssh.run(&format!("test -s '{cert}'"), &[])?.status.success();
        if !ok {
            let msg = format!(
                "certificate {cert} is missing on the host: issue it first or wait for ACME"
            );
            if !force {
                bail!("{msg}");
            }
            notes.push(format!("{msg} · --force"));
        }
    }
    for (i, l) in render::out_links(st, name) {
        let Some(active) = l.active else { continue };
        let to = &st.hosts[&l.to];
        let port = st
            .inbounds
            .get(&l.to)
            .and_then(|m| m.get(&active))
            .map(|x| x.port)
            .unwrap_or(443);
        let check = match active {
            Proto::Awg => format!(
                "ip link show awg{i} >/dev/null 2>&1 || exit 0; h=$(awg show awg{i} latest-handshakes 2>/dev/null | awk '{{print $2}}' | head -1); [ -n \"$h\" ] && [ $(( $(date +%s) - h )) -lt 180 ]"
            ),
            // hysteria2 is UDP: a TCP connect to its port never succeeds. Liveness of the link
            // is checked by the chain probe after the configs are swapped.
            Proto::Hy2 => "true".into(),
            _ => format!(
                "timeout 5 bash -c 'exec 3<>/dev/tcp/{}/{port}' 2>/dev/null",
                to.ip
            ),
        };
        let ok = ssh.run(&check, &[])?.status.success();
        if !ok {
            let msg = format!(
                "link {}: active protocol {} is not responding",
                l.key(),
                active.name()
            );
            if !force {
                bail!("{msg} (use --force to apply anyway)");
            }
            notes.push(format!("{msg} · --force"));
        }
    }
    Ok(notes)
}

pub fn apply(st: &State, opts: &Opts) -> Result<()> {
    guards(st, opts)?;
    let head = crate::git::head(&st.dir)?;
    let rendered: Rendered = render::check(st)?;
    let plans = plan::plan(st, &rendered)?;
    let names: Vec<String> = plans
        .iter()
        .map(|p| p.name.clone())
        .filter(|n| opts.host.as_ref().is_none_or(|h| h == n))
        .collect();
    if names.is_empty() {
        bail!("nothing to apply: host not found");
    }
    let order = order(st, &names);
    println!(
        "plan for {} @ {head} · order: {}",
        crate::git::branch(&st.dir)?,
        order.join(" → ")
    );
    let mut done = 0;
    for name in &order {
        let hp = plans.iter().find(|p| &p.name == name).unwrap();
        let host = &st.hosts[name];
        println!("▸ {name}");
        if let Some(e) = &hp.unreachable {
            bail!("{name}: {e} — series stopped");
        }
        if !hp.bootstrapped {
            println!("  – skipped: bootstrap not done");
            continue;
        }
        let ssh = Ssh::for_host(st, name)?;
        let files = &rendered.hosts[name];
        if hp.changes() == 0 {
            ssh.sh(&format!("mkdir -p /etc/vpnctl && echo {head} > {MARKER}"))?;
            println!("  – no changes, marker {head} updated");
            done += 1;
            continue;
        }
        for n in preflight(st, &ssh, name, files, opts.force)? {
            println!("  ! {n}");
        }
        println!(
            "  ✓ render · {} files, {} changes",
            files.len(),
            hp.changes()
        );
        let tar = tarball(files)?;
        let out = ssh.run(
            "rm -rf /tmp/.vpnctl && mkdir -p /tmp/.vpnctl && tar -xf - -C /tmp/.vpnctl",
            &tar,
        )?;
        if !out.status.success() {
            bail!(
                "{name}: copy failed: {}",
                String::from_utf8_lossy(&out.stderr).trim()
            );
        }
        println!("  ✓ copied over stdin → /tmp/.vpnctl/");
        for (path, s) in &hp.files {
            if *s == Status::Unchanged || *s == Status::Removed {
                continue;
            }
            if let Some(c) = check_cmd(host.engine, path) {
                let out = ssh.run(&c, &[])?;
                if !out.status.success() {
                    ssh.run("rm -rf /tmp/.vpnctl", &[]).ok();
                    bail!(
                        "{name}: check of {path} failed:\n{}",
                        String::from_utf8_lossy(&out.stderr).trim()
                    );
                }
                println!(
                    "  ✓ {}",
                    c.split_whitespace().take(2).collect::<Vec<_>>().join(" ")
                );
            }
        }
        let backup = format!("/var/lib/vpnctl/backup/{head}");
        let out = ssh.run(&install_script(hp, &backup), &[])?;
        if !out.status.success() {
            eprintln!("{}", String::from_utf8_lossy(&out.stderr).trim());
            println!("  ✗ swap failed, rolled back to the backup");
            ssh.run(&rollback_script(hp, &backup), &[]).ok();
            bail!("{name}: swap failed — series stopped");
        }
        println!("  ✓ atomic swap · services reloaded");
        let code = ssh
            .run(&probe_script(host.role), &[])
            .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
            .unwrap_or_default();
        if code != "204" {
            println!(
                "  ✗ probe: {} instead of 204 · rollback",
                if code.is_empty() { "no answer" } else { &code }
            );
            let out = ssh.run(&rollback_script(hp, &backup), &[])?;
            if !out.status.success() {
                eprintln!("{}", String::from_utf8_lossy(&out.stderr).trim());
            }
            bail!("{name}: chain probe failed, config restored — series stopped");
        }
        println!(
            "  ✓ probe: 204 via {}",
            if host.role == Role::Entry {
                "the chain"
            } else {
                "the exit"
            }
        );
        ssh.sh(&format!("mkdir -p /etc/vpnctl && echo {head} > {MARKER}"))?;
        println!("  ✓ marker {head} written");
        done += 1;
    }
    if done > 0 && opts.host.is_none() {
        let ts = std::process::Command::new("date")
            .args(["-u", "+%Y-%m-%dT%H-%M-%S"])
            .output()
            .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
            .context("date")?;
        let tag = format!("applied/{ts}");
        crate::git::tag(&st.dir, &tag)?;
        println!("done {done} of {} · tag {tag}", order.len());
    } else {
        println!("done {done} of {}", order.len());
    }
    Ok(())
}
