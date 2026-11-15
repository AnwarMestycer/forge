//! forge-proxy: a TCP proxy that injects network faults between an app and
//! its upstream (e.g. Postgres).
//!
//! `faults` decides what happens to each chunk of data; `relay` moves the
//! bytes and applies those decisions.

pub mod faults;
pub mod relay;
