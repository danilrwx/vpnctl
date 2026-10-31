//! Pure core: state, validation, config rendering and state operations.
//!
//! Nothing here spawns external processes or depends on the OS —
//! only files and data. Anything that needs `git`, `sops`, `ssh` or a terminal
//! lives in the CLI crate and, later, in the mobile crate.
#![allow(clippy::too_many_arguments)]

pub mod ops;
pub mod render;
pub mod secrets;
pub mod secrets_io;
pub mod state;
pub mod validate;
