use std::path::PathBuf;

use directories::ProjectDirs;

use crate::error::{Error, Result};

/// All on-disk locations used by the launcher.
#[derive(Clone, Debug)]
pub struct Dirs {
    pub root: PathBuf,
}

impl Dirs {
    pub fn new(root: impl Into<PathBuf>) -> Self {
        Self { root: root.into() }
    }

    /// Platform default data directory (e.g. ~/.local/share/java4u).
    pub fn default_root() -> Result<Self> {
        let pd = ProjectDirs::from("dev", "java4u", "java4u")
            .ok_or_else(|| Error::Other("could not determine a data directory".into()))?;
        Ok(Self::new(pd.data_dir()))
    }

    pub fn versions_dir(&self) -> PathBuf {
        self.root.join("versions")
    }
    pub fn version_dir(&self, id: &str) -> PathBuf {
        self.versions_dir().join(id)
    }
    pub fn version_json(&self, id: &str) -> PathBuf {
        self.version_dir(id).join(format!("{id}.json"))
    }
    pub fn version_jar(&self, id: &str) -> PathBuf {
        self.version_dir(id).join(format!("{id}.jar"))
    }
    pub fn libraries_dir(&self) -> PathBuf {
        self.root.join("libraries")
    }
    pub fn assets_dir(&self) -> PathBuf {
        self.root.join("assets")
    }
    pub fn asset_indexes_dir(&self) -> PathBuf {
        self.assets_dir().join("indexes")
    }
    pub fn asset_index(&self, id: &str) -> PathBuf {
        self.asset_indexes_dir().join(format!("{id}.json"))
    }
    pub fn asset_objects_dir(&self) -> PathBuf {
        self.assets_dir().join("objects")
    }
    pub fn natives_dir(&self, version_id: &str) -> PathBuf {
        self.root.join("natives").join(version_id)
    }
    pub fn accounts_file(&self) -> PathBuf {
        self.root.join("accounts.json")
    }
    pub fn runtimes_dir(&self) -> PathBuf {
        self.root.join("runtimes")
    }
    pub fn wallpapers_dir(&self) -> PathBuf {
        self.root.join("wallpapers")
    }
    pub fn settings_file(&self) -> PathBuf {
        self.root.join("settings.json")
    }
    pub fn instances_dir(&self) -> PathBuf {
        self.root.join("instances")
    }
    pub fn instance_dir(&self, name: &str) -> PathBuf {
        self.instances_dir().join(name)
    }
}
