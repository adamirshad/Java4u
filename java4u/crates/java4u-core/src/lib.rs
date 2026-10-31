//! java4u-core: everything a Minecraft launcher needs, with no UI attached.

pub mod accounts;
pub mod auth;
pub mod download;
pub mod error;
pub mod install;
pub mod instance;
pub mod java;
pub mod launch;
pub mod manifest;
pub mod microsoft;
pub mod optimize;
pub mod paths;
pub mod runtime;
pub mod settings;
pub mod version;
pub mod wallpaper;

pub use error::{Error, Result};
pub use paths::Dirs;
pub use reqwest;
