#![allow(clippy::too_many_arguments)]
pub mod apply;
pub mod bootstrap;
pub mod git;
pub mod metrics;
pub mod plan;
pub mod ssh;
pub mod tui;

// Чистое ядро вынесено в отдельный крейт и реэкспортируется под прежними путями,
// чтобы существующий код продолжал работать без массовой правки путей.
pub use vpnctl_core::{ops, render, secrets, state, validate};
