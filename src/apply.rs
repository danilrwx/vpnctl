//! apply: guard'ы → копия через stdin → проверка конфигов → атомарная подмена → проба → маркер.
//! Порядок хостов от листьев к джамп-хосту; откат и стоп серии при неудаче.
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

/// Хосты от листьев к джампу: чем длиннее цепочка via, тем раньше.
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
        bail!("грязное дерево: закоммитьте изменения перед apply");
    }
    let branch = crate::git::branch(&st.dir)?;
    let want = opts.branch.as_deref().unwrap_or("main");
    if branch != want {
        bail!("ветка {branch}, применяется только {want} (или --branch {branch} явно)");
    }
    Ok(())
}

/// Службы, которые надо перечитать после изменения пути.
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
            // rule-set'ы относительные: свои уже в tmp, скачанные .srs подкладываем с хоста
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

/// tar со всеми файлами хоста; пути относительные от корня.
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

/// Скрипт установки: бэкап, подмена, удаление лишнего, перезапуск затронутых служб.
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
        // изменённый юнит перезапускает свою службу, а не только daemon-reload
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
    // systemd и сеть раньше служб, движок — последним
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

/// Откат: вернуть бэкап, удалить новые файлы, перечитать те же службы.
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
        // на выходе движок не в пути пробы, поэтому отдельно требуем, что он жив
        Role::Exit => format!(
            "systemctl is-active --quiet sing-box 2>/dev/null || systemctl is-active --quiet xray 2>/dev/null || exit 1; curl -sS -o /dev/null -w '%{{http_code}}' --max-time 10 {}",
            render::PROBE_URL
        ),
    }
}

/// Проверки на хосте до подмены: сертификат на месте, активный протокол линка жив.
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
            let msg =
                format!("сертификата {cert} нет на хосте: сначала C (выпуск) или дождитесь ACME");
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
            // hysteria2 — UDP: TCP-коннект к его порту не пройдёт никогда. Живость линка
            // проверит проба цепочки после подмены конфигов.
            Proto::Hy2 => "true".into(),
            _ => format!(
                "timeout 5 bash -c 'exec 3<>/dev/tcp/{}/{port}' 2>/dev/null",
                to.ip
            ),
        };
        let ok = ssh.run(&check, &[])?.status.success();
        if !ok {
            let msg = format!(
                "линк {}: активный протокол {} не отвечает",
                l.key(),
                active.name()
            );
            if !force {
                bail!("{msg} (--force чтобы применить всё равно)");
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
        bail!("нечего применять: хост не найден");
    }
    let order = order(st, &names);
    println!(
        "план для {} @ {head} · порядок: {}",
        crate::git::branch(&st.dir)?,
        order.join(" → ")
    );
    let mut done = 0;
    for name in &order {
        let hp = plans.iter().find(|p| &p.name == name).unwrap();
        let host = &st.hosts[name];
        println!("▸ {name}");
        if let Some(e) = &hp.unreachable {
            bail!("{name}: {e} — серия остановлена");
        }
        if !hp.bootstrapped {
            println!("  – пропущен: bootstrap не выполнен");
            continue;
        }
        let ssh = Ssh::for_host(st, name)?;
        let files = &rendered.hosts[name];
        if hp.changes() == 0 {
            ssh.sh(&format!("mkdir -p /etc/vpnctl && echo {head} > {MARKER}"))?;
            println!("  – без изменений, маркер {head} обновлён");
            done += 1;
            continue;
        }
        for n in preflight(st, &ssh, name, files, opts.force)? {
            println!("  ! {n}");
        }
        println!(
            "  ✓ рендер · {} файлов, изменений {}",
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
                "{name}: копирование не удалось: {}",
                String::from_utf8_lossy(&out.stderr).trim()
            );
        }
        println!("  ✓ копирование через stdin → /tmp/.vpnctl/");
        for (path, s) in &hp.files {
            if *s == Status::Unchanged || *s == Status::Removed {
                continue;
            }
            if let Some(c) = check_cmd(host.engine, path) {
                let out = ssh.run(&c, &[])?;
                if !out.status.success() {
                    ssh.run("rm -rf /tmp/.vpnctl", &[]).ok();
                    bail!(
                        "{name}: проверка {path} не прошла:\n{}",
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
            println!("  ✗ подмена не удалась, откат на бэкап");
            ssh.run(&rollback_script(hp, &backup), &[]).ok();
            bail!("{name}: подмена не удалась — серия остановлена");
        }
        println!("  ✓ атомарная подмена · службы перечитаны");
        let code = ssh
            .run(&probe_script(host.role), &[])
            .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
            .unwrap_or_default();
        if code != "204" {
            println!(
                "  ✗ проба: {} вместо 204 · откат",
                if code.is_empty() {
                    "нет ответа"
                } else {
                    &code
                }
            );
            let out = ssh.run(&rollback_script(hp, &backup), &[])?;
            if !out.status.success() {
                eprintln!("{}", String::from_utf8_lossy(&out.stderr).trim());
            }
            bail!("{name}: проба цепочки не прошла, конфиг возвращён — серия остановлена");
        }
        println!(
            "  ✓ проба: 204 через {}",
            if host.role == Role::Entry {
                "цепочку"
            } else {
                "выход"
            }
        );
        ssh.sh(&format!("mkdir -p /etc/vpnctl && echo {head} > {MARKER}"))?;
        println!("  ✓ маркер {head} записан");
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
        println!("готово {done} из {} · тег {tag}", order.len());
    } else {
        println!("готово {done} из {}", order.len());
    }
    Ok(())
}
