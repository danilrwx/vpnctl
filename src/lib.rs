#![allow(clippy::too_many_arguments)]
pub mod apply;
pub mod bootstrap;
pub mod git;
pub mod metrics;
pub mod plan;
pub mod ssh;

// The pure core lives in its own crate and is re-exported under the old paths,
// so existing code keeps working without a mass path rewrite.
pub use vpnctl_core::{ops, render, secrets, state, validate};
