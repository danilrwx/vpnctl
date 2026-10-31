//! Metrics and probes: pulled over SSH into a local SQLite, topology matrix, `status --json` summary.
//! Parsers and delta math are pure functions; SSH only in `collect`, `probe` and `matrix`.
use crate::plan;
use crate::render::{self, PROBE_PORT, PROBE_URL};
use crate::ssh::Ssh;
use crate::state::*;
use anyhow::{Context, Result};
use base64::Engine as _;
use rusqlite::{Connection, OptionalExtension, params};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::path::Path;
use std::time::{Duration, Instant};

pub const DB_FILE: &str = "metrics.sqlite";
pub const RETENTION_DAYS: i64 = 60;
/// An older awg handshake means the link is considered dead (same as the apply preflight).
pub const AWG_HANDSHAKE_MAX: i64 = 180;
pub const LAPTOP: &str = "laptop";
const DAY: i64 = 86_400;

pub fn now() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// Engine gRPC StatsService address: xray `api`; sing-box only in builds with `with_v2ray_api`
/// (the official build lacks it, so per-user traffic is not collected).
pub fn stats_addr(e: Engine) -> &'static str {
    match e {
        Engine::SingBox => "127.0.0.1:8080",
        Engine::Xray => "127.0.0.1:10085",
    }
}

// ---------- gRPC / protobuf ----------

fn varint(b: &[u8], i: &mut usize) -> Option<u64> {
    let mut v = 0u64;
    let mut shift = 0;
    loop {
        let x = *b.get(*i)?;
        *i += 1;
        v |= u64::from(x & 0x7f) << shift;
        if x & 0x80 == 0 {
            return Some(v);
        }
        shift += 7;
        if shift > 63 {
            return None;
        }
    }
}

enum Field<'a> {
    Varint(u64),
    Bytes(&'a [u8]),
}

/// Fields of one protobuf message: (number, value); fixed fields are skipped.
fn fields(b: &[u8]) -> Option<Vec<(u64, Field<'_>)>> {
    let mut out = vec![];
    let mut i = 0;
    while i < b.len() {
        let key = varint(b, &mut i)?;
        match key & 7 {
            0 => out.push((key >> 3, Field::Varint(varint(b, &mut i)?))),
            2 => {
                let n = varint(b, &mut i)? as usize;
                out.push((key >> 3, Field::Bytes(b.get(i..i + n)?)));
                i += n;
            }
            1 => i += 8,
            5 => i += 4,
            _ => return None,
        }
    }
    Some(out)
}

/// Body of a gRPC `QueryStats` response (frames: flag, BE length, `QueryStatsResponse{repeated Stat{name=1, value=2}}`)
/// -> counter name -> value. An empty body or a compressed frame yields None.
pub fn parse_stats(body: &[u8]) -> Option<BTreeMap<String, u64>> {
    if body.is_empty() {
        return None;
    }
    let mut map = BTreeMap::new();
    let mut i = 0;
    while i + 5 <= body.len() {
        if body[i] != 0 {
            return None;
        }
        let n = u32::from_be_bytes(body[i + 1..i + 5].try_into().ok()?) as usize;
        let msg = body.get(i + 5..i + 5 + n)?;
        i += 5 + n;
        for (num, f) in fields(msg)? {
            let (1, Field::Bytes(stat)) = (num, f) else {
                continue;
            };
            let mut name = None;
            let mut value = 0;
            for (num, f) in fields(stat)? {
                match (num, f) {
                    (1, Field::Bytes(s)) => name = Some(String::from_utf8_lossy(s).to_string()),
                    (2, Field::Varint(v)) => value = v,
                    _ => {}
                }
            }
            if let Some(n) = name {
                map.insert(n, value);
            }
        }
    }
    Some(map)
}

/// Counter readings: (kind, name) -> (up, down) in bytes.
pub type Counters = BTreeMap<(String, String), (u64, u64)>;

/// `user>>>brat-split-reality>>>traffic>>>uplink` → (("user", "brat-split-reality"), (up, down)).
pub fn fold_counters(stats: &BTreeMap<String, u64>) -> Counters {
    let mut m = Counters::new();
    for (k, v) in stats {
        let p: Vec<&str> = k.split(">>>").collect();
        if p.len() != 4 || p[2] != "traffic" {
            continue;
        }
        let e = m
            .entry((p[0].to_string(), p[1].to_string()))
            .or_insert((0, 0));
        match p[3] {
            "uplink" => e.0 += v,
            "downlink" => e.1 += v,
            _ => {}
        }
    }
    m
}

/// Counter delta: after an engine restart the counter starts at zero, then the delta is the whole reading.
pub fn delta(prev: Option<u64>, cur: u64) -> u64 {
    match prev {
        Some(p) if cur >= p => cur - p,
        _ => cur,
    }
}

// ---------- host script ----------

#[derive(Debug, Default, Clone, PartialEq, Serialize, Deserialize)]
pub struct HostMetrics {
    pub load1: Option<f64>,
    pub mem_used: Option<u64>,
    pub mem_total: Option<u64>,
    pub disk_used: Option<u64>,
    pub disk_total: Option<u64>,
    pub conns: Option<u64>,
    pub uptime: Option<u64>,
    pub updates: Option<u64>,
    pub reboot: Option<bool>,
    /// Certificate expiry, epoch.
    pub cert_expires: Option<i64>,
    pub probe_code: Option<u16>,
    pub probe_ms: Option<u64>,
    /// Engine counter readings; None means the stats API did not answer.
    #[serde(skip)]
    pub counters: Option<Counters>,
}

/// Chain probe: status code and response time of `generate_204` (entry via the service socks).
pub fn probe_cmd(role: Role) -> String {
    let via = match role {
        Role::Entry => format!("--socks5-hostname 127.0.0.1:{PROBE_PORT} "),
        Role::Exit => String::new(),
    };
    format!(
        "curl -sS -o /dev/null -w '%{{http_code}} %{{time_total}}' --max-time 10 {via}{PROBE_URL} 2>/dev/null"
    )
}

/// gRPC `QueryStats` without a pattern via curl: the body is an empty frame (5 zero bytes), the reply is base64.
pub fn stats_cmd(engine: Engine) -> String {
    format!(
        "printf '\\0\\0\\0\\0\\0' | curl -s --max-time 5 --http2-prior-knowledge -H 'content-type: application/grpc' -H 'te: trailers' --data-binary @- http://{}/v2ray.core.app.stats.command.StatsService/QueryStats | base64 -w0",
        stats_addr(engine)
    )
}

/// One call per host, `key=value` lines parsed by `parse_host`.
pub fn host_script(engine: Engine, role: Role, cert: &str) -> String {
    format!(
        r#"echo "load=$(cut -d' ' -f1 /proc/loadavg)"
awk '/MemTotal/{{t=$2}} /MemAvailable/{{a=$2}} END{{print "mem=" (t-a)*1024, t*1024}}' /proc/meminfo
df -kP / | awk 'NR==2{{print "disk=" $3*1024, $2*1024}}'
echo "conns=$(ss -Htn state established 2>/dev/null | wc -l)"
echo "uptime=$(cut -d. -f1 /proc/uptime)"
echo "updates=$(apt-get -s upgrade 2>/dev/null | grep -c '^Inst ')"
if [ -e /var/run/reboot-required ]; then echo reboot=yes; else echo reboot=no; fi
e=$(openssl x509 -enddate -noout -in '{cert}' 2>/dev/null | cut -d= -f2); if [ -n "$e" ]; then echo "cert=$(date -d "$e" +%s)"; fi
echo "probe=$({probe})"
echo "stats=$({stats})"
"#,
        probe = probe_cmd(role),
        stats = stats_cmd(engine)
    )
}

pub fn parse_host(out: &str) -> HostMetrics {
    let mut m = HostMetrics::default();
    for line in out.lines() {
        let Some((k, v)) = line.split_once('=') else {
            continue;
        };
        let mut w = v.split_whitespace();
        let mut n = || w.next().and_then(|x| x.parse::<u64>().ok());
        match k {
            "load" => m.load1 = v.trim().parse().ok(),
            "mem" => (m.mem_used, m.mem_total) = (n(), n()),
            "disk" => (m.disk_used, m.disk_total) = (n(), n()),
            "conns" => m.conns = n(),
            "uptime" => m.uptime = n(),
            "updates" => m.updates = n(),
            "reboot" => m.reboot = Some(v.trim() == "yes"),
            "cert" => m.cert_expires = v.trim().parse().ok(),
            "probe" => {
                m.probe_code = n().filter(|c| *c > 0).map(|c| c as u16);
                m.probe_ms = w
                    .next()
                    .and_then(|s| s.parse::<f64>().ok())
                    .map(|s| (s * 1000.0).round() as u64)
                    .filter(|_| m.probe_code.is_some());
            }
            "stats" => {
                m.counters = base64::engine::general_purpose::STANDARD
                    .decode(v.trim())
                    .ok()
                    .and_then(|b| parse_stats(&b))
                    .map(|s| fold_counters(&s))
            }
            _ => {}
        }
    }
    m
}

// ---------- database ----------

/// One host sample: reachability and marker from `plan`, machine data from the script.
#[derive(Debug, Default, Clone, PartialEq, Serialize, Deserialize)]
pub struct HostSample {
    pub ts: i64,
    pub reachable: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    #[serde(default)]
    pub marker: Option<String>,
    #[serde(default)]
    pub drift: Option<bool>,
    #[serde(default)]
    pub ssh_ms: Option<u64>,
    #[serde(flatten)]
    pub m: HostMetrics,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Edge {
    pub src: String,
    pub dst: String,
    pub proto: String,
    pub ok: bool,
    pub ms: Option<u64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub note: Option<String>,
}

#[derive(Debug, Default, Clone, Copy, PartialEq, Serialize)]
pub struct Traffic {
    pub up: u64,
    pub down: u64,
}

/// Probe history point: (ts, status code, ms).
pub type ProbePoint = (i64, Option<u16>, Option<u64>);

pub struct Db(Connection);

fn traffic(up: i64, down: i64) -> Traffic {
    Traffic {
        up: up as u64,
        down: down as u64,
    }
}

impl Db {
    /// Opens (creates) `metrics.sqlite` in the state directory.
    pub fn open(dir: &Path) -> Result<Db> {
        let p = dir.join(DB_FILE);
        let c = Connection::open(&p).with_context(|| format!("open {}", p.display()))?;
        // the host sample is JSON in one column (fields change, queries go by host and time);
        // traffic is stored in columns to allow SUM over windows.
        c.execute_batch(
            "CREATE TABLE IF NOT EXISTS samples(ts INTEGER NOT NULL, host TEXT NOT NULL, kind TEXT NOT NULL, data TEXT NOT NULL);
             CREATE INDEX IF NOT EXISTS samples_host ON samples(host, kind, ts);
             CREATE TABLE IF NOT EXISTS traffic(ts INTEGER NOT NULL, host TEXT NOT NULL, kind TEXT NOT NULL, name TEXT NOT NULL,
                 up INTEGER NOT NULL, down INTEGER NOT NULL, up_total INTEGER NOT NULL, down_total INTEGER NOT NULL);
             CREATE INDEX IF NOT EXISTS traffic_name ON traffic(kind, name, ts);
             CREATE TABLE IF NOT EXISTS totals(host TEXT NOT NULL, kind TEXT NOT NULL, name TEXT NOT NULL,
                 up INTEGER NOT NULL, down INTEGER NOT NULL, PRIMARY KEY(host, kind, name));
             CREATE TABLE IF NOT EXISTS edges(ts INTEGER NOT NULL, src TEXT NOT NULL, dst TEXT NOT NULL, proto TEXT NOT NULL,
                 ok INTEGER NOT NULL, ms INTEGER, note TEXT);",
        )?;
        Ok(Db(c))
    }

    /// `kind`: `collect` is a full sample, `probe` is the chain probe only.
    pub fn sample(&self, host: &str, kind: &str, s: &HostSample) -> Result<()> {
        self.0.execute(
            "INSERT INTO samples(ts, host, kind, data) VALUES(?1, ?2, ?3, ?4)",
            params![s.ts, host, kind, serde_json::to_string(s)?],
        )?;
        Ok(())
    }

    /// Last host sample: by kind, or (kind None) the last one with a probe result.
    pub fn last_sample(&self, host: &str, kind: Option<&str>) -> Result<Option<HostSample>> {
        let cond = match kind {
            Some(_) => "kind = ?2",
            None => "?2 IS NULL AND json_extract(data, '$.probe_code') IS NOT NULL",
        };
        let data: Option<String> = self
            .0
            .query_row(
                &format!(
                    "SELECT data FROM samples WHERE host = ?1 AND {cond} ORDER BY ts DESC LIMIT 1"
                ),
                params![host, kind],
                |r| r.get(0),
            )
            .optional()?;
        Ok(data.map(|d| serde_json::from_str(&d)).transpose()?)
    }

    /// Counter deltas against the previous sample of the same host; totals accumulate in `totals`.
    pub fn traffic(&self, ts: i64, host: &str, counters: &Counters) -> Result<()> {
        for ((kind, name), (up_t, down_t)) in counters {
            let prev: Option<(u64, u64)> = self
                .0
                .query_row(
                    "SELECT up_total, down_total FROM traffic WHERE host = ?1 AND kind = ?2 AND name = ?3 ORDER BY ts DESC LIMIT 1",
                    params![host, kind, name],
                    |r| Ok((r.get::<_, i64>(0)? as u64, r.get::<_, i64>(1)? as u64)),
                )
                .optional()?;
            let up = delta(prev.map(|p| p.0), *up_t);
            let down = delta(prev.map(|p| p.1), *down_t);
            self.0.execute(
                "INSERT INTO traffic VALUES(?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)",
                params![
                    ts,
                    host,
                    kind,
                    name,
                    up as i64,
                    down as i64,
                    *up_t as i64,
                    *down_t as i64
                ],
            )?;
            self.0.execute(
                "INSERT INTO totals VALUES(?1, ?2, ?3, ?4, ?5) ON CONFLICT(host, kind, name) DO UPDATE SET up = up + excluded.up, down = down + excluded.down",
                params![host, kind, name, up as i64, down as i64],
            )?;
        }
        Ok(())
    }

    /// Sum of deltas by name since `since` across all hosts.
    pub fn sum(&self, kind: &str, name: &str, since: i64) -> Result<Traffic> {
        Ok(self.0.query_row(
            "SELECT COALESCE(SUM(up), 0), COALESCE(SUM(down), 0) FROM traffic WHERE kind = ?1 AND name = ?2 AND ts >= ?3",
            params![kind, name, since],
            |r| Ok(traffic(r.get(0)?, r.get(1)?)),
        )?)
    }

    pub fn total(&self, kind: &str, name: &str) -> Result<Traffic> {
        Ok(self.0.query_row(
            "SELECT COALESCE(SUM(up), 0), COALESCE(SUM(down), 0) FROM totals WHERE kind = ?1 AND name = ?2",
            params![kind, name],
            |r| Ok(traffic(r.get(0)?, r.get(1)?)),
        )?)
    }

    /// Outbound traffic since `since`: host -> tag -> traffic.
    pub fn outbounds(&self, since: i64) -> Result<BTreeMap<String, BTreeMap<String, Traffic>>> {
        let mut st = self.0.prepare(
            "SELECT host, name, SUM(up), SUM(down) FROM traffic WHERE kind = 'outbound' AND ts >= ?1 GROUP BY host, name",
        )?;
        let rows = st.query_map(params![since], |r| {
            Ok((
                r.get::<_, String>(0)?,
                r.get::<_, String>(1)?,
                traffic(r.get(2)?, r.get(3)?),
            ))
        })?;
        let mut out: BTreeMap<String, BTreeMap<String, Traffic>> = BTreeMap::new();
        for r in rows {
            let (h, n, t) = r?;
            out.entry(h).or_default().insert(n, t);
        }
        Ok(out)
    }

    pub fn edges(&self, ts: i64, edges: &[Edge]) -> Result<()> {
        for e in edges {
            self.0.execute(
                "INSERT INTO edges VALUES(?1, ?2, ?3, ?4, ?5, ?6, ?7)",
                params![
                    ts,
                    e.src,
                    e.dst,
                    e.proto,
                    e.ok,
                    e.ms.map(|m| m as i64),
                    e.note
                ],
            )?;
        }
        Ok(())
    }

    /// Last matrix run: time and edges.
    pub fn last_edges(&self) -> Result<(Option<i64>, Vec<Edge>)> {
        let ts: Option<i64> = self
            .0
            .query_row("SELECT MAX(ts) FROM edges", [], |r| r.get(0))?;
        let Some(ts) = ts else {
            return Ok((None, vec![]));
        };
        let mut st = self.0.prepare(
            "SELECT src, dst, proto, ok, ms, note FROM edges WHERE ts = ?1 ORDER BY rowid",
        )?;
        let rows = st.query_map(params![ts], |r| {
            Ok(Edge {
                src: r.get(0)?,
                dst: r.get(1)?,
                proto: r.get(2)?,
                ok: r.get(3)?,
                ms: r.get::<_, Option<i64>>(4)?.map(|m| m as u64),
                note: r.get(5)?,
            })
        })?;
        Ok((Some(ts), rows.collect::<Result<_, _>>()?))
    }

    /// Chain probe history of a host since `since`: (ts, code, ms).
    pub fn probe_history(&self, host: &str, since: i64) -> Result<Vec<ProbePoint>> {
        let mut st = self.0.prepare(
            "SELECT ts, json_extract(data, '$.probe_code'), json_extract(data, '$.probe_ms') FROM samples WHERE host = ?1 AND ts >= ?2 ORDER BY ts",
        )?;
        let rows = st.query_map(params![host, since], |r| {
            Ok((
                r.get(0)?,
                r.get::<_, Option<i64>>(1)?.map(|c| c as u16),
                r.get::<_, Option<i64>>(2)?.map(|m| m as u64),
            ))
        })?;
        Ok(rows.collect::<Result<_, _>>()?)
    }

    /// Edge history since `since`: (ts, ok, ms).
    pub fn edge_history(
        &self,
        src: &str,
        dst: &str,
        proto: &str,
        since: i64,
    ) -> Result<Vec<(i64, bool, Option<u64>)>> {
        let mut st = self.0.prepare(
            "SELECT ts, ok, ms FROM edges WHERE src = ?1 AND dst = ?2 AND proto = ?3 AND ts >= ?4 ORDER BY ts",
        )?;
        let rows = st.query_map(params![src, dst, proto, since], |r| {
            Ok((
                r.get(0)?,
                r.get(1)?,
                r.get::<_, Option<i64>>(2)?.map(|m| m as u64),
            ))
        })?;
        Ok(rows.collect::<Result<_, _>>()?)
    }

    /// Deletes samples older than the retention; `totals` is left alone.
    pub fn prune(&self, now: i64) -> Result<()> {
        let cut = now - RETENTION_DAYS * DAY;
        for t in ["samples", "traffic", "edges"] {
            self.0
                .execute(&format!("DELETE FROM {t} WHERE ts < ?1"), params![cut])?;
        }
        Ok(())
    }
}

// ---------- collect ----------

pub fn summary(host: &str, s: &HostSample) -> String {
    if !s.reachable {
        return format!("{host} · ✗ {}", s.error.as_deref().unwrap_or("no answer"));
    }
    let m = &s.m;
    let mut p = vec![];
    if let Some(ms) = s.ssh_ms {
        p.push(format!("ssh {ms} ms"));
    }
    if let Some(l) = m.load1 {
        p.push(format!("load {l}"));
    }
    if let (Some(u), Some(t)) = (m.mem_used, m.mem_total) {
        p.push(format!("memory {}/{} MB", u >> 20, t >> 20));
    }
    if let (Some(u), Some(t)) = (m.disk_used, m.disk_total) {
        p.push(format!("disk {}/{} GB", u >> 30, t >> 30));
    }
    if let Some(c) = m.conns {
        p.push(format!("{c} conns"));
    }
    if let Some(u) = m.updates {
        p.push(format!(
            "updates {u}{}",
            if m.reboot == Some(true) {
                " · reboot required"
            } else {
                ""
            }
        ));
    }
    if let Some(e) = m.cert_expires {
        p.push(format!("cert {} d", (e - s.ts) / DAY));
    }
    p.push(match (m.probe_code, m.probe_ms) {
        (Some(c), Some(ms)) => format!("probe {c} in {ms} ms"),
        _ => "probe: no answer".into(),
    });
    p.push(match &m.counters {
        Some(c) => format!("{} counters", c.len()),
        None => "stats unavailable".into(),
    });
    if s.drift == Some(true) {
        p.push("drift".into());
    }
    format!("{host} · {}", p.join(" · "))
}

/// Sample all hosts: `plan` (reachability, marker, drift) plus the host script, deltas, retention.
pub fn collect(st: &State, db: &Db) -> Result<()> {
    let rendered = render::check(st)?;
    let plans = plan::plan(st, &rendered)?;
    let ts = now();
    let outs: Vec<Result<(u64, String), String>> = std::thread::scope(|s| {
        let hs: Vec<_> = plans
            .iter()
            .map(|p| {
                s.spawn(move || {
                    if let Some(e) = &p.unreachable {
                        return Err(e.clone());
                    }
                    let ssh = Ssh::for_host(st, &p.name).map_err(|e| e.to_string())?;
                    let t = Instant::now();
                    ssh.sh("true").map_err(|e| e.to_string())?;
                    let ms = t.elapsed().as_millis() as u64;
                    let h = &st.hosts[&p.name];
                    let (cert, _) = render::cert_paths(st, &p.name);
                    let out = ssh
                        .sh(&host_script(h.engine, h.role, &cert))
                        .map_err(|e| e.to_string())?;
                    Ok((ms, out))
                })
            })
            .collect();
        hs.into_iter().map(|h| h.join().unwrap()).collect()
    });
    for (p, r) in plans.iter().zip(outs) {
        let mut s = HostSample {
            ts,
            marker: p.marker.clone(),
            drift: p
                .unreachable
                .is_none()
                .then(|| p.files.iter().any(|(_, x)| *x == plan::Status::Drift)),
            ..Default::default()
        };
        match r {
            Ok((ms, out)) => {
                s.reachable = true;
                s.ssh_ms = Some(ms);
                s.m = parse_host(&out);
            }
            Err(e) => s.error = Some(e),
        }
        db.sample(&p.name, "collect", &s)?;
        if let Some(c) = &s.m.counters {
            db.traffic(ts, &p.name, c)?;
        }
        println!("{}", summary(&p.name, &s));
    }
    db.prune(ts)
}

/// Chain probe from every entry; the result is a `probe` sample.
pub fn probe(st: &State, db: &Db) -> Result<()> {
    let ts = now();
    for (name, h) in st.entries() {
        let r = Ssh::for_host(st, name)
            .and_then(|ssh| ssh.sh(&format!("echo \"probe=$({})\"", probe_cmd(h.role))));
        let s = match r {
            Ok(out) => HostSample {
                ts,
                reachable: true,
                m: parse_host(&out),
                ..Default::default()
            },
            Err(e) => HostSample {
                ts,
                error: Some(e.to_string()),
                ..Default::default()
            },
        };
        println!(
            "{name} · {}",
            match (s.reachable, s.m.probe_code, s.m.probe_ms) {
                (true, Some(c), Some(ms)) => format!("{c} in {ms} ms"),
                (true, _, _) => "✗ no answer through the chain".into(),
                (false, _, _) => format!("✗ {}", s.error.as_deref().unwrap_or("")),
            }
        );
        db.sample(name, "probe", &s)?;
    }
    Ok(())
}

// ---------- matrix ----------

pub fn tcp_ms(addr: &str, timeout: Duration) -> Option<u64> {
    use std::net::{TcpStream, ToSocketAddrs};
    let a = addr.to_socket_addrs().ok()?.next()?;
    let t = Instant::now();
    TcpStream::connect_timeout(&a, timeout).ok()?;
    Some(t.elapsed().as_millis() as u64)
}

/// Where the laptop connects for each enabled inbound: an address for TCP, None for UDP.
pub fn laptop_targets(st: &State, host: &str) -> Vec<(Proto, Option<String>)> {
    let h = &st.hosts[host];
    st.inbounds
        .get(host)
        .map(|m| {
            m.iter()
                .filter(|(_, i)| i.enabled)
                .map(|(p, i)| {
                    let addr = match p {
                        Proto::Reality | Proto::Mtg => Some(format!("{}:{}", h.ip, i.port)),
                        Proto::Ws => h.domain.as_ref().map(|d| format!("{d}:443")),
                        Proto::Hy2 | Proto::Awg => None,
                    };
                    (*p, addr)
                })
                .collect()
        })
        .unwrap_or_default()
}

fn edge(src: &str, dst: &str, proto: &str, ms: Option<u64>, note: Option<&str>) -> Edge {
    Edge {
        src: src.into(),
        dst: dst.into(),
        proto: proto.into(),
        ok: ms.is_some() && note.is_none(),
        ms,
        note: note.map(String::from),
    }
}

/// Script on the host: for each protocol of its outgoing links a TCP connect (`bash`: dash has no `/dev/tcp`)
/// or an awg handshake plus RTT to the second tunnel address. Lines: `<to> <proto> ok <ms>|fail|hs <epoch> [<ms>]`.
pub fn edge_script(st: &State, host: &str) -> String {
    let mut s = String::new();
    for (i, l) in render::out_links(st, host) {
        let to = &st.hosts[&l.to];
        for p in &l.protocols {
            let port = st
                .inbounds
                .get(&l.to)
                .and_then(|m| m.get(p))
                .map(|x| x.port)
                .unwrap_or(443);
            match p {
                Proto::Awg => {
                    let peer = l
                        .tunnel
                        .and_then(|t| t.hosts().nth(1))
                        .map(|a| a.to_string())
                        .unwrap_or_default();
                    s.push_str(&format!(
                        "h=$(awg show awg{i} latest-handshakes 2>/dev/null | awk '{{print $2; exit}}'); echo \"{} awg hs ${{h:-0}} $(ping -c1 -W2 {peer} 2>/dev/null | sed -n 's/.*time=\\([0-9.]*\\).*/\\1/p')\"\n",
                        l.to
                    ));
                }
                Proto::Hy2 => s.push_str(&format!(
                    // hysteria2 is UDP: a TCP connect to the port never succeeds. The link itself is checked:
                    // the probe via the engine socks follows the route, that is through this exit.
                    "s=$(date +%s%N); c=$(curl -s -o /dev/null -w '%{{http_code}}' --max-time 8 --socks5-hostname 127.0.0.1:{} {}); if [ \"$c\" = 204 ]; then echo \"{} {} ok $(( ($(date +%s%N)-s)/1000000 ))\"; else echo \"{} {} fail udp\"; fi\n",
                    render::PROBE_PORT,
                    render::PROBE_URL,
                    l.to,
                    p.name(),
                    l.to,
                    p.name()
                )),
                _ => s.push_str(&format!(
                    "s=$(date +%s%N); if timeout 5 bash -c 'exec 3<>/dev/tcp/{}/{port}' 2>/dev/null; then echo \"{} {} ok $(( ($(date +%s%N)-s)/1000000 ))\"; else echo \"{} {} fail\"; fi\n",
                    to.ip,
                    l.to,
                    p.name(),
                    l.to,
                    p.name()
                )),
            }
        }
    }
    s
}

pub fn parse_edges(src: &str, out: &str, now: i64) -> Vec<Edge> {
    out.lines()
        .filter_map(|l| {
            let w: Vec<&str> = l.split_whitespace().collect();
            let (dst, proto) = (*w.first()?, *w.get(1)?);
            let num = |i: usize| w.get(i).and_then(|x| x.parse::<f64>().ok());
            Some(match w.get(2).copied() {
                Some("ok") => edge(src, dst, proto, num(3).map(|x| x as u64), None),
                Some("hs") => {
                    let hs = num(3).unwrap_or(0.0) as i64;
                    let age = now - hs;
                    let ms = num(4).map(|x| x.round() as u64);
                    if hs > 0 && age < AWG_HANDSHAKE_MAX {
                        edge(src, dst, proto, ms, None)
                    } else if hs == 0 {
                        edge(src, dst, proto, ms, Some("no handshake"))
                    } else {
                        edge(
                            src,
                            dst,
                            proto,
                            ms,
                            Some(&format!("handshake {} min ago", age / 60)),
                        )
                    }
                }
                _ => edge(src, dst, proto, None, Some("timeout")),
            })
        })
        .collect()
}

/// Laptop->host (tcp, ssh) and host->exit (script on the host) edges, parallel per host; writes to the DB.
pub fn matrix(st: &State, db: &Db) -> Result<Vec<Edge>> {
    let ts = now();
    let per_host: Vec<Vec<Edge>> = std::thread::scope(|s| {
        let hs: Vec<_> = st
            .hosts
            .keys()
            .map(|name| {
                s.spawn(move || {
                    let mut v = vec![];
                    for (p, addr) in laptop_targets(st, name) {
                        v.push(match addr {
                            Some(a) => match tcp_ms(&a, Duration::from_secs(5)) {
                                Some(ms) => edge(LAPTOP, name, p.name(), Some(ms), None),
                                None => edge(LAPTOP, name, p.name(), None, Some("timeout")),
                            },
                            None => edge(LAPTOP, name, p.name(), None, Some("udp")),
                        });
                    }
                    let t = Instant::now();
                    let ssh = Ssh::for_host(st, name).and_then(|ssh| ssh.sh("true").map(|_| ssh));
                    let ssh = match ssh {
                        Ok(ssh) => {
                            v.push(edge(
                                LAPTOP,
                                name,
                                "ssh",
                                Some(t.elapsed().as_millis() as u64),
                                None,
                            ));
                            Some(ssh)
                        }
                        Err(_) => {
                            v.push(edge(LAPTOP, name, "ssh", None, Some("no answer")));
                            None
                        }
                    };
                    let script = edge_script(st, name);
                    if !script.is_empty() {
                        match ssh.and_then(|ssh| ssh.sh(&script).ok()) {
                            Some(out) => v.extend(parse_edges(name, &out, ts)),
                            None => {
                                for l in render::out_links(st, name).into_iter().map(|(_, l)| l) {
                                    for p in &l.protocols {
                                        v.push(edge(
                                            name,
                                            &l.to,
                                            p.name(),
                                            None,
                                            Some("no answer"),
                                        ));
                                    }
                                }
                            }
                        }
                    }
                    v
                })
            })
            .collect();
        hs.into_iter().map(|h| h.join().unwrap()).collect()
    });
    let edges: Vec<Edge> = per_host.into_iter().flatten().collect();
    db.edges(ts, &edges)?;
    Ok(edges)
}

pub fn cell(e: Option<&Edge>) -> String {
    match e {
        None => "— not described".into(),
        Some(e) if e.ok => format!("● {} ms", e.ms.unwrap_or(0)),
        Some(e) if e.note.as_deref() == Some("udp") => "— udp".into(),
        Some(e) => format!("✗ {}", e.note.as_deref().unwrap_or("timeout")),
    }
}

/// "edge x protocol" table as in `screens/05-topology.html`.
pub fn table(edges: &[Edge]) -> String {
    let cols: Vec<&str> = Proto::ALL.iter().map(|p| p.name()).chain(["ssh"]).collect();
    let mut rows: Vec<(&str, &str)> = vec![];
    for e in edges {
        if !rows.contains(&(&e.src, &e.dst)) {
            rows.push((&e.src, &e.dst));
        }
    }
    let mut s = format!("{:<24}", "edge");
    for c in &cols {
        s.push_str(&format!("{c:<20}"));
    }
    s.push('\n');
    for (src, dst) in rows {
        s.push_str(&format!("{:<24}", format!("{src} → {dst}")));
        for c in &cols {
            let e = edges
                .iter()
                .find(|e| e.src == src && e.dst == dst && e.proto == *c);
            s.push_str(&format!("{:<20}", cell(e)));
        }
        s.push('\n');
    }
    s
}

// ---------- status ----------

#[derive(Debug, Serialize)]
pub struct HostOut {
    pub role: Role,
    pub domain: Option<String>,
    /// Last full sample.
    pub last: Option<HostSample>,
    pub cert_days: Option<i64>,
    /// Last chain probe (collect or probe).
    pub probe: Option<ProbeOut>,
}

#[derive(Debug, Serialize)]
pub struct ProbeOut {
    pub ts: i64,
    pub code: Option<u16>,
    pub ms: Option<u64>,
}

#[derive(Debug, Serialize)]
pub struct UserOut {
    pub enabled: bool,
    pub day: Traffic,
    pub week: Traffic,
    pub total: Traffic,
}

#[derive(Debug, Serialize)]
pub struct Status {
    pub ts: i64,
    pub hosts: BTreeMap<String, HostOut>,
    pub users: BTreeMap<String, UserOut>,
    /// Host -> outbound tag -> traffic per day.
    pub outbounds: BTreeMap<String, BTreeMap<String, Traffic>>,
    pub matrix_ts: Option<i64>,
    pub matrix: Vec<Edge>,
}

/// User accounts in the engine (awg peers live in the kernel and never reach stats).
pub fn accounts(name: &str, u: &User) -> Vec<String> {
    u.modes
        .iter()
        .flat_map(|m| {
            u.protocols
                .iter()
                .filter(|p| p.is_engine_user())
                .map(move |p| State::email(name, *m, *p))
        })
        .collect()
}

fn add(a: Traffic, b: Traffic) -> Traffic {
    Traffic {
        up: a.up + b.up,
        down: a.down + b.down,
    }
}

/// Summary without touching the hosts: state plus database.
pub fn status(st: &State, db: &Db, now: i64) -> Result<Status> {
    let mut hosts = BTreeMap::new();
    for (name, h) in &st.hosts {
        let last = db.last_sample(name, Some("collect"))?;
        let probe = db.last_sample(name, None)?.map(|s| ProbeOut {
            ts: s.ts,
            code: s.m.probe_code,
            ms: s.m.probe_ms,
        });
        hosts.insert(
            name.clone(),
            HostOut {
                role: h.role,
                domain: h.domain.clone(),
                cert_days: last
                    .as_ref()
                    .and_then(|s| s.m.cert_expires)
                    .map(|e| (e - now) / DAY),
                last,
                probe,
            },
        );
    }
    let mut users = BTreeMap::new();
    for (name, u) in &st.users {
        let mut o = UserOut {
            enabled: u.enabled,
            day: Traffic::default(),
            week: Traffic::default(),
            total: Traffic::default(),
        };
        for a in accounts(name, u) {
            o.day = add(o.day, db.sum("user", &a, now - DAY)?);
            o.week = add(o.week, db.sum("user", &a, now - 7 * DAY)?);
            o.total = add(o.total, db.total("user", &a)?);
        }
        users.insert(name.clone(), o);
    }
    let (matrix_ts, matrix) = db.last_edges()?;
    Ok(Status {
        ts: now,
        hosts,
        users,
        outbounds: db.outbounds(now - DAY)?,
        matrix_ts,
        matrix,
    })
}

fn gb(b: u64) -> String {
    format!("{:.1}", b as f64 / 1e9)
}

/// Human-readable summary for `status` without `--json`.
pub fn print_status(s: &Status) {
    for (name, h) in &s.hosts {
        match &h.last {
            // certificate expiry is already in the summary
            Some(l) => println!("{}", summary(name, l)),
            None => println!("{name} · not collected yet"),
        }
    }
    for (name, u) in &s.users {
        println!(
            "{name}{} · day ↓{} ↑{} GB · week ↓{} ↑{} · total ↓{} ↑{}",
            if u.enabled { "" } else { " (off)" },
            gb(u.day.down),
            gb(u.day.up),
            gb(u.week.down),
            gb(u.week.up),
            gb(u.total.down),
            gb(u.total.up)
        );
    }
    if !s.matrix.is_empty() {
        print!("{}", table(&s.matrix));
    }
}
