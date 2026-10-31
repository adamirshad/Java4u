use std::path::PathBuf;

use serde::{Deserialize, Serialize};

use crate::error::{Error, Result};
use crate::optimize::{self, Profile};
use crate::paths::Dirs;

fn default_memory() -> u32 {
    optimize::recommended_memory_mb()
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Instance {
    pub name: String,
    pub version: String,
    #[serde(default = "default_memory")]
    pub memory_mb: u32,
    #[serde(default)]
    pub java_path: Option<PathBuf>,
    #[serde(default)]
    pub jvm_args: Vec<String>,
    #[serde(default)]
    pub profile: Profile,
}

fn validate_name(name: &str) -> Result<()> {
    let ok = !name.is_empty()
        && name.len() <= 64
        && !name.starts_with('.')
        && name
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || matches!(c, '-' | '_' | ' ' | '.'));
    if ok {
        Ok(())
    } else {
        Err(Error::Other(
            "instance name may only contain letters, digits, space, '-', '_' and '.'".into(),
        ))
    }
}

impl Instance {
    pub fn create(dirs: &Dirs, name: &str, version: &str) -> Result<Self> {
        validate_name(name)?;
        if dirs.instance_dir(name).join("instance.json").exists() {
            return Err(Error::Other(format!("instance '{name}' already exists")));
        }
        let inst = Self {
            name: name.into(),
            version: version.into(),
            memory_mb: default_memory(),
            java_path: None,
            jvm_args: Vec::new(),
            profile: Profile::default(),
        };
        inst.save(dirs)?;
        Ok(inst)
    }

    pub fn load(dirs: &Dirs, name: &str) -> Result<Self> {
        validate_name(name)?;
        let path = dirs.instance_dir(name).join("instance.json");
        let text = std::fs::read_to_string(&path)
            .map_err(|_| Error::Other(format!("no such instance: {name}")))?;
        Ok(serde_json::from_str(&text)?)
    }

    pub fn save(&self, dirs: &Dirs) -> Result<()> {
        let dir = dirs.instance_dir(&self.name);
        std::fs::create_dir_all(&dir)?;
        std::fs::write(dir.join("instance.json"), serde_json::to_string_pretty(self)?)?;
        Ok(())
    }

    pub fn list(dirs: &Dirs) -> Result<Vec<Self>> {
        let mut out = Vec::new();
        let Ok(rd) = std::fs::read_dir(dirs.instances_dir()) else {
            return Ok(out);
        };
        for entry in rd.flatten() {
            if let Ok(text) = std::fs::read_to_string(entry.path().join("instance.json")) {
                if let Ok(inst) = serde_json::from_str::<Instance>(&text) {
                    out.push(inst);
                }
            }
        }
        out.sort_by(|a, b| a.name.cmp(&b.name));
        Ok(out)
    }

    /// Profile flags followed by the user's own arguments (which win on conflict).
    pub fn effective_jvm_args(&self) -> Vec<String> {
        let mut args = optimize::jvm_flags(self.profile, self.memory_mb, &self.jvm_args);
        args.extend(self.jvm_args.iter().cloned());
        args
    }

    /// The directory used as Minecraft's game directory (saves, mods, options.txt...).
    pub fn game_dir(&self, dirs: &Dirs) -> PathBuf {
        dirs.instance_dir(&self.name).join("minecraft")
    }
}
