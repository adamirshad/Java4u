use serde::{Deserialize, Serialize};

use crate::error::Result;
use crate::paths::Dirs;

fn default_dim() -> f32 {
    0.45
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    /// File name inside the wallpapers directory.
    #[serde(default)]
    pub wallpaper: Option<String>,
    /// 0.0 (no darkening) to 0.9; keeps text readable on bright images.
    #[serde(default = "default_dim")]
    pub wallpaper_dim: f32,
    #[serde(default)]
    pub offline_username: String,
    #[serde(default)]
    pub last_instance: Option<String>,
    #[serde(default)]
    pub show_snapshots: bool,
}

impl Default for Settings {
    fn default() -> Self {
        Self {
            wallpaper: None,
            wallpaper_dim: default_dim(),
            offline_username: "Player".into(),
            last_instance: None,
            show_snapshots: false,
        }
    }
}

impl Settings {
    /// Missing or unreadable settings fall back to defaults rather than blocking startup.
    pub fn load(dirs: &Dirs) -> Self {
        std::fs::read_to_string(dirs.settings_file())
            .ok()
            .and_then(|t| serde_json::from_str(&t).ok())
            .unwrap_or_default()
    }

    pub fn save(&self, dirs: &Dirs) -> Result<()> {
        std::fs::create_dir_all(&dirs.root)?;
        std::fs::write(dirs.settings_file(), serde_json::to_string_pretty(self)?)?;
        Ok(())
    }
}
