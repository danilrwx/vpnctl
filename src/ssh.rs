//! Системный `ssh`: `-J` по графу `via`, ControlMaster, host key из состояния.
use crate::state::State;
use anyhow::{Context, Result, bail};
use std::path::PathBuf;
use std::process::{Command, Output, Stdio};

pub struct Ssh {
    pub target: String,
    pub jumps: Vec<String>,
    known_hosts: PathBuf,
    strict: bool,
}

impl Ssh {
    /// Цепочка джампов: via хоста, via его джампа и т.д., ближний к нам первый.
    pub fn jump_chain(st: &State, host: &str) -> Result<Vec<String>> {
        let mut chain = vec![];
        let mut cur = st.host(host)?.via.clone();
        while let Some(v) = cur {
            if chain.contains(&v) || v == host {
                bail!("цикл в графе via у хоста {host}");
            }
            cur = st.host(&v)?.via.clone();
            chain.push(v);
        }
        chain.reverse();
        Ok(chain)
    }

    pub fn for_host(st: &State, host: &str) -> Result<Ssh> {
        let h = st.host(host)?;
        let jumps = Self::jump_chain(st, host)?
            .iter()
            .map(|j| st.hosts[j].ssh.clone())
            .collect();
        // known_hosts из состояния: все хосты с записанным ключом
        let dir = std::env::temp_dir().join(format!("vpnctl-{}", std::process::id()));
        std::fs::create_dir_all(&dir)?;
        let known: String = st
            .hosts
            .values()
            .filter_map(|x| x.host_key.as_ref().map(|k| format!("{} {k}\n", x.ip)))
            .collect();
        let known_hosts = dir.join("known_hosts");
        std::fs::write(&known_hosts, known)?;
        Ok(Ssh {
            target: h.ssh.clone(),
            jumps,
            known_hosts,
            strict: h.host_key.is_some(),
        })
    }

    fn cmd(&self) -> Command {
        let mut c = Command::new("ssh");
        c.args([
            "-o",
            "BatchMode=yes",
            "-o",
            "ConnectTimeout=10",
            "-o",
            "ServerAliveInterval=15",
            "-o",
            "ServerAliveCountMax=4",
        ])
        .args([
            "-o",
            "ControlMaster=auto",
            "-o",
            "ControlPath=~/.ssh/vpnctl-%C",
            "-o",
            "ControlPersist=120",
        ])
        .arg("-o")
        .arg(format!("UserKnownHostsFile={}", self.known_hosts.display()))
        .args([
            "-o",
            if self.strict {
                "StrictHostKeyChecking=yes"
            } else {
                "StrictHostKeyChecking=accept-new"
            },
        ]);
        if !self.jumps.is_empty() {
            c.arg("-J").arg(self.jumps.join(","));
        }
        c.arg(&self.target);
        c
    }

    /// Готовая команда `ssh … <cmd>` для запуска снаружи (TUI: журнал, обновление ОС).
    pub fn command(&self, cmd: &str) -> Command {
        let mut c = self.cmd();
        c.arg(cmd);
        c
    }

    /// Одна строка для удалённого шелла: ssh склеивает аргументы пробелами, поэтому скрипт
    /// надо завернуть в одинарные кавычки самим.
    pub fn quote(script: &str) -> String {
        format!("sh -c '{}'", script.replace('\'', "'\\''"))
    }

    /// Выполняет скрипт через `sh -c` на хосте, stdin — данные для скрипта (или пусто).
    pub fn run(&self, script: &str, stdin: &[u8]) -> Result<Output> {
        use std::io::Write;
        let mut child = self
            .cmd()
            .arg(Self::quote(script))
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .context("запуск ssh")?;
        child.stdin.take().unwrap().write_all(stdin).ok();
        Ok(child.wait_with_output()?)
    }

    /// Выполняет команду, stdout/stderr идут оператору как есть; stdin — данные.
    pub fn stream(&self, cmd: &str, stdin: &[u8]) -> Result<std::process::ExitStatus> {
        use std::io::Write;
        let mut child = self
            .cmd()
            .arg(cmd)
            .stdin(Stdio::piped())
            .stdout(Stdio::inherit())
            .stderr(Stdio::inherit())
            .spawn()
            .context("запуск ssh")?;
        child.stdin.take().unwrap().write_all(stdin).ok();
        Ok(child.wait()?)
    }

    /// Как `run`, но ошибка при ненулевом статусе.
    pub fn sh(&self, script: &str) -> Result<String> {
        let out = self.run(script, &[])?;
        if !out.status.success() {
            bail!(
                "{}: {}",
                self.target,
                String::from_utf8_lossy(&out.stderr).trim()
            );
        }
        Ok(String::from_utf8_lossy(&out.stdout).to_string())
    }

    pub fn reachable(&self) -> Result<(), String> {
        match self.run("true", &[]) {
            Ok(o) if o.status.success() => Ok(()),
            Ok(o) => Err(String::from_utf8_lossy(&o.stderr)
                .lines()
                .last()
                .unwrap_or("нет ответа")
                .to_string()),
            Err(e) => Err(e.to_string()),
        }
    }

    /// Публичный ключ хоста как в known_hosts (без имени хоста).
    pub fn scan_key(ip: &str) -> Result<String> {
        let out = Command::new("ssh-keyscan")
            .args(["-t", "ed25519", "-T", "5", ip])
            .stderr(Stdio::null())
            .output()?;
        String::from_utf8_lossy(&out.stdout)
            .lines()
            .find(|l| !l.starts_with('#'))
            .and_then(|l| l.split_once(' ').map(|(_, k)| k.to_string()))
            .context("ssh-keyscan ничего не вернул")
    }

    /// Публичный ключ хоста, снятый через его джамп: хост может быть виден только оттуда
    /// (типичный случай — нода, доступная лишь по IPv6 из сети джампа).
    pub fn scan_key_in_state(st: &State, host: &str) -> Result<String> {
        let h = st.host(host)?;
        let ip = h.ip.to_string();
        let Some(jump_name) = Self::jump_chain(st, host)?.first().cloned() else {
            return Self::scan_key(&ip);
        };
        let jump = Self::for_host(st, &jump_name)?;
        let out = jump.run(
            &format!("ssh-keyscan -t ed25519 -T 5 {ip} 2>/dev/null"),
            &[],
        )?;
        String::from_utf8_lossy(&out.stdout)
            .lines()
            .find(|l| !l.starts_with('#') && !l.trim().is_empty())
            .and_then(|l| l.split_once(' ').map(|(_, k)| k.to_string()))
            .with_context(|| format!("ssh-keyscan {ip} через джамп {jump_name} ничего не вернул"))
    }
}
