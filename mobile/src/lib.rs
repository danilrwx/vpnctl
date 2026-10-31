//! Mobile-facing API over the pure core.
//!
//! The functions here are what a phone app needs: read the state, list users,
//! build subscription and direct links. No SSH, no apply — those stay on the
//! laptop for now.
//!
//! The secrets backend must be installed first (`secrets_io::install`), because
//! a phone has no `sops` binary to spawn.

pub use vpnctl_core::secrets_io::{Secrets, install};

use anyhow::{Context, Result};
use std::path::Path;
use vpnctl_core::render::subs;
use vpnctl_core::state::{Mode, State};

fn load(state_dir: &str) -> Result<State> {
    State::load(Path::new(state_dir))
}

/// User names in the state, sorted.
pub fn users(state_dir: &str) -> Result<Vec<String>> {
    let st = load(state_dir)?;
    Ok(st.users.keys().cloned().collect())
}

/// Subscription URL of a user for its first mode.
pub fn subscription(state_dir: &str, user: &str) -> Result<String> {
    let st = load(state_dir)?;
    let u = st
        .users
        .get(user)
        .with_context(|| format!("user «{user}» is not described"))?;
    let entry = st.entry_of(u).context("the user has no entry")?;
    let mode = u.modes.first().copied().unwrap_or(Mode::Split);
    subs::sub_url(&st, &entry, user, mode)
}

/// Direct vless links of a user (one per protocol), for clients and routers
/// that do not support subscriptions.
pub fn links(state_dir: &str, user: &str) -> Result<Vec<String>> {
    let st = load(state_dir)?;
    let u = st
        .users
        .get(user)
        .with_context(|| format!("user «{user}» is not described"))?;
    let entry = st.entry_of(u).context("the user has no entry")?;
    let mode = u.modes.first().copied().unwrap_or(Mode::Split);
    subs::links(&st, &entry, user, u, mode)
}

/// Telegram MTProto proxy link of the entry, when the entry has the inbound.
pub fn telegram_proxy(state_dir: &str) -> Result<Option<String>> {
    let st = load(state_dir)?;
    let entry = st
        .hosts
        .iter()
        .find(|(_, h)| h.role == vpnctl_core::state::Role::Entry)
        .map(|(n, _)| n.clone())
        .context("the state has no entry")?;
    Ok(subs::tg_link(&st, &entry))
}

/// Validation errors of the state; an empty list means it is valid.
pub fn validate(state_dir: &str) -> Result<Vec<String>> {
    let st = load(state_dir)?;
    Ok(vpnctl_core::validate::validate(&st))
}
