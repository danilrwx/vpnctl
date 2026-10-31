//! Host bootstrap: one idempotent shell script from the template, executed over SSH.
use crate::render;
use crate::ssh::Ssh;
use crate::state::*;
use anyhow::{Context, Result};
use minijinja::context;

pub const SING_BOX_URL: &str =
    "https://github.com/SagerNet/sing-box/releases/download/v{v}/sing-box-{v}-linux-amd64.tar.gz";
pub const XRAY_URL: &str =
    "https://github.com/XTLS/Xray-core/releases/download/v{v}/Xray-linux-64.zip";
pub const CADDY_URL: &str =
    "https://github.com/caddyserver/caddy/releases/download/v{v}/caddy_{v}_linux_amd64.tar.gz";
pub const MTG_URL: &str =
    "https://github.com/9seconds/mtg/releases/download/v{v}/mtg-{v}-linux-amd64.tar.gz";

/// Local rule set tags from the rendered sing-box config.json.
pub fn rule_set_tags(config_json: &str) -> Vec<String> {
    let v: serde_json::Value = serde_json::from_str(config_json).unwrap_or_default();
    let mut tags: Vec<String> = v["route"]["rule_set"]
        .as_array()
        .map(|a| {
            a.iter()
                .filter(|r| r["format"] == "binary")
                .filter_map(|r| r["tag"].as_str().map(String::from))
                .collect()
        })
        .unwrap_or_default();
    tags.sort();
    tags.dedup();
    tags
}

/// Rule set URL: `geosite-x` -> sing-geosite, `geoip-x` -> sing-geoip; `latest` or a release tag.
pub fn rule_set_url(tag: &str, release: &str) -> String {
    let repo = if tag.starts_with("geoip-") {
        "sing-geoip"
    } else {
        "sing-geosite"
    };
    if release == "latest" {
        format!("https://raw.githubusercontent.com/SagerNet/{repo}/rule-set/{tag}.srs")
    } else {
        format!("https://github.com/SagerNet/{repo}/releases/download/{release}/{tag}.srs")
    }
}

/// Auto-reboot days: odd for the entry, even for exits.
pub fn reboot_days(role: Role) -> String {
    let start = if role == Role::Entry { 1 } else { 2 };
    (start..=31)
        .step_by(2)
        .map(|d| format!("{d:02}"))
        .collect::<Vec<_>>()
        .join(",")
}

fn pin<'a>(st: &'a State, name: &str) -> Result<&'a Pin> {
    st.versions
        .get(name)
        .with_context(|| format!("versions.yaml has no «{name}»"))
}

fn download(name: &str, p: &Pin, url: &str, check: String, install: String) -> minijinja::Value {
    let v = &p.version;
    let url = url.replace("{v}", v);
    let file = url.rsplit('/').next().unwrap_or(name).to_string();
    context! { name => name, version => v.clone(), url => url, sha => p.sha256.clone().unwrap_or_default(), file => file, check => check, install => install }
}

/// Bootstrap script for a host: a pure function of the state and the render.
pub fn script(st: &State, host: &str) -> Result<String> {
    let h = st.host(host)?;
    let files = render::render_host(st, host)?;
    let env = render::env(st)?;
    let mut downloads = vec![];
    let dl = "/var/lib/vpnctl/dl";
    match h.engine {
        Engine::SingBox => {
            let p = pin(st, "sing-box")?;
            let v = &p.version;
            downloads.push(download("sing-box", p, SING_BOX_URL, format!("sing-box version 2>/dev/null | grep -q 'version {v}$'"), format!("tar -xzf {dl}/sing-box-{v}-linux-amd64.tar.gz -C {dl} && install -m 0755 {dl}/sing-box-{v}-linux-amd64/sing-box /usr/local/bin/sing-box && mkdir -p /etc/sing-box/rules")));
        }
        Engine::Xray => {
            let p = pin(st, "xray")?;
            let v = &p.version;
            downloads.push(download("xray", p, XRAY_URL, format!("xray version 2>/dev/null | grep -q '^Xray {v} '"), format!("rm -rf {dl}/xray && mkdir -p {dl}/xray && unzip -qo {dl}/Xray-linux-64.zip -d {dl}/xray && install -m 0755 {dl}/xray/xray /usr/local/bin/xray && mkdir -p /etc/xray")));
            // unlike sing-box, xray reads geosite.dat/geoip.dat from the binary directory
            let g = pin(st, "geodata")?;
            for f in ["geosite", "geoip"] {
                let url = format!(
                    "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/{{v}}/download/{f}.dat"
                );
                downloads.push(download(
                    f,
                    g,
                    &url,
                    format!("test -s /usr/local/bin/{f}.dat"),
                    format!("install -m 0644 {dl}/{f}.dat /usr/local/bin/{f}.dat"),
                ));
            }
        }
    }
    if h.role == Role::Entry {
        let p = pin(st, "caddy")?;
        let v = &p.version;
        downloads.push(download("caddy", p, CADDY_URL, format!("caddy version 2>/dev/null | grep -q '^v{v} '"), format!("tar -xzf {dl}/caddy_{v}_linux_amd64.tar.gz -C {dl} caddy && install -m 0755 {dl}/caddy /usr/local/bin/caddy")));
        let p = pin(st, "mtg")?;
        let v = &p.version;
        downloads.push(download("mtg", p, MTG_URL, format!("mtg --version 2>/dev/null | grep -q '{v}'"), format!("tar -xzf {dl}/mtg-{v}-linux-amd64.tar.gz -C {dl} && install -m 0755 {dl}/mtg-{v}-linux-amd64/mtg /usr/local/bin/mtg")));
    }
    let geodata = pin(st, "geodata")?.version.clone();
    let rule_sets: Vec<minijinja::Value> = match h.engine {
        Engine::SingBox => rule_set_tags(&files[&render::engine_config_path(Engine::SingBox)])
            .iter()
            .map(|t| context! { tag => t.clone(), url => rule_set_url(t, &geodata) })
            .collect(),
        Engine::Xray => vec![],
    };
    let mut units: Vec<minijinja::Value> = vec![];
    let mut enable: Vec<String> = vec![];
    for (path, body) in &files {
        if let Some(name) = path.strip_prefix("/etc/systemd/system/") {
            units.push(context! { name => name.to_string(), body => body.clone() });
            enable.push(name.to_string());
        }
        if let Some(f) = path.strip_prefix("/etc/amnezia/amneziawg/") {
            enable.push(format!("awg-quick@{}", f.trim_end_matches(".conf")));
        }
    }
    if h.role == Role::Entry {
        enable.push("caddy".into());
    }
    enable.sort();
    // AWG is needed when there is an awg inbound or an awg link involving the host
    let awg = st
        .inbounds
        .get(host)
        .is_some_and(|m| m.contains_key(&Proto::Awg))
        || st
            .links
            .iter()
            .any(|l| (l.from == host || l.to == host) && l.protocols.contains(&Proto::Awg));
    let commit = crate::git::head(&st.dir).unwrap_or_else(|_| "-".into());
    let role = if h.role == Role::Entry {
        "entry"
    } else {
        "exit"
    };
    Ok(env.get_template("bootstrap.sh.j2")?.render(context! {
        host => host, role => role, domain => h.domain.clone().unwrap_or_default(),
        reboot_days => reboot_days(h.role), downloads => downloads, rule_sets => rule_sets, geodata => geodata,
        awg => awg, awg_version => pin(st, "awg")?.version.clone(),
        nftables => files.get("/etc/nftables.conf").cloned().unwrap_or_default(),
        sysctl => files.get("/etc/sysctl.d/90-vpnctl.conf").cloned().unwrap_or_default(),
        units => units, enable => enable, commit => commit,
    })?)
}

/// Records the host key (if missing), then runs the script over SSH with live output.
pub fn run(st: &mut State, host: &str, ssh_override: Option<String>) -> Result<()> {
    if let Some(s) = ssh_override {
        st.hosts
            .get_mut(host)
            .with_context(|| format!("host «{host}» is not described"))?
            .ssh = s;
    }
    let h = st.host(host)?.clone();
    if h.host_key.is_none() {
        let key = Ssh::scan_key_in_state(st, host)?;
        println!(
            "  ✓ host key {} accepted, recorded in the state",
            key.split_whitespace()
                .nth(1)
                .map(|k| format!("{}…", &k[..12.min(k.len())]))
                .unwrap_or_default()
        );
        st.hosts.get_mut(host).unwrap().host_key = Some(key);
        st.save()?;
        crate::git::commit(&st.dir, &format!("hosts: {host}: host key"))?;
    }
    let script = script(st, host)?;
    println!("▸ bootstrap {host} · {}", h.ssh);
    let status = Ssh::for_host(st, host)?.stream("sh -s", script.as_bytes())?;
    if !status.success() {
        anyhow::bail!("bootstrap {host} failed; a re-run will finish the rest");
    }
    Ok(())
}
